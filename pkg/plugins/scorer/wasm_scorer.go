package scorer

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/gateway-api-inference-extension/pkg/epp/plugins"
	"sigs.k8s.io/gateway-api-inference-extension/pkg/epp/scheduling/framework"
	"sigs.k8s.io/gateway-api-inference-extension/pkg/epp/scheduling/types"
	logutil "sigs.k8s.io/gateway-api-inference-extension/pkg/epp/util/logging"
)

const (
	// WasmScorerType is the type of the WASM scorer.
	WasmScorerType = "wasm-scorer"
)

//go:embed plugins/scorer/scorer.wasm
var scorerWasm []byte

// WasmScorerParameters defines the parameters for the WASM scorer.
type WasmScorerParameters struct {
	// Future: allow custom WASM module paths
}

// ScoringInput represents the complete input data for the WASM scorer
type ScoringInput struct {
	Request *RequestData      `json:"request,omitempty"`
	Pods    []PodData         `json:"pods"`
}

// RequestData represents LLM request information for WASM
type RequestData struct {
	RequestID   string `json:"request_id"`
	TargetModel string `json:"target_model"`
	// Data contains the request-body fields that we parse out as user input.
	Body *types.LLMRequestBody `json:"body,omitempty"`
	// Headers is a map of the request headers.
	Headers map[string]string `json:"headers,omitempty"`
}

// PodData represents the input pod information for WASM
type PodData struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Address   string            `json:"address"`
	Port      string            `json:"port"`
	Labels    map[string]string `json:"labels"`
}

// ScoredPod represents a pod with its score from WASM
type ScoredPod struct {
	Name      string  `json:"name"`
	Namespace string  `json:"namespace"`
	Score     float64 `json:"score"`
}

// compile-time type assertion
var _ framework.Scorer = &WasmScorer{}

// WasmScorerFactory defines the factory function for the WASM scorer.
func WasmScorerFactory(name string, rawParameters json.RawMessage, handle plugins.Handle) (plugins.Plugin, error) {
	parameters := WasmScorerParameters{}
	if rawParameters != nil {
		if err := json.Unmarshal(rawParameters, &parameters); err != nil {
			return nil, fmt.Errorf("failed to parse the parameters of the '%s' scorer - %w", WasmScorerType, err)
		}
	}

	return NewWasmScorer(handle.Context(), &parameters).WithName(name), nil
}

// NewWasmScorer creates a new WASM scorer.
func NewWasmScorer(ctx context.Context, params *WasmScorerParameters) *WasmScorer {
	return &WasmScorer{
		typedName: plugins.TypedName{Type: WasmScorerType},
		ctx:       ctx,
	}
}

// WasmScorer is a scorer that delegates to a WASM module.
type WasmScorer struct {
	typedName plugins.TypedName
	ctx       context.Context
}

// TypedName returns the typed name of the plugin.
func (s *WasmScorer) TypedName() plugins.TypedName {
	return s.typedName
}

// WithName sets the name of the plugin.
func (s *WasmScorer) WithName(name string) *WasmScorer {
	s.typedName.Name = name
	return s
}

// Score calls the WASM module to score pods.
func (s *WasmScorer) Score(ctx context.Context, cycleState *types.CycleState, request *types.LLMRequest,
	pods []types.Pod) map[types.Pod]float64 {

	logger := log.FromContext(ctx).V(logutil.DEBUG)

	// Convert pods to WASM-friendly format
	podDataList := make([]PodData, len(pods))
	for i, pod := range pods {
		podDataList[i] = PodData{
			Name:      pod.GetPod().NamespacedName.Name,
			Namespace: pod.GetPod().NamespacedName.Namespace,
			Address:   pod.GetPod().Address,
			Port:      pod.GetPod().Port,
			Labels:    pod.GetPod().Labels,
		}
	}

	// Build request data
	var requestData *RequestData
	if request != nil {
		requestData = &RequestData{
			RequestID:   request.RequestId,
			TargetModel: request.TargetModel,
			Body:        request.Body,
			Headers:     request.Headers,
		}
	}

	// Build scoring input
	scoringInput := ScoringInput{
		Request: requestData,
		Pods:    podDataList,
	}

	// Marshal input
	inputJSON, err := json.Marshal(scoringInput)
	if err != nil {
		logger.Error(err, "Failed to marshal scoring input for WASM")
		return make(map[types.Pod]float64)
	}

	// Call WASM module
	scores, err := s.callWasm(inputJSON)
	if err != nil {
		logger.Error(err, "Failed to call WASM scorer")
		return make(map[types.Pod]float64)
	}

	// Map results back to pods
	scoredPods := make(map[types.Pod]float64, len(pods))
	scoreMap := make(map[string]float64)
	for _, scored := range scores {
		scoreMap[scored.Namespace+"/"+scored.Name] = scored.Score
	}

	for _, pod := range pods {
		key := pod.GetPod().NamespacedName.Namespace + "/" + pod.GetPod().NamespacedName.Name
		if score, ok := scoreMap[key]; ok {
			scoredPods[pod] = score
		} else {
			scoredPods[pod] = 0.0
		}
	}

	logger.Info("WASM scorer - scored pods", "podCount", len(pods))
	return scoredPods
}

// callWasm executes the embedded WASM scorer
func (s *WasmScorer) callWasm(inputJSON []byte) ([]ScoredPod, error) {
	// Create a new runtime
	runtime := wazero.NewRuntime(s.ctx)
	defer runtime.Close(s.ctx)

	// Instantiate WASI
	wasi_snapshot_preview1.MustInstantiate(s.ctx, runtime)

	// Compile the WASM module
	compiledModule, err := runtime.CompileModule(s.ctx, scorerWasm)
	if err != nil {
		return nil, fmt.Errorf("failed to compile WASM module: %w", err)
	}
	defer compiledModule.Close(s.ctx)

	// Instantiate the module
	module, err := runtime.InstantiateModule(s.ctx, compiledModule, wazero.NewModuleConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate WASM module: %w", err)
	}
	defer module.Close(s.ctx)

	// Get the allocate and score functions
	allocateFn := module.ExportedFunction("allocate")
	scoreFn := module.ExportedFunction("score")
	if allocateFn == nil || scoreFn == nil {
		return nil, fmt.Errorf("WASM module missing required exports")
	}

	// Allocate memory in WASM for input
	inputSize := uint64(len(inputJSON))
	results, err := allocateFn.Call(s.ctx, inputSize)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate memory: %w", err)
	}
	inputPtr := uint32(results[0])

	// Write input to WASM memory
	if !module.Memory().Write(inputPtr, inputJSON) {
		return nil, fmt.Errorf("failed to write input to WASM memory")
	}

	// Call the score function
	results, err = scoreFn.Call(s.ctx, uint64(inputPtr), inputSize)
	if err != nil {
		return nil, fmt.Errorf("failed to call score function: %w", err)
	}

	// Extract result pointer and size from packed uint64
	packed := results[0]
	resultPtr := uint32(packed >> 32)
	resultSize := uint32(packed & 0xFFFFFFFF)

	// Read result from WASM memory
	resultJSON, ok := module.Memory().Read(resultPtr, resultSize)
	if !ok {
		return nil, fmt.Errorf("failed to read result from WASM memory")
	}

	// Unmarshal result
	var scoredPods []ScoredPod
	if err := json.Unmarshal(resultJSON, &scoredPods); err != nil {
		return nil, fmt.Errorf("failed to unmarshal WASM result: %w", err)
	}

	return scoredPods, nil
}
