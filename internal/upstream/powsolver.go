package upstream

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"runtime"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// wasmMode returns the execution mode wazero will use on the current arch.
// wazero auto-selects compiler on amd64/arm64 and interpreter elsewhere.
// Compiler mode is ~10x faster for PoW workloads.
func wasmMode() string {
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return "compiler"
	default:
		return "interpreter"
	}
}

// powWasm is the SHA-3 PoW solver module shipped by DeepSeek's web client.
// Same binary the browser loads — guarantees algorithm parity.
//
//go:embed wasm/sha3_wasm_bg.wasm
var powWasm []byte

// powSolver wraps the wazero runtime hosting DeepSeek's WASM solver.
// All exported calls share a single linear-memory module, so concurrent solves
// are serialized by mu — solving is ~50 ms, which is fine for our throughput.
type powSolver struct {
	mu       sync.Mutex
	runtime  wazero.Runtime
	module   api.Module
	alloc    api.Function
	stackAdj api.Function
	solve    api.Function
}

func newPoWSolver(ctx context.Context) (*powSolver, error) {
	r := wazero.NewRuntime(ctx)

	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("instantiate wasi: %w", err)
	}

	mod, err := r.Instantiate(ctx, powWasm)
	if err != nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("instantiate wasm: %w", err)
	}

	s := &powSolver{
		runtime:  r,
		module:   mod,
		alloc:    mod.ExportedFunction("__wbindgen_export_0"),
		stackAdj: mod.ExportedFunction("__wbindgen_add_to_stack_pointer"),
		solve:    mod.ExportedFunction("wasm_solve"),
	}

	if s.alloc == nil || s.stackAdj == nil || s.solve == nil {
		_ = r.Close(ctx)
		return nil, fmt.Errorf("wasm module missing required exports")
	}

	slog.Debug("wasm pow solver ready",
		slog.String("mode", wasmMode()),
		slog.String("arch", runtime.GOARCH),
	)
	return s, nil
}

func (s *powSolver) Close(ctx context.Context) error {
	return s.runtime.Close(ctx)
}

// writeStr allocates `len(str)` bytes inside the WASM linear memory via the
// wasm-bindgen allocator and copies the UTF-8 bytes into it.
func (s *powSolver) writeStr(ctx context.Context, str string) (ptr, length uint32, err error) {
	b := []byte(str)
	length = uint32(len(b))

	res, err := s.alloc.Call(ctx, uint64(length), 1)
	if err != nil {
		return 0, 0, fmt.Errorf("alloc(%d): %w", length, err)
	}
	ptr = uint32(res[0])

	if !s.module.Memory().Write(ptr, b) {
		return 0, 0, fmt.Errorf("memory write at %d (len %d) out of range", ptr, length)
	}
	return ptr, length, nil
}

// Solve runs DeepSeekHashV1 in WASM and returns the integer answer.
//
// Calling convention mirrors the Python reference (xtekky/deepseek4free):
//
//	prefix := salt + "_" + expire_at + "_"
//	wasm_solve(retptr, challenge_ptr, challenge_len, prefix_ptr, prefix_len, difficulty as f64)
//
// The result is a 16-byte struct at retptr: int32 status at offset 0,
// f64 answer at offset 8 (little-endian, both).
func (s *powSolver) Solve(ctx context.Context, challenge, salt string, difficulty int, expireAt int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := fmt.Sprintf("%s_%d_", salt, expireAt)

	// Reserve 16 bytes on the WASM stack for the return struct.
	// stackAdj takes an i32; -16 in two's-complement uint32 = 0xFFFFFFF0.
	const negSixteen = uint64(0x00000000FFFFFFF0)
	res, err := s.stackAdj.Call(ctx, negSixteen)
	if err != nil {
		return 0, fmt.Errorf("stack adjust(-16): %w", err)
	}
	retptr := uint32(res[0])
	defer func() {
		_, _ = s.stackAdj.Call(ctx, 16)
	}()

	challengePtr, challengeLen, err := s.writeStr(ctx, challenge)
	if err != nil {
		return 0, err
	}
	prefixPtr, prefixLen, err := s.writeStr(ctx, prefix)
	if err != nil {
		return 0, err
	}

	_, err = s.solve.Call(ctx,
		uint64(retptr),
		uint64(challengePtr), uint64(challengeLen),
		uint64(prefixPtr), uint64(prefixLen),
		math.Float64bits(float64(difficulty)),
	)
	if err != nil {
		return 0, fmt.Errorf("wasm_solve: %w", err)
	}

	statusBytes, ok := s.module.Memory().Read(retptr, 4)
	if !ok {
		return 0, fmt.Errorf("read status: out of range at %d", retptr)
	}
	status := int32(binary.LittleEndian.Uint32(statusBytes))
	if status == 0 {
		return 0, fmt.Errorf("wasm_solve returned no solution (status=0)")
	}

	valueBytes, ok := s.module.Memory().Read(retptr+8, 8)
	if !ok {
		return 0, fmt.Errorf("read answer: out of range at %d", retptr+8)
	}
	answer := math.Float64frombits(binary.LittleEndian.Uint64(valueBytes))

	return int(answer), nil
}
