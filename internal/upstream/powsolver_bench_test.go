package upstream

import (
	"context"
	"testing"
	"time"
)

// BenchmarkSolve measures PoW solving throughput at a low synthetic difficulty.
// Production difficulty is ~144_000 — on compiler mode (amd64/arm64) expect
// ~100 ms/op; on interpreter mode (386/etc.) expect ~3–8 s/op.
//
// Run with: go test -bench=BenchmarkSolve -benchmem ./internal/upstream/
func BenchmarkSolve(b *testing.B) {
	ctx := context.Background()
	s, err := newPoWSolver(ctx)
	if err != nil {
		b.Fatalf("newPoWSolver: %v", err)
	}
	defer s.Close(ctx) //nolint:errcheck

	// Low difficulty so the benchmark completes in milliseconds even in
	// interpreter mode. Increase to 144_000 to simulate production workload.
	const (
		benchDifficulty = 500
		challenge       = "bdc814e2de522ae7766ededbf8b4bd6de4f8c1768317be7b69770865cc62db43"
		salt            = "a45d55753eecc4a5f676"
	)
	expireAt := time.Now().Add(5 * time.Minute).UnixMilli()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := s.Solve(ctx, challenge, salt, benchDifficulty, expireAt); err != nil {
			b.Fatalf("Solve: %v", err)
		}
	}
}
