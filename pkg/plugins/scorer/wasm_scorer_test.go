package scorer

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/gateway-api-inference-extension/pkg/epp/backend"
	backendmetrics "sigs.k8s.io/gateway-api-inference-extension/pkg/epp/backend/metrics"
	"sigs.k8s.io/gateway-api-inference-extension/pkg/epp/scheduling/types"
)

func TestWasmScorer_Score(t *testing.T) {
	podA := &types.PodMetrics{
		Pod: &backend.Pod{NamespacedName: k8stypes.NamespacedName{Name: "pod-a", Namespace: "default"}},
		MetricsState: &backendmetrics.MetricsState{
			WaitingQueueSize: 2,
		},
	}
	podB := &types.PodMetrics{
		Pod: &backend.Pod{NamespacedName: k8stypes.NamespacedName{Name: "pod-b", Namespace: "default"}},
		MetricsState: &backendmetrics.MetricsState{
			WaitingQueueSize: 0,
		},
	}
	podC := &types.PodMetrics{
		Pod: &backend.Pod{NamespacedName: k8stypes.NamespacedName{Name: "pod-c", Namespace: "default"}},
		MetricsState: &backendmetrics.MetricsState{
			WaitingQueueSize: 15,
		},
	}

	tests := []struct {
		name       string
		input      []types.Pod
		wantScores map[types.Pod]float64
	}{
		{
			name:  "score all pods with WASM module",
			input: []types.Pod{podA, podB, podC},
			wantScores: map[types.Pod]float64{
				podA: 1.0,
				podB: 1.0,
				podC: 1.0,
			},
		},
		{
			name:  "score two pods",
			input: []types.Pod{podA, podB},
			wantScores: map[types.Pod]float64{
				podA: 1.0,
				podB: 1.0,
			},
		},
		{
			name:       "empty pods list",
			input:      []types.Pod{},
			wantScores: map[types.Pod]float64{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scorer := NewWasmScorer(context.Background(), nil)

			got := scorer.Score(context.Background(), nil, nil, test.input)

			if diff := cmp.Diff(test.wantScores, got); diff != "" {
				t.Errorf("Unexpected output (-want +got): %v", diff)
			}
		})
	}
}

func TestWasmScorer_TypedName(t *testing.T) {
	scorer := NewWasmScorer(context.Background(), nil)

	typedName := scorer.TypedName()
	if typedName.Type != WasmScorerType {
		t.Errorf("Expected type %s, got %s", WasmScorerType, typedName.Type)
	}
}

func TestWasmScorer_WithName(t *testing.T) {
	scorer := NewWasmScorer(context.Background(), nil)
	testName := "test-wasm-scorer"

	scorer = scorer.WithName(testName)

	if scorer.TypedName().Name != testName {
		t.Errorf("Expected name %s, got %s", testName, scorer.TypedName().Name)
	}
}
