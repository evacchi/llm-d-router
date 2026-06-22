package wasm

import (
	"context"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// moduleInstance wraps a single wazero module instance with cached function references.
type moduleInstance struct {
	mod      api.Module
	allocFn  api.Function
	filterFn api.Function
	scoreFn  api.Function
}

// CompiledPlugin holds a compiled Wasm module and a pool of ready instances.
type CompiledPlugin struct {
	rt       wazero.Runtime
	compiled wazero.CompiledModule
	pool     sync.Pool
	mu       sync.Mutex
	seq      uint64
}

// NewCompiledPlugin compiles a Wasm module and validates its exports.
// The runtime is sandboxed: no WASI, no filesystem, no network.
func NewCompiledPlugin(ctx context.Context, wasmBytes []byte) (*CompiledPlugin, error) {
	rt := wazero.NewRuntime(ctx)

	_, err := rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, ptr, size uint32) {
			buf, ok := m.Memory().Read(ptr, size)
			if !ok {
				return
			}
			log.FromContext(ctx).Info("[wasm guest]", "msg", string(buf))
		}).
		Export("log_message").
		Instantiate(ctx)
	if err != nil {
		rt.Close(ctx) //nolint:errcheck
		return nil, fmt.Errorf("instantiating host module: %w", err)
	}

	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		rt.Close(ctx) //nolint:errcheck
		return nil, fmt.Errorf("compiling wasm module: %w", err)
	}

	exports := compiled.ExportedFunctions()
	if _, ok := exports["alloc"]; !ok {
		rt.Close(ctx) //nolint:errcheck
		return nil, fmt.Errorf("wasm module missing required export: alloc")
	}

	hasFilter := false
	hasScore := false
	if _, ok := exports["filter"]; ok {
		hasFilter = true
	}
	if _, ok := exports["score"]; ok {
		hasScore = true
	}
	if !hasFilter && !hasScore {
		rt.Close(ctx) //nolint:errcheck
		return nil, fmt.Errorf("wasm module must export at least one of: filter, score")
	}

	return &CompiledPlugin{
		rt:       rt,
		compiled: compiled,
	}, nil
}

func (cp *CompiledPlugin) getInstance(ctx context.Context) (*moduleInstance, error) {
	if v := cp.pool.Get(); v != nil {
		return v.(*moduleInstance), nil
	}

	cp.mu.Lock()
	name := fmt.Sprintf("inst-%d", cp.seq)
	cp.seq++
	cp.mu.Unlock()

	mod, err := cp.rt.InstantiateModule(ctx, cp.compiled, wazero.NewModuleConfig().WithName(name))
	if err != nil {
		return nil, fmt.Errorf("instantiating wasm module: %w", err)
	}

	return &moduleInstance{
		mod:      mod,
		allocFn:  mod.ExportedFunction("alloc"),
		filterFn: mod.ExportedFunction("filter"),
		scoreFn:  mod.ExportedFunction("score"),
	}, nil
}

func (cp *CompiledPlugin) putInstance(inst *moduleInstance) {
	cp.pool.Put(inst)
}

// callGuest writes input to guest memory via alloc, calls the target function,
// and reads the result. The return convention: the guest function returns a
// uint64 where the high 32 bits are the result pointer and the low 32 bits
// are the result length.
func callGuest(ctx context.Context, inst *moduleInstance, fn api.Function, input []byte) ([]byte, error) {
	size := uint64(len(input))

	results, err := inst.allocFn.Call(ctx, size)
	if err != nil {
		return nil, fmt.Errorf("calling alloc(%d): %w", size, err)
	}
	inputPtr := uint32(results[0])

	if !inst.mod.Memory().Write(inputPtr, input) {
		return nil, fmt.Errorf("writing %d bytes at offset %d: out of bounds", size, inputPtr)
	}

	results, err = fn.Call(ctx, uint64(inputPtr), size)
	if err != nil {
		return nil, fmt.Errorf("calling guest function: %w", err)
	}

	packed := results[0]
	outPtr := uint32(packed >> 32)
	outLen := uint32(packed & 0xFFFFFFFF)

	if outLen == 0 {
		return nil, nil
	}

	out, ok := inst.mod.Memory().Read(outPtr, outLen)
	if !ok {
		return nil, fmt.Errorf("reading %d bytes at offset %d: out of bounds", outLen, outPtr)
	}

	result := make([]byte, len(out))
	copy(result, out)
	return result, nil
}

// Close releases all wazero resources.
func (cp *CompiledPlugin) Close(ctx context.Context) error {
	return cp.rt.Close(ctx)
}
