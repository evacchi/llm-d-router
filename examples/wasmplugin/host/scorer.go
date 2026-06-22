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

const WasmScorerType = "wasm-scorer"

var _ scheduling.Scorer = &WasmScorer{}

type wasmScorerParams struct {
	Module             string                    `json:"module,omitempty"`
	Category           scheduling.ScorerCategory `json:"category,omitempty"`
	PlainHTTP          bool                      `json:"plainHTTP,omitempty"`
	ConfigMapName      string                    `json:"configMapName,omitempty"`
	ConfigMapNamespace string                    `json:"configMapNamespace,omitempty"`
	ConfigMapKey       string                    `json:"configMapKey,omitempty"`
}

// WasmScorerFactory creates a WasmScorer by loading and compiling a Wasm module.
func WasmScorerFactory(name string, rawParameters *json.Decoder, handle plugin.Handle) (plugin.Plugin, error) {
	var params wasmScorerParams
	if rawParameters == nil {
		return nil, fmt.Errorf("%s: parameters are required", WasmScorerType)
	}
	if err := rawParameters.Decode(&params); err != nil {
		return nil, fmt.Errorf("%s: failed to parse parameters: %w", WasmScorerType, err)
	}

	ctx := context.Background()
	if handle != nil {
		ctx = handle.Context()
	}

	s := &WasmScorer{
		typedName: plugin.TypedName{Type: WasmScorerType, Name: name},
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
			return s.loadAndSwap(ctx, cfg.Module, cfg.PlainHTTP)
		}, log.FromContext(ctx))
	} else {
		if params.Module == "" {
			return nil, fmt.Errorf("%s: 'module' or 'configMapName' parameter is required", WasmScorerType)
		}
		switch params.Category {
		case scheduling.Affinity, scheduling.Distribution, scheduling.Balance:
		default:
			return nil, fmt.Errorf("%s: invalid category %q", WasmScorerType, params.Category)
		}
		cat := params.Category
		s.category.Store(&cat)
		if err := s.loadAndSwap(ctx, params.Module, params.PlainHTTP); err != nil {
			return nil, fmt.Errorf("%s: %w", WasmScorerType, err)
		}
	}

	return s, nil
}

// WasmScorer implements scheduling.Scorer by delegating to a Wasm module.
type WasmScorer struct {
	typedName plugin.TypedName
	compiled  atomic.Pointer[CompiledPlugin]
	category  atomic.Pointer[scheduling.ScorerCategory]
}

func (s *WasmScorer) loadAndSwap(ctx context.Context, module string, plainHTTP bool) error {
	wasmBytes, err := LoadModule(ctx, module, plainHTTP)
	if err != nil {
		return fmt.Errorf("loading module %q: %w", module, err)
	}
	newCompiled, err := NewCompiledPlugin(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("compiling module %q: %w", module, err)
	}
	old := s.compiled.Swap(newCompiled)
	if old != nil {
		go func() {
			time.Sleep(5 * time.Second)
			old.Close(context.Background()) //nolint:errcheck
		}()
	}
	return nil
}

func (s *WasmScorer) TypedName() plugin.TypedName {
	return s.typedName
}

func (s *WasmScorer) Category() scheduling.ScorerCategory {
	if cat := s.category.Load(); cat != nil {
		return *cat
	}
	return scheduling.Distribution
}

func (s *WasmScorer) Score(ctx context.Context, request *scheduling.InferenceRequest, endpoints []scheduling.Endpoint) map[scheduling.Endpoint]float64 {
	logger := log.FromContext(ctx)
	compiled := s.compiled.Load()
	if compiled == nil {
		logger.Error(nil, "wasm scorer: no compiled module available")
		return nil
	}

	input := ABIScorerInput{
		Request:   toABIRequest(request),
		Endpoints: toABIEndpoints(endpoints),
	}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		logger.Error(err, "wasm scorer: failed to marshal input")
		return nil
	}

	inst, err := compiled.getInstance(ctx)
	if err != nil {
		logger.Error(err, "wasm scorer: failed to get instance")
		return nil
	}
	defer compiled.putInstance(inst)

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
