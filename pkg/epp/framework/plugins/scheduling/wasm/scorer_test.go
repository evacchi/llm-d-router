package wasm

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	fwkdl "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
	fwksched "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
)

func TestWasmScorer(t *testing.T) {
	wasmBytes, err := os.ReadFile("testdata/queue-scorer.wasm")
	require.NoError(t, err)

	ctx := context.Background()
	compiled, err := NewCompiledPlugin(ctx, wasmBytes)
	require.NoError(t, err)
	defer compiled.Close(ctx) //nolint:errcheck

	s := &WasmScorer{compiled: compiled, category: fwksched.Distribution}

	endpoints := []fwksched.Endpoint{
		fwksched.NewEndpoint(
			&fwkdl.EndpointMetadata{
				NamespacedName: types.NamespacedName{Namespace: "ns", Name: "pod-1"},
				Address:        "10.0.0.1",
				Port:           "8080",
			},
			&fwkdl.Metrics{WaitingQueueSize: 10},
			nil,
		),
		fwksched.NewEndpoint(
			&fwkdl.EndpointMetadata{
				NamespacedName: types.NamespacedName{Namespace: "ns", Name: "pod-2"},
				Address:        "10.0.0.2",
				Port:           "8080",
			},
			&fwkdl.Metrics{WaitingQueueSize: 0},
			nil,
		),
	}

	req := &fwksched.InferenceRequest{RequestID: "req-1", TargetModel: "llama"}
	scores := s.Score(ctx, req, endpoints)

	require.Len(t, scores, 2)
	assert.InDelta(t, 0.0, scores[endpoints[0]], 0.001, "pod with queue=10 should score 0")
	assert.InDelta(t, 1.0, scores[endpoints[1]], 0.001, "pod with queue=0 should score 1")
}

func TestWasmScorerEqualQueues(t *testing.T) {
	wasmBytes, err := os.ReadFile("testdata/queue-scorer.wasm")
	require.NoError(t, err)

	ctx := context.Background()
	compiled, err := NewCompiledPlugin(ctx, wasmBytes)
	require.NoError(t, err)
	defer compiled.Close(ctx) //nolint:errcheck

	s := &WasmScorer{compiled: compiled, category: fwksched.Distribution}

	endpoints := []fwksched.Endpoint{
		fwksched.NewEndpoint(
			&fwkdl.EndpointMetadata{
				NamespacedName: types.NamespacedName{Namespace: "ns", Name: "pod-1"},
				Address:        "10.0.0.1",
				Port:           "8080",
			},
			&fwkdl.Metrics{WaitingQueueSize: 0},
			nil,
		),
		fwksched.NewEndpoint(
			&fwkdl.EndpointMetadata{
				NamespacedName: types.NamespacedName{Namespace: "ns", Name: "pod-2"},
				Address:        "10.0.0.2",
				Port:           "8080",
			},
			&fwkdl.Metrics{WaitingQueueSize: 0},
			nil,
		),
	}

	req := &fwksched.InferenceRequest{RequestID: "req-2", TargetModel: "llama"}
	scores := s.Score(ctx, req, endpoints)

	require.Len(t, scores, 2)
	assert.InDelta(t, 1.0, scores[endpoints[0]], 0.001)
	assert.InDelta(t, 1.0, scores[endpoints[1]], 0.001)
}

func TestWasmScorerCategory(t *testing.T) {
	s := &WasmScorer{category: fwksched.Affinity}
	assert.Equal(t, fwksched.Affinity, s.Category())
}
