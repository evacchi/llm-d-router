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

func TestHelloWorldScorer_Score(t *testing.T) {
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
		score      float64
		input      []types.Pod
		wantScores map[types.Pod]float64
	}{
		{
			name:  "default score of 1.0 for all pods",
			score: 1.0,
			input: []types.Pod{podA, podB, podC},
			wantScores: map[types.Pod]float64{
				podA: 1.0,
				podB: 1.0,
				podC: 1.0,
			},
		},
		{
			name:  "custom score of 0.5 for all pods",
			score: 0.5,
			input: []types.Pod{podA, podB, podC},
			wantScores: map[types.Pod]float64{
				podA: 0.5,
				podB: 0.5,
				podC: 0.5,
			},
		},
		{
			name:  "score of 0.0 for all pods",
			score: 0.0,
			input: []types.Pod{podA, podB},
			wantScores: map[types.Pod]float64{
				podA: 0.0,
				podB: 0.0,
			},
		},
		{
			name:       "empty pods list",
			score:      1.0,
			input:      []types.Pod{},
			wantScores: map[types.Pod]float64{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := &HelloWorldParameters{Score: test.score}
			scorer := NewHelloWorld(params)

			got := scorer.Score(context.Background(), nil, nil, test.input)

			if diff := cmp.Diff(test.wantScores, got); diff != "" {
				t.Errorf("Unexpected output (-want +got): %v", diff)
			}
		})
	}
}

func TestHelloWorldScorer_TypedName(t *testing.T) {
	scorer := NewHelloWorld(nil)

	typedName := scorer.TypedName()
	if typedName.Type != HelloWorldType {
		t.Errorf("Expected type %s, got %s", HelloWorldType, typedName.Type)
	}
}

func TestHelloWorldScorer_WithName(t *testing.T) {
	scorer := NewHelloWorld(nil)
	testName := "test-hello-world"

	scorer = scorer.WithName(testName)

	if scorer.TypedName().Name != testName {
		t.Errorf("Expected name %s, got %s", testName, scorer.TypedName().Name)
	}
}

func TestNewHelloWorld_DefaultScore(t *testing.T) {
	scorer := NewHelloWorld(nil)

	if scorer.score != 1.0 {
		t.Errorf("Expected default score 1.0, got %f", scorer.score)
	}
}

func TestNewHelloWorld_CustomScore(t *testing.T) {
	params := &HelloWorldParameters{Score: 0.42}
	scorer := NewHelloWorld(params)

	if scorer.score != 0.42 {
		t.Errorf("Expected score 0.42, got %f", scorer.score)
	}
}
