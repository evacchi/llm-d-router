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

func newTestFilter(t *testing.T, wasmFile string) *WasmFilter {
	t.Helper()
	wasmBytes, err := os.ReadFile(wasmFile)
	require.NoError(t, err)
	ctx := context.Background()
	compiled, err := NewCompiledPlugin(ctx, wasmBytes)
	require.NoError(t, err)
	t.Cleanup(func() { compiled.Close(ctx) }) //nolint:errcheck
	f := &WasmFilter{}
	f.compiled.Store(compiled)
	return f
}

func TestWasmFilter(t *testing.T) {
	f := newTestFilter(t, "testdata/label-filter.wasm")

	endpoints := []fwksched.Endpoint{
		fwksched.NewEndpoint(
			&fwkdl.EndpointMetadata{
				NamespacedName: types.NamespacedName{Namespace: "ns", Name: "pod-a100"},
				Address:        "10.0.0.1",
				Port:           "8080",
				Labels:         map[string]string{"gpu-type": "a100"},
			},
			fwkdl.NewMetrics(), nil,
		),
		fwksched.NewEndpoint(
			&fwkdl.EndpointMetadata{
				NamespacedName: types.NamespacedName{Namespace: "ns", Name: "pod-h100"},
				Address:        "10.0.0.2",
				Port:           "8080",
				Labels:         map[string]string{"gpu-type": "h100"},
			},
			fwkdl.NewMetrics(), nil,
		),
		fwksched.NewEndpoint(
			&fwkdl.EndpointMetadata{
				NamespacedName: types.NamespacedName{Namespace: "ns", Name: "pod-a100-2"},
				Address:        "10.0.0.3",
				Port:           "8080",
				Labels:         map[string]string{"gpu-type": "a100"},
			},
			fwkdl.NewMetrics(), nil,
		),
	}

	req := &fwksched.InferenceRequest{RequestID: "req-1", TargetModel: "llama"}
	result := f.Filter(context.Background(), req, endpoints)

	require.Len(t, result, 2)
	assert.Equal(t, "pod-a100", result[0].GetMetadata().NamespacedName.Name)
	assert.Equal(t, "pod-a100-2", result[1].GetMetadata().NamespacedName.Name)
}

func TestWasmFilterEmptyWhenNoMatch(t *testing.T) {
	f := newTestFilter(t, "testdata/label-filter.wasm")

	endpoints := []fwksched.Endpoint{
		fwksched.NewEndpoint(
			&fwkdl.EndpointMetadata{
				NamespacedName: types.NamespacedName{Namespace: "ns", Name: "pod-h100"},
				Address:        "10.0.0.1",
				Port:           "8080",
				Labels:         map[string]string{"gpu-type": "h100"},
			},
			fwkdl.NewMetrics(), nil,
		),
	}

	req := &fwksched.InferenceRequest{RequestID: "req-2", TargetModel: "llama"}
	result := f.Filter(context.Background(), req, endpoints)

	assert.Empty(t, result, "filter should return empty when no a100 match")
}
