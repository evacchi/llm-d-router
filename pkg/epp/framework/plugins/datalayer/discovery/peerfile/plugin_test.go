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

package peerfile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	fwkdl "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
	fwkplugin "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
)

// recordingNotifier captures Upsert and Delete calls for assertions.
type recordingNotifier struct {
	mu       sync.Mutex
	upserted []*fwkdl.PeerMetadata
	deleted  []types.NamespacedName
}

func (r *recordingNotifier) Upsert(meta *fwkdl.PeerMetadata) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upserted = append(r.upserted, meta)
}

func (r *recordingNotifier) Delete(id types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, id)
}

func (r *recordingNotifier) upsertedNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, len(r.upserted))
	for i, m := range r.upserted {
		names[i] = m.ID.String()
	}
	return names
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "peers-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func newFPD(path string, watch bool) *FilePeerDiscovery {
	return &FilePeerDiscovery{
		path:            path,
		watchFile:       watch,
		validateAddress: validateIPv4Address,
		peers:           make(map[types.NamespacedName]struct{}),
		ready:           make(chan struct{}),
	}
}

const validYAML = `
peers:
  - name: epp-0
    namespace: ns1
    address: "10.0.0.1"
    port: "9000"
  - name: epp-1
    address: "10.0.0.2"
    port: "9001"
`

func TestFactory_MissingPath(t *testing.T) {
	_, err := Factory("", fwkplugin.StrictDecoder(json.RawMessage(`{}`)), nil)
	assert.ErrorContains(t, err, "'path' parameter is required")
}

func TestFactory_InvalidJSON(t *testing.T) {
	_, err := Factory("", fwkplugin.StrictDecoder(json.RawMessage(`{bad json`)), nil)
	assert.ErrorContains(t, err, "failed to parse parameters")
}

func TestFactory_ValidParams(t *testing.T) {
	path := writeTemp(t, validYAML)
	plugin, err := Factory("my-peers", fwkplugin.StrictDecoder(json.RawMessage(`{"path":"`+path+`"}`)), nil)
	require.NoError(t, err)
	assert.Equal(t, PluginType, plugin.TypedName().Type)
	assert.Equal(t, "my-peers", plugin.TypedName().Name)
}

func TestFactory_DefaultName(t *testing.T) {
	path := writeTemp(t, validYAML)
	plugin, err := Factory("", fwkplugin.StrictDecoder(json.RawMessage(`{"path":"`+path+`"}`)), nil)
	require.NoError(t, err)
	assert.Equal(t, PluginType, plugin.TypedName().Name)
}

func TestStart_LoadsPeers(t *testing.T) {
	path := writeTemp(t, validYAML)
	notifier := &recordingNotifier{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fpd := newFPD(path, false)
	require.NoError(t, fpd.Start(ctx, notifier))

	assert.ElementsMatch(t, []string{"ns1/epp-0", "default/epp-1"}, notifier.upsertedNames())
	assert.Empty(t, notifier.deleted)

	select {
	case <-fpd.Ready():
	default:
		t.Fatal("Ready() channel should be closed after a successful initial load")
	}
}

func TestReady_StaysOpenWhenInitialLoadFails(t *testing.T) {
	fpd := newFPD("/nonexistent/peers.yaml", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := fpd.Start(ctx, &recordingNotifier{})
	require.Error(t, err)

	select {
	case <-fpd.Ready():
		t.Fatal("Ready() must not be closed when initial load fails")
	default:
	}
}

func TestStart_DefaultNamespace(t *testing.T) {
	path := writeTemp(t, `
peers:
  - name: epp-0
    address: "10.0.0.1"
    port: "9000"
`)
	notifier := &recordingNotifier{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, newFPD(path, false).Start(ctx, notifier))
	assert.Equal(t, "default", notifier.upserted[0].ID.Namespace)
}

func TestStart_AddressAndPort(t *testing.T) {
	path := writeTemp(t, `
peers:
  - name: epp-0
    address: "10.0.0.1"
    port: "9000"
`)
	notifier := &recordingNotifier{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, newFPD(path, false).Start(ctx, notifier))
	assert.Equal(t, "10.0.0.1", notifier.upserted[0].Address)
	assert.Equal(t, "9000", notifier.upserted[0].Port)
}

func TestStart_MissingFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := newFPD("/nonexistent/peers.yaml", false).Start(ctx, &recordingNotifier{})
	assert.ErrorContains(t, err, "initial load failed")
}

func TestStart_InvalidIP(t *testing.T) {
	path := writeTemp(t, `
peers:
  - name: epp-0
    address: "not-an-ip"
    port: "9000"
`)
	err := newFPD(path, false).Start(context.Background(), &recordingNotifier{})
	assert.ErrorContains(t, err, "invalid IPv4 address")
}

func TestStart_RejectsIPv6(t *testing.T) {
	path := writeTemp(t, `
peers:
  - name: epp-0
    address: "::1"
    port: "9000"
`)
	err := newFPD(path, false).Start(context.Background(), &recordingNotifier{})
	assert.ErrorContains(t, err, "invalid IPv4 address")
}

func TestStart_InvalidPort(t *testing.T) {
	path := writeTemp(t, `
peers:
  - name: epp-0
    address: "10.0.0.1"
    port: "99999"
`)
	err := newFPD(path, false).Start(context.Background(), &recordingNotifier{})
	assert.ErrorContains(t, err, "invalid port")
}

func TestStart_FileTooLarge(t *testing.T) {
	content := strings.Repeat("x", maxPeersFileSize+1)
	path := writeTemp(t, content)
	err := newFPD(path, false).Start(context.Background(), &recordingNotifier{})
	assert.ErrorContains(t, err, "exceeds 1 MiB limit")
}

func TestStart_DeletesRemovedPeers(t *testing.T) {
	path := writeTemp(t, validYAML)
	fpd := newFPD(path, false)
	notifier := &recordingNotifier{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, fpd.Start(ctx, notifier))
	assert.Len(t, notifier.upserted, 2)

	require.NoError(t, os.WriteFile(path, []byte(`
peers:
  - name: epp-0
    namespace: ns1
    address: "10.0.0.1"
    port: "9000"
`), 0o600))
	notifier2 := &recordingNotifier{}
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	require.NoError(t, fpd.Start(ctx2, notifier2))

	assert.Len(t, notifier2.upserted, 1)
	assert.Len(t, notifier2.deleted, 1)
	assert.Equal(t, types.NamespacedName{Name: "epp-1", Namespace: "default"}, notifier2.deleted[0])
}

func TestLoad_ReloadErrorRetainsPriorState(t *testing.T) {
	path := writeTemp(t, validYAML)
	fpd := newFPD(path, false)
	notifier := &recordingNotifier{}
	require.NoError(t, fpd.load(notifier))

	fpd.mu.RLock()
	before := len(fpd.peers)
	fpd.mu.RUnlock()
	require.Equal(t, 2, before)

	require.NoError(t, os.WriteFile(path, []byte("peers: [ this is not valid"), 0o600))
	require.Error(t, fpd.load(notifier))

	fpd.mu.RLock()
	after := len(fpd.peers)
	fpd.mu.RUnlock()
	assert.Equal(t, before, after, "a parse failure must retain the prior peer set")
	assert.Empty(t, notifier.deleted, "a failed reload must not delete peers")
}

func TestStart_WatchFileReloadsOnWrite(t *testing.T) {
	path := writeTemp(t, validYAML)
	fpd := newFPD(path, true)
	notifier := &recordingNotifier{}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- fpd.Start(ctx, notifier) }()

	newContent := []byte(`
peers:
  - name: epp-2
    address: "10.0.0.3"
    port: "9002"
`)
	// Re-touch the file each poll so the write that lands after the watcher
	// is attached is the one that triggers the reload. Avoids racing on the
	// gap between Start()'s initial load and watcher.Add().
	require.Eventually(t, func() bool {
		if err := os.WriteFile(path, newContent, 0o600); err != nil {
			return false
		}
		notifier.mu.Lock()
		defer notifier.mu.Unlock()
		for _, m := range notifier.upserted {
			if m.ID.Name == "epp-2" {
				return true
			}
		}
		return false
	}, 2*time.Second, 50*time.Millisecond)

	cancel()
	assert.NoError(t, <-done)
}

func TestDumpState(t *testing.T) {
	f := &FilePeerDiscovery{
		peers: map[types.NamespacedName]struct{}{
			{Namespace: "default", Name: "epp-b"}: {},
			{Namespace: "default", Name: "epp-a"}: {},
		},
	}

	payload, err := f.DumpState()
	require.NoError(t, err)

	var state discoveryState
	require.NoError(t, json.Unmarshal(payload, &state))
	assert.Equal(t, []string{"default/epp-a", "default/epp-b"}, state.Peers)
	assert.Equal(t, 2, state.TotalPeers)
	assert.Equal(t, maxDebugDumpPeers, state.MaxPeers)
	assert.LessOrEqual(t, state.TotalPeers, state.MaxPeers)
}

func TestDumpStateCaps(t *testing.T) {
	peers := make(map[types.NamespacedName]struct{}, maxDebugDumpPeers+5)
	for i := range maxDebugDumpPeers + 5 {
		peers[types.NamespacedName{Namespace: "default", Name: fmt.Sprintf("epp-%03d", i)}] = struct{}{}
	}
	f := &FilePeerDiscovery{peers: peers}

	payload, err := f.DumpState()
	require.NoError(t, err)

	var state discoveryState
	require.NoError(t, json.Unmarshal(payload, &state))
	assert.Equal(t, maxDebugDumpPeers+5, state.TotalPeers)
	assert.Greater(t, state.TotalPeers, state.MaxPeers)
	assert.Len(t, state.Peers, maxDebugDumpPeers)
	assert.Equal(t, "default/epp-000", state.Peers[0])
	assert.Equal(t, fmt.Sprintf("default/epp-%03d", maxDebugDumpPeers-1), state.Peers[maxDebugDumpPeers-1])
}

func TestDumpStateConcurrentWithLoad(t *testing.T) {
	path := writeTemp(t, "peers:\n- name: epp-0\n  address: 10.0.0.1\n  port: \"9000\"\n")
	f := &FilePeerDiscovery{path: path, validateAddress: validateIPv4Address, peers: map[types.NamespacedName]struct{}{}}
	notifier := &recordingNotifier{}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			_ = f.load(notifier)
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			if _, err := f.DumpState(); err != nil {
				t.Errorf("DumpState returned error: %v", err)
			}
		}
	}()
	wg.Wait()
}

func TestDumpStateEmpty(t *testing.T) {
	f := &FilePeerDiscovery{peers: map[types.NamespacedName]struct{}{}}

	payload, err := f.DumpState()
	require.NoError(t, err)
	assert.True(t, json.Valid(payload))

	var state discoveryState
	require.NoError(t, json.Unmarshal(payload, &state))
	assert.Empty(t, state.Peers)
	assert.Equal(t, 0, state.TotalPeers)
	assert.Equal(t, maxDebugDumpPeers, state.MaxPeers)
}
