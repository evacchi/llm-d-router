package wasm

import (
	"context"
	"encoding/json"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
)

const WasmScorerType = "wasm-scorer"

var _ scheduling.Scorer = &WasmScorer{}

type wasmScorerParams struct {
	Module    string                   `json:"module"`
	Category  scheduling.ScorerCategory `json:"category"`
	PlainHTTP bool                     `json:"plainHTTP,omitempty"`
}

// WasmScorerFactory creates a WasmScorer by loading and compiling a Wasm module.
func WasmScorerFactory(name string, rawParameters *json.Decoder, handle plugin.Handle) (plugin.Plugin, error) {
	var params wasmScorerParams
	if rawParameters == nil {
		return nil, fmt.Errorf("%s: 'module' and 'category' parameters are required", WasmScorerType)
	}
	if err := rawParameters.Decode(&params); err != nil {
		return nil, fmt.Errorf("%s: failed to parse parameters: %w", WasmScorerType, err)
	}
	if params.Module == "" {
		return nil, fmt.Errorf("%s: 'module' parameter is required", WasmScorerType)
	}

	switch params.Category {
	case scheduling.Affinity, scheduling.Distribution, scheduling.Balance:
	default:
		return nil, fmt.Errorf("%s: invalid category %q (must be Affinity, Distribution, or Balance)", WasmScorerType, params.Category)
	}

	ctx := context.Background()
	if handle != nil {
		ctx = handle.Context()
	}

	wasmBytes, err := LoadModule(ctx, params.Module, params.PlainHTTP)
	if err != nil {
		return nil, fmt.Errorf("%s: loading module %q: %w", WasmScorerType, params.Module, err)
	}

	compiled, err := NewCompiledPlugin(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("%s: compiling module %q: %w", WasmScorerType, params.Module, err)
	}

	return &WasmScorer{
		typedName: plugin.TypedName{Type: WasmScorerType, Name: name},
		compiled:  compiled,
		category:  params.Category,
	}, nil
}

// WasmScorer implements scheduling.Scorer by delegating to a Wasm module.
type WasmScorer struct {
	typedName plugin.TypedName
	compiled  *CompiledPlugin
	category  scheduling.ScorerCategory
}

func (s *WasmScorer) TypedName() plugin.TypedName {
	return s.typedName
}

func (s *WasmScorer) Category() scheduling.ScorerCategory {
	return s.category
}

func (s *WasmScorer) Score(ctx context.Context, request *scheduling.InferenceRequest, endpoints []scheduling.Endpoint) map[scheduling.Endpoint]float64 {
	logger := log.FromContext(ctx)

	input := ABIScorerInput{
		Request:   toABIRequest(request),
		Endpoints: toABIEndpoints(endpoints),
	}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		logger.Error(err, "wasm scorer: failed to marshal input")
		return nil
	}

	inst, err := s.compiled.getInstance(ctx)
	if err != nil {
		logger.Error(err, "wasm scorer: failed to get instance")
		return nil
	}
	defer s.compiled.putInstance(inst)

	if inst.scoreFn == nil {
		logger.Error(nil, "wasm scorer: module does not export 'score'")
		return nil
	}

	resultBytes, err := callGuest(ctx, inst, inst.scoreFn, inputBytes)
	if err != nil {
		logger.Error(err, "wasm scorer: guest call failed")
		return nil
	}

	var output ABIScorerOutput
	if err := json.Unmarshal(resultBytes, &output); err != nil {
		logger.Error(err, "wasm scorer: failed to unmarshal output")
		return nil
	}

	epByID := make(map[string]scheduling.Endpoint, len(endpoints))
	for _, ep := range endpoints {
		epByID[ep.GetMetadata().NamespacedName.String()] = ep
	}

	scores := make(map[scheduling.Endpoint]float64, len(output.Scores))
	for id, score := range output.Scores {
		if ep, ok := epByID[id]; ok {
			scores[ep] = score
		}
	}
	return scores
}
