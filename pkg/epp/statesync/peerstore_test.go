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

package statesync

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/types"

	fwkdl "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
)

func peer(ns, name, addr string) fwkdl.PeerMetadata {
	return fwkdl.PeerMetadata{ID: types.NamespacedName{Namespace: ns, Name: name}, Address: addr, Port: "9010"}
}

func TestMemoryPeerStore(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryPeerStore()

	if got := s.Peers(); len(got) != 0 {
		t.Fatalf("empty store Peers() = %v, want empty", got)
	}

	b := peer("ns", "b", "10.0.0.2")
	a := peer("ns", "a", "10.0.0.1")
	s.PeerUpsert(ctx, &b)
	s.PeerUpsert(ctx, &a)

	// Peers is ordered by ID for deterministic consumption.
	want := []fwkdl.PeerMetadata{a, b}
	if got := s.Peers(); !cmp.Equal(got, want) {
		t.Errorf("Peers() = %v, want %v", got, want)
	}

	// Upsert replaces by ID.
	aUpdated := peer("ns", "a", "10.0.0.9")
	s.PeerUpsert(ctx, &aUpdated)
	if got := s.Peers(); !cmp.Equal(got, []fwkdl.PeerMetadata{aUpdated, b}) {
		t.Errorf("after update Peers() = %v, want a replaced", got)
	}

	s.PeerDelete(a.ID)
	if got := s.Peers(); !cmp.Equal(got, []fwkdl.PeerMetadata{b}) {
		t.Errorf("after delete Peers() = %v, want only b", got)
	}

	// Deleting an absent peer is a no-op.
	s.PeerDelete(types.NamespacedName{Namespace: "ns", Name: "missing"})
	if got := s.Peers(); len(got) != 1 {
		t.Errorf("after no-op delete Peers() = %v, want 1", got)
	}
}
