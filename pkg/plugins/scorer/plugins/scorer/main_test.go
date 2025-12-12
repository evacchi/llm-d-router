package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func TestWasmModule(t *testing.T) {
	// Read the WASM file
	wasmBytes, err := os.ReadFile("scorer.wasm")
	if err != nil {
		t.Fatalf("Failed to read WASM file: %v", err)
	}

	// Create runtime
	ctx := context.Background()
	runtime := wazero.NewRuntime(ctx)
	defer runtime.Close(ctx)

	// Instantiate WASI
	wasi_snapshot_preview1.MustInstantiate(ctx, runtime)

	// Compile module
	compiledModule, err := runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("Failed to compile WASM module: %v", err)
	}
	defer compiledModule.Close(ctx)

	// Instantiate module
	cfg := wazero.NewModuleConfig().WithStdout(os.Stdout).WithStderr(os.Stderr)
	module, err := runtime.InstantiateModule(ctx, compiledModule, cfg)
	if err != nil {
		t.Fatalf("Failed to instantiate WASM module: %v", err)
	}
	defer module.Close(ctx)

	// Get exported functions
	allocateFn := module.ExportedFunction("allocate")
	scoreFn := module.ExportedFunction("score")

	if allocateFn == nil {
		t.Fatal("allocate function not found")
	}
	if scoreFn == nil {
		t.Fatal("score function not found")
	}

	// Create test input
	testInput := []PodData{
		{
			Name:      "pod-a",
			Namespace: "default",
			Address:   "10.0.0.1",
			Port:      "8080",
			Labels:    map[string]string{"app": "test"},
		},
		{
			Name:      "pod-b",
			Namespace: "default",
			Address:   "10.0.0.2",
			Port:      "8080",
			Labels:    map[string]string{"app": "test"},
		},
	}

	inputJSON, err := json.Marshal(testInput)
	if err != nil {
		t.Fatalf("Failed to marshal input: %v", err)
	}

	t.Logf("Input JSON: %s", string(inputJSON))

	// Allocate memory for input
	inputSize := uint64(len(inputJSON))
	results, err := allocateFn.Call(ctx, inputSize)
	if err != nil {
		t.Fatalf("Failed to allocate memory: %v", err)
	}
	inputPtr := uint32(results[0])

	t.Logf("Allocated input at ptr: %d, size: %d", inputPtr, inputSize)

	// Write input to WASM memory
	if !module.Memory().Write(inputPtr, inputJSON) {
		t.Fatal("Failed to write input to WASM memory")
	}

	// Call score function
	results, err = scoreFn.Call(ctx, uint64(inputPtr), inputSize)
	if err != nil {
		t.Fatalf("Failed to call score function: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Score function returned no results")
	}

	packed := results[0]
	t.Logf("Score function returned packed value: %d (0x%x)", packed, packed)

	if packed == 0 {
		t.Fatal("Score function returned 0, indicating an error")
	}

	// Extract result pointer and size
	resultPtr := uint32(packed >> 32)
	resultSize := uint32(packed & 0xFFFFFFFF)

	t.Logf("Result ptr: %d, size: %d", resultPtr, resultSize)

	// Read result from WASM memory
	resultJSON, ok := module.Memory().Read(resultPtr, resultSize)
	if !ok {
		t.Fatal("Failed to read result from WASM memory")
	}

	t.Logf("Result JSON: %s", string(resultJSON))

	// Unmarshal result
	var scoredPods []ScoredPod
	if err := json.Unmarshal(resultJSON, &scoredPods); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Verify results
	if len(scoredPods) != len(testInput) {
		t.Errorf("Expected %d scored pods, got %d", len(testInput), len(scoredPods))
	}

	for i, scored := range scoredPods {
		if scored.Name != testInput[i].Name {
			t.Errorf("Pod %d: expected name %s, got %s", i, testInput[i].Name, scored.Name)
		}
		if scored.Namespace != testInput[i].Namespace {
			t.Errorf("Pod %d: expected namespace %s, got %s", i, testInput[i].Namespace, scored.Namespace)
		}
		if scored.Score != 1.0 {
			t.Errorf("Pod %d: expected score 1.0, got %f", i, scored.Score)
		}
	}

	t.Log("Test passed!")
}
