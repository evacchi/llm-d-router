package wasm

import (
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/scheduling"
)

// ABIRequest is a JSON-serializable projection of scheduling.InferenceRequest
// for the host-guest boundary.
type ABIRequest struct {
	RequestID        string            `json:"request_id"`
	TargetModel      string            `json:"target_model"`
	Headers          map[string]string `json:"headers,omitempty"`
	RequestSizeBytes int               `json:"request_size_bytes,omitempty"`
}

// ABIEndpoint is a JSON-serializable projection of scheduling.Endpoint.
type ABIEndpoint struct {
	ID      string            `json:"id"`
	Address string            `json:"address"`
	Port    string            `json:"port"`
	Labels  map[string]string `json:"labels,omitempty"`
	Metrics ABIMetrics        `json:"metrics"`
}

// ABIMetrics is a JSON-serializable projection of datalayer.Metrics.
type ABIMetrics struct {
	ActiveModels        map[string]int `json:"active_models,omitempty"`
	WaitingModels       map[string]int `json:"waiting_models,omitempty"`
	RunningRequestsSize int            `json:"running_requests_size"`
	WaitingQueueSize    int            `json:"waiting_queue_size"`
	KVCacheUsagePercent float64        `json:"kv_cache_usage_percent"`
}

// ABIFilterInput is serialized and passed to the guest filter function.
type ABIFilterInput struct {
	Request   ABIRequest    `json:"request"`
	Endpoints []ABIEndpoint `json:"endpoints"`
}

// ABIFilterOutput is deserialized from the guest filter function return.
type ABIFilterOutput struct {
	EndpointIDs []string `json:"endpoint_ids"`
}

// ABIScorerInput is serialized and passed to the guest score function.
type ABIScorerInput struct {
	Request   ABIRequest    `json:"request"`
	Endpoints []ABIEndpoint `json:"endpoints"`
}

// ABIScorerOutput is deserialized from the guest score function return.
type ABIScorerOutput struct {
	Scores map[string]float64 `json:"scores"`
}

func toABIRequest(r *scheduling.InferenceRequest) ABIRequest {
	return ABIRequest{
		RequestID:        r.RequestID,
		TargetModel:      r.TargetModel,
		Headers:          r.Headers,
		RequestSizeBytes: r.RequestSizeBytes,
	}
}

func toABIEndpoint(ep scheduling.Endpoint) ABIEndpoint {
	meta := ep.GetMetadata()
	out := ABIEndpoint{
		ID:      meta.NamespacedName.String(),
		Address: meta.Address,
		Port:    meta.Port,
		Labels:  meta.Labels,
	}
	if m := ep.GetMetrics(); m != nil {
		out.Metrics = ABIMetrics{
			ActiveModels:        m.ActiveModels,
			WaitingModels:       m.WaitingModels,
			RunningRequestsSize: m.RunningRequestsSize,
			WaitingQueueSize:    m.WaitingQueueSize,
			KVCacheUsagePercent: m.KVCacheUsagePercent,
		}
	}
	return out
}

func toABIEndpoints(eps []scheduling.Endpoint) []ABIEndpoint {
	out := make([]ABIEndpoint, len(eps))
	for i, ep := range eps {
		out[i] = toABIEndpoint(ep)
	}
	return out
}
