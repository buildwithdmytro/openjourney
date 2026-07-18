package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func (h *Host) invokeWasm(ctx context.Context, p domain.Principal, ev domain.ExtensionVersion, cfg domain.ExtensionConfig, invocation string, input json.RawMessage, wasmBytes []byte) (json.RawMessage, error) {
	maxMemoryMb := cfg.MaxMemoryMb
	if maxMemoryMb <= 0 {
		maxMemoryMb = 64
	}
	limitPages := uint32(maxMemoryMb) * 16 // 1MB = 16 pages of 64KB

	// Configure the runtime with the memory limit and context done closure (for timeout interrupts)
	rConfig := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(limitPages).
		WithCloseOnContextDone(true)

	r := wazero.NewRuntimeWithConfig(ctx, rConfig)
	defer r.Close(ctx)

	// Instantiate WASI snapshot preview1 host imports
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	// Compile the Wasm module
	compiledModule, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to compile Wasm module: %w", err)
	}

	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}

	moduleConfig := wazero.NewModuleConfig().
		WithStdin(bytes.NewReader(input)).
		WithStdout(stdoutBuf).
		WithStderr(stderrBuf).
		WithFS(nil) // no host filesystem access

	// Instantiate the module. If it's a command module (with _start), this automatically executes it.
	mod, err := r.InstantiateModule(ctx, compiledModule, moduleConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate Wasm module: %w", err)
	}
	defer mod.Close(ctx)

	// If a custom exported function is requested, check if it exists and invoke it.
	if invocation != "" && invocation != "_start" && invocation != "run" {
		fn := mod.ExportedFunction(invocation)
		if fn != nil {
			// Look for standard memory allocation functions
			allocFn := mod.ExportedFunction("malloc")
			if allocFn == nil {
				allocFn = mod.ExportedFunction("alloc")
			}
			if allocFn == nil {
				allocFn = mod.ExportedFunction("allocate")
			}

			if allocFn != nil {
				// Allocate guest memory for input JSON
				results, err := allocFn.Call(ctx, uint64(len(input)))
				if err != nil {
					return nil, fmt.Errorf("failed to allocate memory in Wasm guest: %w", err)
				}
				if len(results) == 0 {
					return nil, fmt.Errorf("wasm allocator returned no pointer")
				}
				ptr := uint32(results[0])

				// Write input JSON to guest memory
				if len(input) > 0 {
					if !mod.Memory().Write(ptr, input) {
						return nil, fmt.Errorf("failed to write input JSON to Wasm guest memory")
					}
				}

				// Invoke guest function passing pointer and length
				res, err := fn.Call(ctx, uint64(ptr), uint64(len(input)))
				if err != nil {
					return nil, fmt.Errorf("failed to call Wasm guest function %q: %w", invocation, err)
				}

				// Read output from packed (ptr, len) return value
				if len(res) > 0 {
					packed := res[0]
					outPtr := uint32(packed >> 32)
					outLen := uint32(packed)
					outBytes, ok := mod.Memory().Read(outPtr, outLen)
					if !ok {
						return nil, fmt.Errorf("failed to read output from Wasm guest memory")
					}
					return json.RawMessage(outBytes), nil
				}
			} else {
				return nil, fmt.Errorf("wasm guest function %q requires input JSON but no allocator (malloc/alloc/allocate) was exported", invocation)
			}
		}
	}

	return json.RawMessage(stdoutBuf.Bytes()), nil
}
