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

// Package k8speer provides a PeerDiscovery plugin that watches the EPP Service's
// EndpointSlices via a controller-runtime reconciler.
package k8speer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/llm-d/llm-d-router/pkg/epp/controller"
	fwkdl "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
	fwkplugin "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
	"github.com/llm-d/llm-d-router/pkg/epp/statesync"
)

const PluginType = "k8s-peer-discovery"

type params struct {
	// ServiceName is the EPP's own Service whose EndpointSlices enumerate
	// peer replicas. Required.
	ServiceName string `json:"serviceName"`
}

// Plugin implements PeerDiscovery by watching EndpointSlices via a
// controller-runtime reconciler registered with the caller's manager.
type Plugin struct {
	typedName   fwkplugin.TypedName
	serviceName string
	selfAddress string
	store       *statesync.MemoryPeerStore

	ready     chan struct{}
	readyOnce sync.Once
}

var _ fwkdl.PeerDiscovery = (*Plugin)(nil)

func Factory(name string, parameters *json.Decoder, _ fwkplugin.Handle) (fwkplugin.Plugin, error) {
	p := &params{}
	if parameters != nil {
		if err := parameters.Decode(p); err != nil {
			return nil, fmt.Errorf("%s: failed to parse parameters: %w", PluginType, err)
		}
	}
	if p.ServiceName == "" {
		return nil, errors.New(PluginType + ": 'serviceName' parameter is required")
	}
	if name == "" {
		name = PluginType
	}
	return &Plugin{
		typedName:   fwkplugin.TypedName{Type: PluginType, Name: name},
		serviceName: p.ServiceName,
		selfAddress: os.Getenv("POD_IP"),
		store:       statesync.NewMemoryPeerStore(),
		ready:       make(chan struct{}),
	}, nil
}

func (p *Plugin) TypedName() fwkplugin.TypedName { return p.typedName }

// ServiceName returns the EPP Service name so the runner can scope the
// EndpointSlice informer cache to this Service.
func (p *Plugin) ServiceName() string { return p.serviceName }

// SelfAddress returns this replica's address for self-exclusion.
func (p *Plugin) SelfAddress() string { return p.selfAddress }

// Store returns the peer store populated by the reconciler.
func (p *Plugin) Store() *statesync.MemoryPeerStore { return p.store }

// SetupWithManager registers the EPPPeerReconciler with the given manager.
// Must be called before the manager starts.
func (p *Plugin) SetupWithManager(mgr ctrl.Manager, namespace string) error {
	return (&controller.EPPPeerReconciler{
		Reader:      mgr.GetClient(),
		Notifier:    fwkdl.NewPeerNotifier(p.store),
		ServiceName: p.serviceName,
		Namespace:   namespace,
		SelfAddress: p.selfAddress,
		OnFirstReconcile: func() {
			p.readyOnce.Do(func() { close(p.ready) })
		},
	}).SetupWithManager(mgr)
}

func (p *Plugin) Ready() <-chan struct{} { return p.ready }

// Start blocks until ctx is cancelled. The reconciler is driven by the
// controller-runtime manager, not by this method.
func (p *Plugin) Start(ctx context.Context, _ fwkdl.PeerNotifier) error {
	<-ctx.Done()
	return nil
}
