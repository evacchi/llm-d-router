/*
Copyright 2026 The llm-d Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package peerfile provides a file-based PeerDiscovery implementation that reads
// a YAML (or JSON) file listing peer EPP replicas. It is the
// Kubernetes-independent counterpart to the EndpointSlice-based EPPPeerReconciler.
package peerfile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"sync"

	"github.com/fsnotify/fsnotify"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	fwkdl "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
	fwkplugin "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
)

const PluginType = "file-peer-discovery"

// PeerEntry is the YAML/JSON representation of a single peer EPP replica.
type PeerEntry struct {
	Name      string `json:"name"                yaml:"name"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Address   string `json:"address"             yaml:"address"`
	Port      string `json:"port"                yaml:"port"`
}

// PeersFile is the top-level structure of the peers YAML/JSON file.
type PeersFile struct {
	Peers []PeerEntry `json:"peers" yaml:"peers"`
}

// params is the user-facing configuration for the file-peer-discovery plugin.
// It is unmarshalled from the plugin's "parameters" block in the EPP config.
type params struct {
	// Path is the absolute path to the YAML/JSON file listing peers. Required.
	Path string `json:"path"`
	// WatchFile enables hot-reload via fsnotify: edits, atomic renames, and
	// ConfigMap-style symlink swaps trigger a reload of the file. When false
	// (default), the file is read once at startup and never re-read.
	WatchFile bool `json:"watchFile"`
}

// FilePeerDiscovery implements PeerDiscovery by reading a static peers file.
type FilePeerDiscovery struct {
	typedName fwkplugin.TypedName
	path      string
	watchFile bool
	// validateAddress checks a peer address. Injected at construction so a
	// variant can loosen it without load() branching on the plugin type.
	validateAddress func(string) error
	// mu guards peers, which DumpState reads concurrently with load.
	mu sync.RWMutex
	// peers is the set of peer identities applied from the last successful load.
	// Used as a key set only -- values are zero-byte structs. Compared against
	// the entries parsed during a reload to compute which peers to delete.
	peers map[types.NamespacedName]struct{}

	ready     chan struct{}
	readyOnce sync.Once
}

var (
	_ fwkdl.PeerDiscovery   = (*FilePeerDiscovery)(nil)
	_ fwkplugin.StateDumper = (*FilePeerDiscovery)(nil)
)

// Factory is the plugin factory for file-peer-discovery. Peer addresses must be IPv4.
func Factory(name string, parameters *json.Decoder, _ fwkplugin.Handle) (fwkplugin.Plugin, error) {
	return newFilePeerDiscovery(PluginType, name, parameters, validateIPv4Address)
}

// newFilePeerDiscovery decodes the shared parameters and builds a
// FilePeerDiscovery with the given address validator. name defaults to pluginType.
func newFilePeerDiscovery(pluginType, name string, parameters *json.Decoder, validateAddress func(string) error) (*FilePeerDiscovery, error) {
	p := &params{}
	if parameters != nil {
		if err := parameters.Decode(p); err != nil {
			return nil, fmt.Errorf("file-peer-discovery: failed to parse parameters: %w", err)
		}
	}
	if p.Path == "" {
		return nil, errors.New("file-peer-discovery: 'path' parameter is required")
	}
	if name == "" {
		name = pluginType
	}
	return &FilePeerDiscovery{
		typedName:       fwkplugin.TypedName{Type: pluginType, Name: name},
		path:            p.Path,
		watchFile:       p.WatchFile,
		validateAddress: validateAddress,
		peers:           make(map[types.NamespacedName]struct{}),
		ready:           make(chan struct{}),
	}, nil
}

// validateIPv4Address requires the address to be an IPv4 literal, matching the
// pod endpoint discovery contract and the EndpointSlice-based peer reconciler.
func validateIPv4Address(address string) error {
	if ip := net.ParseIP(address); ip == nil || ip.To4() == nil {
		return fmt.Errorf("invalid IPv4 address %q", address)
	}
	return nil
}

func (f *FilePeerDiscovery) TypedName() fwkplugin.TypedName { return f.typedName }

const maxDebugDumpPeers = 100

// discoveryState is the sanitized snapshot returned by DumpState: discovered
// peer identities only, never their addresses. The dump is partial when
// TotalPeers exceeds MaxPeers.
type discoveryState struct {
	Peers      []string `json:"peers"`
	TotalPeers int      `json:"totalPeers"`
	MaxPeers   int      `json:"maxPeers"`
}

// DumpState reports the peer identities currently loaded from the file, sorted
// and capped to maxDebugDumpPeers so the payload stays bounded. The set is
// snapshotted under a read lock, so a concurrent reload may not yet be
// reflected; best-effort visibility is enough for a debug endpoint.
func (f *FilePeerDiscovery) DumpState() (json.RawMessage, error) {
	f.mu.RLock()
	names := make([]string, 0, len(f.peers))
	for id := range f.peers {
		names = append(names, id.String())
	}
	f.mu.RUnlock()

	total := len(names)
	sort.Strings(names)

	state := discoveryState{TotalPeers: total, MaxPeers: maxDebugDumpPeers}
	if len(names) > maxDebugDumpPeers {
		names = names[:maxDebugDumpPeers]
	}
	state.Peers = names
	return json.Marshal(state)
}

// Ready returns a channel closed after the first successful load of the peers
// file. See PeerDiscovery.Ready for the contract.
func (f *FilePeerDiscovery) Ready() <-chan struct{} { return f.ready }

// Start loads the peers file, notifies the consumer, then optionally watches for
// changes. Blocks until ctx is cancelled or a fatal error occurs.
func (f *FilePeerDiscovery) Start(ctx context.Context, notifier fwkdl.PeerNotifier) error {
	logger := log.FromContext(ctx).WithValues("plugin", PluginType, "path", f.path)

	if err := f.load(notifier); err != nil {
		return fmt.Errorf("file-peer-discovery: initial load failed: %w", err)
	}
	f.readyOnce.Do(func() { close(f.ready) })

	if !f.watchFile {
		<-ctx.Done()
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("file-peer-discovery: failed to create watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(f.path); err != nil {
		return fmt.Errorf("file-peer-discovery: failed to watch %s: %w", f.path, err)
	}

	logger.Info("watching peers file for changes")
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Create) {
				// Re-attach to the new inode at f.path after atomic rename
				// (editor safe-write) or ConfigMap symlink swap. Safe to
				// ignore error: if the file isn't present yet the subsequent
				// Create event will re-add it.
				_ = watcher.Add(f.path)
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
				logger.Info("peers file changed, reloading")
				if err := f.load(notifier); err != nil {
					logger.Error(err, "failed to reload peers file")
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			logger.Error(err, "watcher error")
		}
	}
}

const maxPeersFileSize = 1 << 20 // 1 MiB

func (f *FilePeerDiscovery) load(notifier fwkdl.PeerNotifier) error {
	fh, err := os.Open(f.path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", f.path, err)
	}
	defer fh.Close()

	data, err := io.ReadAll(io.LimitReader(fh, maxPeersFileSize+1))
	if err != nil {
		return fmt.Errorf("reading %s: %w", f.path, err)
	}
	if len(data) > maxPeersFileSize {
		return fmt.Errorf("peers file %s exceeds 1 MiB limit", f.path)
	}

	jsonData, err := yaml.YAMLToJSON(data)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", f.path, err)
	}

	var pf PeersFile
	if err := json.Unmarshal(jsonData, &pf); err != nil {
		return fmt.Errorf("unmarshalling %s: %w", f.path, err)
	}

	incoming := make(map[types.NamespacedName]struct{}, len(pf.Peers))
	var errs []error
	for _, p := range pf.Peers {
		if err := f.validateAddress(p.Address); err != nil {
			errs = append(errs, fmt.Errorf("peer %q: %w", p.Name, err))
			continue
		}
		if portNum, err := strconv.Atoi(p.Port); err != nil || portNum < 1 || portNum > 65535 {
			errs = append(errs, fmt.Errorf("peer %q: invalid port %q", p.Name, p.Port))
			continue
		}
		ns := p.Namespace
		if ns == "" {
			ns = "default"
		}
		meta := &fwkdl.PeerMetadata{
			ID:      types.NamespacedName{Name: p.Name, Namespace: ns},
			Address: p.Address,
			Port:    p.Port,
		}
		incoming[meta.ID] = struct{}{}
		notifier.Upsert(meta)
	}

	f.mu.Lock()
	old := f.peers
	f.peers = incoming
	f.mu.Unlock()

	for id := range old {
		if _, ok := incoming[id]; !ok {
			notifier.Delete(id)
		}
	}
	return errors.Join(errs...)
}
