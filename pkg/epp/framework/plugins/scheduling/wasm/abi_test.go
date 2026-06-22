package wasm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	fwkdl "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/datalayer"
	fwksched "github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
)

func TestToABIRequest(t *testing.T) {
	req := &fwksched.InferenceRequest{
		RequestID:        "req-1",
		TargetModel:      "llama-3",
		Headers:          map[string]string{"x-custom": "val"},
		RequestSizeBytes: 1024,
	}
	abi := toABIRequest(req)

	assert.Equal(t, "req-1", abi.RequestID)
	assert.Equal(t, "llama-3", abi.TargetModel)
	assert.Equal(t, "val", abi.Headers["x-custom"])
	assert.Equal(t, 1024, abi.RequestSizeBytes)
}

func TestToABIEndpoint(t *testing.T) {
	ep := fwksched.NewEndpoint(
		&fwkdl.EndpointMetadata{
			NamespacedName: types.NamespacedName{Namespace: "ns", Name: "pod-1"},
			Address:        "10.0.0.1",
			Port:           "8080",
			Labels:         map[string]string{"gpu-type": "a100"},
		},
		&fwkdl.Metrics{
			ActiveModels:        map[string]int{"llama": 1},
			RunningRequestsSize: 3,
			WaitingQueueSize:    5,
			KVCacheUsagePercent: 42.5,
		},
		nil,
	)
	abi := toABIEndpoint(ep)

	assert.Equal(t, "ns/pod-1", abi.ID)
	assert.Equal(t, "10.0.0.1", abi.Address)
	assert.Equal(t, "8080", abi.Port)
	assert.Equal(t, "a100", abi.Labels["gpu-type"])
	assert.Equal(t, 3, abi.Metrics.RunningRequestsSize)
	assert.Equal(t, 5, abi.Metrics.WaitingQueueSize)
	assert.InDelta(t, 42.5, abi.Metrics.KVCacheUsagePercent, 0.001)
}

func TestABIFilterRoundTrip(t *testing.T) {
	input := ABIFilterInput{
		Request: ABIRequest{RequestID: "r1", TargetModel: "m1"},
		Endpoints: []ABIEndpoint{
			{ID: "ns/pod-1", Address: "10.0.0.1", Port: "8080"},
		},
	}
	data, err := json.Marshal(input)
	require.NoError(t, err)

	var decoded ABIFilterInput
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, input, decoded)
}

func TestABIScorerRoundTrip(t *testing.T) {
	output := ABIScorerOutput{
		Scores: map[string]float64{"ns/pod-1": 0.75, "ns/pod-2": 0.25},
	}
	data, err := json.Marshal(output)
	require.NoError(t, err)

	var decoded ABIScorerOutput
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, output, decoded)
}
