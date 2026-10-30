package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// Exercise the public tool boundary, including per-call database open/schema,
// final authorization/hash checks, and JSON output. The synthetic workspace is
// immutable, so a synchronously registered snapshot provides known coverage
// without timing watcher startup, sleeps, or background goroutines.
func BenchmarkFullWarmCodeIndexSearch(b *testing.B) {
	for _, count := range []int{200, 2000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) { benchmarkFullWarmSearch(b, count) })
	}
}

func benchmarkFullWarmSearch(b *testing.B, count int) {
	root := allocationWorkspace(b, count)
	watcher := &IndexWatcher{root: root, state: indexWatchState{covered: true, dirty: map[string]uint64{}}}
	indexWatchers.Store(root, watcher)
	defer indexWatchers.CompareAndDelete(root, watcher)
	ctx := context.WithValue(toolContext(), accessKey{}, &accessPolicy{tool: "search_code_index", approved: map[string]bool{}})
	db, err := openCodeIndex(ctx, root)
	if err != nil {
		b.Fatal(err)
	}
	_, refreshErr := refreshCodeIndex(ctx, db, root)
	closeErr := db.Close()
	if refreshErr != nil || closeErr != nil {
		b.Fatalf("initialize full search: %v %v", refreshErr, closeErr)
	}
	if !watcher.state.known {
		b.Fatal("synthetic snapshot is not ready")
	}
	tool := NewCodeIndexTool()
	input := ToolCall{Input: `{"query":"audit","limit":6}`}
	result, err := tool.Run(ctx, input)
	if err != nil || result.IsError {
		b.Fatalf("warm full search: %v %+v", err, result)
	}
	verifyFullSearchBenchmark(b, result)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		// Pin only fixture readiness outside the measured public-tool boundary.
		b.StopTimer()
		watcher.state.reconciled = time.Now()
		b.StartTimer()
		result, err = tool.Run(ctx, input)
		if err != nil || result.IsError {
			b.Fatalf("full search failed: %v %+v", err, result)
		}
	}
	b.StopTimer()
	verifyFullSearchBenchmark(b, result)
}

func verifyFullSearchBenchmark(b *testing.B, result ToolResponse) {
	b.Helper()
	var page struct {
		Results []codeIndexHit `json:"results"`
		Reads   int            `json:"scan_reads"`
	}
	if err := json.Unmarshal([]byte(result.Content), &page); err != nil {
		b.Fatal(err)
	}
	if len(page.Results) != 6 || page.Reads != 0 {
		b.Fatalf("warm verified hits missing: hits=%d reads=%d", len(page.Results), page.Reads)
	}
	for _, hit := range page.Results {
		if hit.Hash == "" || hit.Path == "" || hit.Preview == "" {
			b.Fatal("incomplete verified search hit")
		}
	}
}
