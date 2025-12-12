package main

import (
	"encoding/json"
	"unsafe"
)

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

//export score
func score(ptr, size uint32) uint64 {
	// Read input JSON from memory
	inputData := readString(ptr, size)

	// Parse input pods
	var pods []PodData
	if err := json.Unmarshal([]byte(inputData), &pods); err != nil {
		return 0
	}

	// Score each pod (simple: all pods get score 1.0)
	results := make([]ScoredPod, len(pods))
	for i, pod := range pods {
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

	// Allocate memory for result and copy
	resultPtr := allocate(uint32(len(resultJSON)))
	copyToMemory(resultPtr, resultJSON)

	// Return pointer and size as packed uint64
	return (uint64(resultPtr) << 32) | uint64(len(resultJSON))
}

//export allocate
func allocate(size uint32) uint32 {
	buf := make([]byte, size)
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
