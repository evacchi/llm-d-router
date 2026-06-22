package host

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCallGuestFilter(t *testing.T) {
	wasmBytes, err := os.ReadFile("testdata/label-filter.wasm")
	require.NoError(t, err)

	ctx := context.Background()
	compiled, err := NewCompiledPlugin(ctx, wasmBytes)
	require.NoError(t, err)
	defer compiled.Close(ctx) //nolint:errcheck

	inst, err := compiled.getInstance(ctx)
	require.NoError(t, err)

	input := ABIFilterInput{
		Request: ABIRequest{RequestID: "r1", TargetModel: "m1"},
		Endpoints: []ABIEndpoint{
			{ID: "ns/pod-1", Labels: map[string]string{"gpu-type": "a100"}},
			{ID: "ns/pod-2", Labels: map[string]string{"gpu-type": "h100"}},
		},
	}
	inputBytes, err := json.Marshal(input)
	require.NoError(t, err)

	result, err := callGuest(ctx, inst, inst.filterFn, inputBytes)
	require.NoError(t, err)
	require.NotNil(t, result)

	var output ABIFilterOutput
	require.NoError(t, json.Unmarshal(result, &output))
	require.Equal(t, []string{"ns/pod-1"}, output.EndpointIDs)
}
