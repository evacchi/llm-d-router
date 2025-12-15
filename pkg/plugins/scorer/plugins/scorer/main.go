package main

import (
	"encoding/json"
	"unsafe"
)

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
	// You can access input.Request (types.LLMRequest) for request-aware scoring:
	// - input.Request.RequestId
	// - input.Request.TargetModel
	// - input.Request.Headers
	// - input.Request.Body.Completions.Prompt
	// - input.Request.Body.ChatCompletions.Messages
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
	resultLen := len(resultJSON)
	resultPtr := allocate(uint32(resultLen))
	copyToMemory(resultPtr, resultJSON)

	// Return pointer and size as packed uint64
	return (uint64(resultPtr) << 32) | uint64(resultLen)
}

//export allocate
func allocate(size uint32) uint32 {
	buf := make([]byte, size)
	buffers = append(buffers, buf)
	return uint32(uintptr(unsafe.Pointer(&buf[0])))
}

// Helper to read string from memory
func readString(ptr, size uint32) string {
	return unsafe.String((*byte)(unsafe.Pointer(uintptr(ptr))), size)
}

// Helper to copy data to allocated memory
func copyToMemory(ptr uint32, data []byte) {
	dest := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), len(data))
	copy(dest, data)
}

func main() {}
