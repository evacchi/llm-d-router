package scorer

import (
	"context"
	"encoding/json"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/gateway-api-inference-extension/pkg/epp/plugins"
	"sigs.k8s.io/gateway-api-inference-extension/pkg/epp/scheduling/framework"
	"sigs.k8s.io/gateway-api-inference-extension/pkg/epp/scheduling/types"
	logutil "sigs.k8s.io/gateway-api-inference-extension/pkg/epp/util/logging"
)

const (
	// HelloWorldType is the type of the HelloWorld scorer.
	HelloWorldType = "hello-world-scorer"
)

// HelloWorldParameters defines the parameters for the HelloWorld scorer.
type HelloWorldParameters struct {
	// Score is the fixed score to return for all pods.
	Score float64 `json:"score"`
}

// compile-time type assertion
var _ framework.Scorer = &HelloWorld{}

// HelloWorldFactory defines the factory function for the HelloWorld scorer.
func HelloWorldFactory(name string, rawParameters json.RawMessage, handle plugins.Handle) (plugins.Plugin, error) {
	parameters := HelloWorldParameters{Score: 1.0} // default score
	if rawParameters != nil {
		if err := json.Unmarshal(rawParameters, &parameters); err != nil {
			return nil, fmt.Errorf("failed to parse the parameters of the '%s' scorer - %w", HelloWorldType, err)
		}
	}

	return NewHelloWorld(&parameters).WithName(name), nil
}

// NewHelloWorld creates a new HelloWorld scorer.
func NewHelloWorld(params *HelloWorldParameters) *HelloWorld {
	score := 1.0
	if params != nil {
		score = params.Score
	}

	return &HelloWorld{
		typedName: plugins.TypedName{Type: HelloWorldType},
		score:     score,
	}
}

// HelloWorld is a simple scorer that returns a fixed score for all pods.
// This is a minimal example scorer for demonstration purposes.
type HelloWorld struct {
	typedName plugins.TypedName
	score     float64
}

// TypedName returns the typed name of the plugin.
func (s *HelloWorld) TypedName() plugins.TypedName {
	return s.typedName
}

// WithName sets the name of the plugin.
func (s *HelloWorld) WithName(name string) *HelloWorld {
	s.typedName.Name = name
	return s
}

// Score returns a fixed score for all pods.
func (s *HelloWorld) Score(ctx context.Context, _ *types.CycleState, _ *types.LLMRequest,
	pods []types.Pod) map[types.Pod]float64 {
	scoredPods := make(map[types.Pod]float64, len(pods))
	for _, pod := range pods {
		scoredPods[pod] = s.score
	}

	log.FromContext(ctx).V(logutil.DEBUG).Info("Hello World scorer - scored pods",
		"score", s.score, "podCount", len(pods))
	return scoredPods
}
