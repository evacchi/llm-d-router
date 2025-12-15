package main

import (
	"encoding/json"
	"unsafe"
)

// ScoringInput represents the complete input data for the WASM scorer
type ScoringInput struct {
	Request *RequestData      `json:"request,omitempty"`
	Pods    []PodData         `json:"pods"`
}

// RequestData represents LLM request information
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
	Prompt string `json:"prompt,omitempty"`
}

// ChatCompletionsRequest represents a chat completions request
type ChatCompletionsRequest struct {
	Messages []Message `json:"messages,omitempty"`
}

// Message represents a chat message
type Message struct {
	Role    string  `json:"role,omitempty"`
	Content Content `json:"content,omitempty"`
}

// Content can be either a string or structured content
type Content struct {
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

// Global buffer pool to prevent garbage collection
var buffers [][]byte

//export score
func score(ptr, size uint32) uint64 {
	// Read input JSON from memory
	inputData := readString(ptr, size)

	// Parse scoring input
	var input ScoringInput
	if err := json.Unmarshal([]byte(inputData), &input); err != nil {
		return 0
	}

	// Score each pod
	// In this simple example, all pods get score 1.0
	// You can access input data for request-aware scoring:
	// - input.Request.RequestID
	// - input.Request.TargetModel
	// - input.Request.Headers
	// - input.Request.Body.Completions.Prompt
	// - input.Request.Body.ChatCompletions.Messages
	// - input.State (map of cycle state data)
	results := make([]ScoredPod, len(input.Pods))
	for i, pod := range input.Pods {
		results[i] = ScoredPod{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Score:     1.0, // Simple fixed score
		}
	}

	// Marshal results to JSON
	resultJSON, err := json.Marshal(results)
	if err != nil {
		return 0
	}

	// Allocate memory for result - keep reference to prevent GC
	resultBuf := make([]byte, len(resultJSON))
	copy(resultBuf, resultJSON)
	buffers = append(buffers, resultBuf)
	resultPtr := uint32(uintptr(unsafe.Pointer(&resultBuf[0])))

	// Return pointer and size as packed uint64
	return (uint64(resultPtr) << 32) | uint64(len(resultBuf))
}

//export allocate
func allocate(size uint32) uint32 {
	buf := make([]byte, size)
	buffers = append(buffers, buf)
	return uint32(uintptr(unsafe.Pointer(&buf[0])))
}

// Helper to read string from memory
func readString(ptr, size uint32) string {
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), size)
	return string(data)
}

// Helper to copy data to allocated memory
func copyToMemory(ptr uint32, data []byte) {
	dest := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), len(data))
	copy(dest, data)
}

func main() {}
