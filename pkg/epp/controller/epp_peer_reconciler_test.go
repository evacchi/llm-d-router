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

package controller

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fwkdl "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
)

const (
	testPeerNS  = "eppns"
	testPeerSvc = "epp"
)

type recordingNotifier struct {
	upserts []fwkdl.PeerMetadata
	deletes []types.NamespacedName
}

func (r *recordingNotifier) Upsert(p *fwkdl.PeerMetadata)   { r.upserts = append(r.upserts, *p) }
func (r *recordingNotifier) Delete(id types.NamespacedName) { r.deletes = append(r.deletes, id) }
func (r *recordingNotifier) reset()                         { r.upserts, r.deletes = nil, nil }

func readyEndpoint(addr, podName string) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{
		Addresses:  []string{addr},
		Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
		TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: podName, Namespace: testPeerNS},
	}
}

func peerSlice(name string, port int32, eps ...discoveryv1.Endpoint) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testPeerNS,
			Labels:    map[string]string{discoveryv1.LabelServiceName: testPeerSvc},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       []discoveryv1.EndpointPort{{Port: ptr.To(port)}},
		Endpoints:   eps,
	}
}

func nn(ns, name string) types.NamespacedName {
	return types.NamespacedName{Namespace: ns, Name: name}
}

func TestEPPPeerReconciler(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)

	notReady := discoveryv1.Endpoint{
		Addresses:  []string{"10.0.0.9"},
		Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(false)},
		TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: "epp-notready", Namespace: testPeerNS},
	}
	// A slice for a different service must be ignored.
	otherSvc := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other",
			Namespace: testPeerNS,
			Labels:    map[string]string{discoveryv1.LabelServiceName: "not-epp"},
		},
		Ports:     []discoveryv1.EndpointPort{{Port: ptr.To(int32(8000))}},
		Endpoints: []discoveryv1.Endpoint{readyEndpoint("10.9.9.9", "other-pod")},
	}

	sliceA := peerSlice("epp-a", 9010,
		readyEndpoint("10.0.0.1", "epp-0"), // self, excluded
		readyEndpoint("10.0.0.2", "epp-1"),
		notReady,
	)
	sliceB := peerSlice("epp-b", 9010, readyEndpoint("10.0.0.3", "epp-2"))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(sliceA, sliceB, otherSvc).
		Build()

	notifier := &recordingNotifier{}
	r := &EPPPeerReconciler{
		Reader:      fakeClient,
		Notifier:    notifier,
		ServiceName: testPeerSvc,
		Namespace:   testPeerNS,
		SelfAddress: "10.0.0.1",
	}

	// First reconcile: the ready, non-self peers across both slices are upserted.
	if _, err := r.Reconcile(context.Background(), ctrl.Request{}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	wantUpserts := []fwkdl.PeerMetadata{
		{ID: nn(testPeerNS, "epp-1"), Address: "10.0.0.2", Port: "9010"},
		{ID: nn(testPeerNS, "epp-2"), Address: "10.0.0.3", Port: "9010"},
	}
	if diff := cmp.Diff(wantUpserts, notifier.upserts, sortPeers); diff != "" {
		t.Errorf("upserts mismatch (-want +got):\n%s", diff)
	}
	if len(notifier.deletes) != 0 {
		t.Errorf("unexpected deletes: %v", notifier.deletes)
	}

	// Second reconcile with no changes: nothing is re-emitted.
	notifier.reset()
	if _, err := r.Reconcile(context.Background(), ctrl.Request{}); err != nil {
		t.Fatalf("Reconcile (no change): %v", err)
	}
	if len(notifier.upserts) != 0 || len(notifier.deletes) != 0 {
		t.Errorf("no-change reconcile emitted upserts=%v deletes=%v", notifier.upserts, notifier.deletes)
	}

	// Remove sliceB: its peer is deleted, survivors are not re-upserted.
	notifier.reset()
	if err := fakeClient.Delete(context.Background(), sliceB); err != nil {
		t.Fatalf("delete sliceB: %v", err)
	}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{}); err != nil {
		t.Fatalf("Reconcile (after delete): %v", err)
	}
	if len(notifier.upserts) != 0 {
		t.Errorf("unexpected upserts after delete: %v", notifier.upserts)
	}
	wantDeletes := []types.NamespacedName{nn(testPeerNS, "epp-2")}
	if diff := cmp.Diff(wantDeletes, notifier.deletes); diff != "" {
		t.Errorf("deletes mismatch (-want +got):\n%s", diff)
	}
}

// TestEPPPeerReconciler_FallbackID covers the identity fallback when an endpoint
// has no TargetRef.
func TestEPPPeerReconciler_FallbackID(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)

	ep := discoveryv1.Endpoint{
		Addresses:  []string{"10.0.0.5"},
		Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(peerSlice("epp-a", 9010, ep)).
		Build()

	notifier := &recordingNotifier{}
	r := &EPPPeerReconciler{
		Reader:      fakeClient,
		Notifier:    notifier,
		ServiceName: testPeerSvc,
		Namespace:   testPeerNS,
	}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	want := []fwkdl.PeerMetadata{{ID: nn(testPeerNS, "10.0.0.5"), Address: "10.0.0.5", Port: "9010"}}
	if diff := cmp.Diff(want, notifier.upserts, sortPeers); diff != "" {
		t.Errorf("upserts mismatch (-want +got):\n%s", diff)
	}
}

var sortPeers = cmpopts.SortSlices(func(a, b fwkdl.PeerMetadata) bool {
	return a.ID.String() < b.ID.String()
})
