package guest

// ABIRequest mirrors the host-side ABIRequest.
type ABIRequest struct {
	RequestID        string            `json:"request_id"`
	TargetModel      string            `json:"target_model"`
	Headers          map[string]string `json:"headers,omitempty"`
	RequestSizeBytes int               `json:"request_size_bytes,omitempty"`
}

// ABIEndpoint mirrors the host-side ABIEndpoint.
type ABIEndpoint struct {
	ID      string            `json:"id"`
	Address string            `json:"address"`
	Port    string            `json:"port"`
	Labels  map[string]string `json:"labels,omitempty"`
	Metrics ABIMetrics        `json:"metrics"`
}

// ABIMetrics mirrors the host-side ABIMetrics.
type ABIMetrics struct {
	ActiveModels        map[string]int `json:"active_models,omitempty"`
	WaitingModels       map[string]int `json:"waiting_models,omitempty"`
	RunningRequestsSize int            `json:"running_requests_size"`
	WaitingQueueSize    int            `json:"waiting_queue_size"`
	KVCacheUsagePercent float64        `json:"kv_cache_usage_percent"`
}

type filterInput struct {
	Request   ABIRequest    `json:"request"`
	Endpoints []ABIEndpoint `json:"endpoints"`
}

type filterOutput struct {
	EndpointIDs []string `json:"endpoint_ids"`
}

type scorerInput struct {
	Request   ABIRequest    `json:"request"`
	Endpoints []ABIEndpoint `json:"endpoints"`
}

type scorerOutput struct {
	Scores map[string]float64 `json:"scores"`
}
