package wasm

import (
	"context"
	"encoding/json"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
)

const WasmFilterType = "wasm-filter"

var _ scheduling.Filter = &WasmFilter{}

type wasmFilterParams struct {
	Module    string `json:"module"`
	PlainHTTP bool   `json:"plainHTTP,omitempty"`
}

// WasmFilterFactory creates a WasmFilter by loading and compiling a Wasm module.
func WasmFilterFactory(name string, rawParameters *json.Decoder, handle plugin.Handle) (plugin.Plugin, error) {
	var params wasmFilterParams
	if rawParameters == nil {
		return nil, fmt.Errorf("%s: 'module' parameter is required", WasmFilterType)
	}
	if err := rawParameters.Decode(&params); err != nil {
		return nil, fmt.Errorf("%s: failed to parse parameters: %w", WasmFilterType, err)
	}
	if params.Module == "" {
		return nil, fmt.Errorf("%s: 'module' parameter is required", WasmFilterType)
	}

	ctx := context.Background()
	if handle != nil {
		ctx = handle.Context()
	}

	wasmBytes, err := LoadModule(ctx, params.Module, params.PlainHTTP)
	if err != nil {
		return nil, fmt.Errorf("%s: loading module %q: %w", WasmFilterType, params.Module, err)
	}

	compiled, err := NewCompiledPlugin(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("%s: compiling module %q: %w", WasmFilterType, params.Module, err)
	}

	return &WasmFilter{
		typedName: plugin.TypedName{Type: WasmFilterType, Name: name},
		compiled:  compiled,
	}, nil
}

// WasmFilter implements scheduling.Filter by delegating to a Wasm module.
type WasmFilter struct {
	typedName plugin.TypedName
	compiled  *CompiledPlugin
}

func (f *WasmFilter) TypedName() plugin.TypedName {
	return f.typedName
}

func (f *WasmFilter) Filter(ctx context.Context, request *scheduling.InferenceRequest, endpoints []scheduling.Endpoint) []scheduling.Endpoint {
	logger := log.FromContext(ctx)

	input := ABIFilterInput{
		Request:   toABIRequest(request),
		Endpoints: toABIEndpoints(endpoints),
	}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		logger.Error(err, "wasm filter: failed to marshal input")
		return endpoints
	}

	inst, err := f.compiled.getInstance(ctx)
	if err != nil {
		logger.Error(err, "wasm filter: failed to get instance")
		return endpoints
	}
	defer f.compiled.putInstance(inst)

	if inst.filterFn == nil {
		logger.Error(nil, "wasm filter: module does not export 'filter'")
		return endpoints
	}

	resultBytes, err := callGuest(ctx, inst, inst.filterFn, inputBytes)
	if err != nil {
		logger.Error(err, "wasm filter: guest call failed")
		return endpoints
	}

	var output ABIFilterOutput
	if err := json.Unmarshal(resultBytes, &output); err != nil {
		logger.Error(err, "wasm filter: failed to unmarshal output")
		return endpoints
	}

	keep := make(map[string]struct{}, len(output.EndpointIDs))
	for _, id := range output.EndpointIDs {
		keep[id] = struct{}{}
	}

	filtered := make([]scheduling.Endpoint, 0, len(output.EndpointIDs))
	for _, ep := range endpoints {
		id := ep.GetMetadata().NamespacedName.String()
		if _, ok := keep[id]; ok {
			filtered = append(filtered, ep)
		}
	}
	return filtered
}
