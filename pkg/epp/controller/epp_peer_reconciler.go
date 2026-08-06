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
	"fmt"
	"strconv"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	logutil "github.com/llm-d/llm-d-router/pkg/common/observability/logging"
	fwkdl "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
)

// EPPPeerReconciler discovers peer EPP replicas by watching the EndpointSlices
// of the EPP's own Service and drives their membership through a PeerNotifier.
// Peer discovery must run on every replica (not just the leader), so leader
// election is disabled for its controller.
type EPPPeerReconciler struct {
	client.Reader
	// Notifier receives peer add/update/remove events.
	Notifier fwkdl.PeerNotifier
	// ServiceName is the EPP's own Service whose EndpointSlices enumerate peers.
	ServiceName string
	// Namespace is the namespace of the EPP Service and its EndpointSlices.
	Namespace string
	// SelfAddress is this replica's endpoint address, excluded from the peer
	// set. Empty includes all endpoints.
	SelfAddress string

	// prev is the last reported peer set, used to compute deletes. Access is
	// serialized by the controller (single concurrent reconcile).
	prev map[types.NamespacedName]fwkdl.PeerMetadata
}

func (r *EPPPeerReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// A Service can back multiple EndpointSlices; rebuild the full peer set from
	// all of them on every event and diff against the last reported set.
	var slices discoveryv1.EndpointSliceList
	if err := r.List(ctx, &slices,
		client.InNamespace(r.Namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: r.ServiceName},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing EndpointSlices for service %s/%s - %w", r.Namespace, r.ServiceName, err)
	}

	desired := r.desiredPeers(slices.Items)

	for id, peer := range desired {
		if existing, ok := r.prev[id]; !ok || existing != peer {
			p := peer
			r.Notifier.Upsert(&p)
		}
	}
	for id := range r.prev {
		if _, ok := desired[id]; !ok {
			r.Notifier.Delete(id)
		}
	}
	r.prev = desired

	logger.V(logutil.DEBUG).Info("Reconciled EPP peers", "service", r.ServiceName, "peers", len(desired))
	return ctrl.Result{}, nil
}

// desiredPeers folds the ready endpoints of the given slices into a peer set,
// keyed by peer identity and excluding this replica.
func (r *EPPPeerReconciler) desiredPeers(slices []discoveryv1.EndpointSlice) map[types.NamespacedName]fwkdl.PeerMetadata {
	desired := map[types.NamespacedName]fwkdl.PeerMetadata{}
	for i := range slices {
		slice := &slices[i]
		port := firstPort(slice.Ports)
		for j := range slice.Endpoints {
			ep := &slice.Endpoints[j]
			if !endpointReady(ep) || len(ep.Addresses) == 0 {
				continue
			}
			addr := ep.Addresses[0]
			if addr == r.SelfAddress {
				continue
			}
			id := peerID(ep, r.Namespace, addr)
			desired[id] = fwkdl.PeerMetadata{ID: id, Address: addr, Port: port}
		}
	}
	return desired
}

func (r *EPPPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	forThisService := func(obj client.Object) bool {
		return obj.GetNamespace() == r.Namespace &&
			obj.GetLabels()[discoveryv1.LabelServiceName] == r.ServiceName
	}
	// Peer discovery runs on every replica; the leader alone is not enough.
	needLeaderElection := false
	return ctrl.NewControllerManagedBy(mgr).
		For(&discoveryv1.EndpointSlice{}).
		WithEventFilter(predicate.NewPredicateFuncs(forThisService)).
		WithOptions(controller.Options{NeedLeaderElection: &needLeaderElection}).
		Complete(r)
}

// endpointReady reports whether an endpoint is serving. A nil Ready condition is
// treated as not ready.
func endpointReady(ep *discoveryv1.Endpoint) bool {
	return ep.Conditions.Ready != nil && *ep.Conditions.Ready
}

// peerID derives a stable identity for an endpoint, preferring its backing Pod
// reference and falling back to the address within the reconciled namespace.
func peerID(ep *discoveryv1.Endpoint, namespace, addr string) types.NamespacedName {
	if ref := ep.TargetRef; ref != nil && ref.Name != "" {
		ns := ref.Namespace
		if ns == "" {
			ns = namespace
		}
		return types.NamespacedName{Namespace: ns, Name: ref.Name}
	}
	return types.NamespacedName{Namespace: namespace, Name: addr}
}

// firstPort returns the first defined port as a string, or "" if none.
func firstPort(ports []discoveryv1.EndpointPort) string {
	for i := range ports {
		if ports[i].Port != nil {
			return strconv.Itoa(int(*ports[i].Port))
		}
	}
	return ""
}
