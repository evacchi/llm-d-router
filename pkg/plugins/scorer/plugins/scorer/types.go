package main

// ScoringInput represents the complete input data for the WASM scorer
type ScoringInput struct {
	Request *RequestData `json:"request,omitempty"`
	Pods    []PodData    `json:"pods"`
}

// RequestData mirrors the RequestData from wasm_scorer.go
// We need this duplication because:
// 1. The WASM module is compiled separately with TinyGo for WASI target
// 2. TinyGo/WASI can't compile external packages with k8s.io dependencies
type RequestData struct {
	RequestID   string            `json:"request_id"`
	TargetModel string            `json:"target_model"`
	Body        *LLMRequestBody   `json:"body,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// LLMRequestBody contains the request-body fields
type LLMRequestBody struct {
	Completions     *CompletionsRequest     `json:"completions,omitempty"`
	ChatCompletions *ChatCompletionsRequest `json:"chat_completions,omitempty"`
}

// CompletionsRequest represents a completions request
type CompletionsRequest struct {
	Prompt    string `json:"prompt,omitempty"`
	CacheSalt string `json:"cache_salt,omitempty"`
}

// ChatCompletionsRequest represents a chat completions request
type ChatCompletionsRequest struct {
	Messages  []Message     `json:"messages,omitempty"`
	Tools     []interface{} `json:"tools,omitempty"`
	CacheSalt string        `json:"cache_salt,omitempty"`
}

// Message represents a chat message
type Message struct {
	Role    string  `json:"role,omitempty"`
	Content Content `json:"content,omitempty"`
}

// Content represents message content
type Content struct {
	// Raw string content - we simplify this for WASM
	// The actual types.Content is more complex with UnmarshalJSON
	Raw string `json:"-"`
}

// PodData represents the input pod information
type PodData struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Address   string            `json:"address"`
	Port      string            `json:"port"`
	Labels    map[string]string `json:"labels"`
}

// ScoredPod represents a pod with its score
type ScoredPod struct {
	Name      string  `json:"name"`
	Namespace string  `json:"namespace"`
	Score     float64 `json:"score"`
}
