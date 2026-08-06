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

package epp

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/llm-d/llm-d-router/pkg/epp/controller"
	fwkdl "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
	"github.com/llm-d/llm-d-router/pkg/epp/statesync"
)

const testPeerService = "epp-peers"

// TestIntegrationEPPPeerDiscovery exercises the peer reconciler against a real
// apiserver: the manager, informer, predicate, and namespace-scoped cache all
// run, so it covers the watch/list path the unit tests bypass. EndpointSlices
// are created by hand because envtest has no kube-controller-manager to generate
// them.
func TestIntegrationEPPPeerDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short runMode")
	}

	uid := uuid.New().String()[:8]
	nsName := "epp-peer-test-" + uid
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	require.NoError(t, k8sClient.Create(context.Background(), ns))
	t.Cleanup(func() { _ = k8sClient.Delete(context.Background(), ns) })

	mgr, mgrClient := setupTestManager(t, testEnv.Config, nsName)
	store := statesync.NewMemoryPeerStore()
	r := &controller.EPPPeerReconciler{
		Reader:      mgr.GetClient(),
		Notifier:    fwkdl.NewPeerNotifier(store),
		ServiceName: testPeerService,
		Namespace:   nsName,
		SelfAddress: "10.0.0.1", // self, excluded from the peer set
	}
	require.NoError(t, r.SetupWithManager(mgr))

	ctx, cancel := context.WithTimeout(context.Background(), testContextTimeout)
	t.Cleanup(cancel)
	startManagerAndWaitForSync(ctx, t, mgr)

	peerAddrs := func() []string {
		peers := store.Peers()
		out := make([]string, 0, len(peers))
		for _, p := range peers {
			out = append(out, p.Address)
		}
		sort.Strings(out)
		return out
	}
	eventuallyPeers := func(want ...string) {
		t.Helper()
		require.Eventually(t, func() bool { return equalStrings(peerAddrs(), want) },
			eventWaitTimeout, eventPollInterval, "want peers %v", want)
	}

	// A slice with this replica (excluded) plus one ready peer.
	sliceA := peerEndpointSlice("epp-a", nsName,
		readyPeerEndpoint("10.0.0.1", "epp-0", nsName),
		readyPeerEndpoint("10.0.0.2", "epp-1", nsName),
	)
	require.NoError(t, mgrClient.Create(ctx, sliceA))
	eventuallyPeers("10.0.0.2")

	// A second slice adds another peer.
	sliceB := peerEndpointSlice("epp-b", nsName, readyPeerEndpoint("10.0.0.3", "epp-2", nsName))
	require.NoError(t, mgrClient.Create(ctx, sliceB))
	eventuallyPeers("10.0.0.2", "10.0.0.3")

	// Marking a peer NotReady drops it.
	var latest discoveryv1.EndpointSlice
	require.NoError(t, mgrClient.Get(ctx, client.ObjectKeyFromObject(sliceA), &latest))
	latest.Endpoints[1].Conditions.Ready = ptr.To(false)
	require.NoError(t, mgrClient.Update(ctx, &latest))
	eventuallyPeers("10.0.0.3")

	// Deleting a slice removes its peers.
	require.NoError(t, mgrClient.Delete(ctx, sliceB))
	eventuallyPeers()
}

func peerEndpointSlice(name, ns string, eps ...discoveryv1.Endpoint) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{discoveryv1.LabelServiceName: testPeerService},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       []discoveryv1.EndpointPort{{Port: ptr.To(int32(9002))}},
		Endpoints:   eps,
	}
}

func readyPeerEndpoint(addr, podName, ns string) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{
		Addresses:  []string{addr},
		Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
		TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: podName, Namespace: ns},
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
