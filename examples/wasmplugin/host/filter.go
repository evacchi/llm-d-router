package host

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
)

const WasmFilterType = "wasm-filter"

var _ scheduling.Filter = &WasmFilter{}

type wasmFilterParams struct {
	Module             string `json:"module,omitempty"`
	PlainHTTP          bool   `json:"plainHTTP,omitempty"`
	ConfigMapName      string `json:"configMapName,omitempty"`
	ConfigMapNamespace string `json:"configMapNamespace,omitempty"`
	ConfigMapKey       string `json:"configMapKey,omitempty"`
}

// WasmFilterFactory creates a WasmFilter by loading and compiling a Wasm module.
func WasmFilterFactory(name string, rawParameters *json.Decoder, handle plugin.Handle) (plugin.Plugin, error) {
	var params wasmFilterParams
	if rawParameters == nil {
		return nil, fmt.Errorf("%s: parameters are required", WasmFilterType)
	}
	if err := rawParameters.Decode(&params); err != nil {
		return nil, fmt.Errorf("%s: failed to parse parameters: %w", WasmFilterType, err)
	}

	ctx := context.Background()
	if handle != nil {
		ctx = handle.Context()
	}

	f := &WasmFilter{
		typedName: plugin.TypedName{Type: WasmFilterType, Name: name},
	}

	if params.ConfigMapName != "" {
		ref := configMapRef{
			Namespace: params.ConfigMapNamespace,
			Name:      params.ConfigMapName,
			Key:       params.ConfigMapKey,
		}
		if ref.Key == "" {
			ref.Key = "config.json"
		}
		if ref.Namespace == "" {
			ref.Namespace = "default"
		}
		go watchConfigMap(ctx, ref, func(cfg moduleConfig) error {
			return f.loadAndSwap(ctx, cfg.Module, cfg.PlainHTTP)
		}, log.FromContext(ctx))
	} else {
		if params.Module == "" {
			return nil, fmt.Errorf("%s: 'module' or 'configMapName' parameter is required", WasmFilterType)
		}
		if err := f.loadAndSwap(ctx, params.Module, params.PlainHTTP); err != nil {
			return nil, fmt.Errorf("%s: %w", WasmFilterType, err)
		}
	}

	return f, nil
}

// WasmFilter implements scheduling.Filter by delegating to a Wasm module.
type WasmFilter struct {
	typedName plugin.TypedName
	compiled  atomic.Pointer[CompiledPlugin]
}

func (f *WasmFilter) loadAndSwap(ctx context.Context, module string, plainHTTP bool) error {
	wasmBytes, err := LoadModule(ctx, module, plainHTTP)
	if err != nil {
		return fmt.Errorf("loading module %q: %w", module, err)
	}
	newCompiled, err := NewCompiledPlugin(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("compiling module %q: %w", module, err)
	}
	old := f.compiled.Swap(newCompiled)
	if old != nil {
		go func() {
			time.Sleep(5 * time.Second)
			old.Close(context.Background()) //nolint:errcheck
		}()
	}
	return nil
}

func (f *WasmFilter) TypedName() plugin.TypedName {
	return f.typedName
}

func (f *WasmFilter) Filter(ctx context.Context, request *scheduling.InferenceRequest, endpoints []scheduling.Endpoint) []scheduling.Endpoint {
	logger := log.FromContext(ctx)
	compiled := f.compiled.Load()
	if compiled == nil {
		logger.Error(nil, "wasm filter: no compiled module available, passing through")
		return endpoints
	}

	input := ABIFilterInput{
		Request:   toABIRequest(request),
		Endpoints: toABIEndpoints(endpoints),
	}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		logger.Error(err, "wasm filter: failed to marshal input")
		return endpoints
	}

	inst, err := compiled.getInstance(ctx)
	if err != nil {
		logger.Error(err, "wasm filter: failed to get instance")
		return endpoints
	}
	defer compiled.putInstance(inst)

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
