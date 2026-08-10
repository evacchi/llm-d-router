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

package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configapi "github.com/llm-d/llm-d-router/apix/config/v1alpha1"
	fwkplugin "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
	discoverypeerfile "github.com/llm-d/llm-d-router/pkg/epp/framework/plugins/datalayer/discovery/peerfile"
	runserver "github.com/llm-d/llm-d-router/pkg/epp/server"
)

func peerDiscoveryConfig(discoveryRef, peerRef string) *configapi.EndpointPickerConfig {
	dl := &configapi.DataLayerConfig{}
	if discoveryRef != "" {
		dl.Discovery = &configapi.DiscoveryConfig{PluginRef: discoveryRef}
	}
	if peerRef != "" {
		dl.PeerDiscovery = &configapi.PeerDiscoveryConfig{PluginRef: peerRef}
	}
	return &configapi.EndpointPickerConfig{DataLayer: dl}
}

func TestResolvePeerDiscovery_FilePeerDiscovery(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "peers-*.yaml")
	require.NoError(t, err)
	_, _ = f.WriteString("peers: []\n")
	require.NoError(t, f.Close())

	params, _ := json.Marshal(map[string]any{"path": f.Name()})
	p, err := discoverypeerfile.Factory("my-peers", json.NewDecoder(bytes.NewReader(params)), nil)
	require.NoError(t, err)

	r := &Runner{PluginHandle: newHandleWithPlugin(t, "my-peers", p)}
	disc, err := r.resolvePeerDiscovery(peerDiscoveryConfig("my-disc", "my-peers"))
	require.NoError(t, err)
	assert.IsType(t, &discoverypeerfile.FilePeerDiscovery{}, disc)
	assert.Equal(t, discoverypeerfile.PluginType, disc.TypedName().Type)
	assert.Equal(t, "my-peers", disc.TypedName().Name)
}

func TestResolvePeerDiscovery_NotConfigured(t *testing.T) {
	r := &Runner{PluginHandle: fwkplugin.NewEppHandle(context.Background(), nil)}
	disc, err := r.resolvePeerDiscovery(peerDiscoveryConfig("my-disc", ""))
	require.NoError(t, err)
	assert.Nil(t, disc)
}

func TestResolvePeerDiscovery_PluginRefNotFound(t *testing.T) {
	r := &Runner{PluginHandle: fwkplugin.NewEppHandle(context.Background(), nil)}
	_, err := r.resolvePeerDiscovery(peerDiscoveryConfig("my-disc", "nonexistent"))
	assert.ErrorContains(t, err, "nonexistent")
}

func TestResolvePeerDiscovery_NotPeerDiscovery(t *testing.T) {
	r := &Runner{PluginHandle: newHandleWithPlugin(t, "not-peer", &notDiscoveryPlugin{})}
	_, err := r.resolvePeerDiscovery(peerDiscoveryConfig("my-disc", "not-peer"))
	assert.ErrorContains(t, err, "not-peer")
	assert.ErrorContains(t, err, "PeerDiscovery")
}

func TestValidatePeerDiscoverySelection(t *testing.T) {
	tests := []struct {
		name       string
		enableFlag bool
		discovery  string
		peer       string
		wantErr    string
	}{
		{name: "no peer discovery is valid", discovery: "my-disc"},
		{name: "file peer discovery with file discovery is valid", discovery: "my-disc", peer: "my-peers"},
		{
			name:       "flag and plugin are mutually exclusive",
			enableFlag: true,
			discovery:  "my-disc",
			peer:       "my-peers",
			wantErr:    "mutually exclusive",
		},
		{
			name:    "file peer discovery requires file discovery",
			peer:    "my-peers",
			wantErr: "requires dataLayer.discovery",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &runserver.Options{EnablePeerDiscovery: tt.enableFlag}
			err := validatePeerDiscoverySelection(opts, peerDiscoveryConfig(tt.discovery, tt.peer))
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
