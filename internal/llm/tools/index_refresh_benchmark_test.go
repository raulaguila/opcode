package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/raulaguila/opcode/internal/config"
)

func BenchmarkIndexRefreshChanges(b *testing.B) {
	for _, mode := range []string{"one-file", "policy", "periodic"} {
		b.Run(mode, func(b *testing.B) { benchmarkIndexRefreshChange(b, mode) })
	}
}

func benchmarkIndexRefreshChange(b *testing.B, mode string) {
	root := allocationWorkspace(b, 200)
	previous := config.Get().Index
	disabled := false
	config.Get().Index.Background = &disabled
	defer func() { config.Get().Index = previous }()
	ctx := toolContext()
	watcher := StartIndexWatcher(ctx)
	defer watcher.Close()
	awaitBenchmarkIndexCoverage(b, watcher)
	db, err := openCodeIndex(ctx, root)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	if _, err := refreshSearchCodeIndex(ctx, db, root); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		b.StopTimer()
		prepareRefreshBenchmark(b, watcher, root, mode, i)
		b.StartTimer()
		stats, err := refreshSearchCodeIndex(ctx, db, root)
		if err != nil {
			b.Fatal(err)
		}
		validateBenchmarkRefresh(b, mode, stats)
	}
}

func prepareRefreshBenchmark(b *testing.B, watcher *IndexWatcher, root, mode string, i int) {
	b.Helper()
	switch mode {
	case "one-file":
		path := filepath.Join(root, "source00000.txt")
		if err := os.WriteFile(path, []byte(fmt.Sprintf("changed audit text %08d\n", i)), 0600); err != nil {
			b.Fatal(err)
		}
		// Deliver the same coalesced event synchronously; filesystem notification
		// latency is excluded from measurements of refresh work.
		watcher.recordEvent(fsnotify.Event{Name: path, Op: fsnotify.Write})
	case "policy":
		name := fmt.Sprintf("source%05d.txt\n", i%200)
		if err := os.WriteFile(filepath.Join(root, ".opcodeignore"), []byte(name), 0600); err != nil {
			b.Fatal(err)
		}
		watcher.invalidate()
	case "periodic":
		watcher.mu.Lock()
		watcher.state.reconciled = time.Now().Add(-indexReconcileInterval)
		watcher.mu.Unlock()
	}
	// Full reconciliations may need to restore directory coverage after a policy event.
	if mode == "policy" {
		watcher.mu.Lock()
		watcher.state.covered = true
		watcher.mu.Unlock()
	}
}

func awaitBenchmarkIndexCoverage(b *testing.B, watcher *IndexWatcher) {
	b.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		watcher.mu.Lock()
		covered := watcher.state.covered
		watcher.mu.Unlock()
		if covered {
			return
		}
		if time.Now().After(deadline) {
			b.Fatal("watch coverage unavailable")
		}
		time.Sleep(time.Millisecond)
	}
}

func validateBenchmarkRefresh(b *testing.B, mode string, stats indexRefresh) {
	b.Helper()
	if mode == "one-file" && stats.Read != 1 {
		b.Fatalf("read %d files for one-file edit", stats.Read)
	}
	if mode == "policy" && stats.Reused+stats.Read != 199 {
		b.Fatalf("visited %d files after policy change", stats.Reused+stats.Read)
	}
	if mode == "periodic" && stats.Reused != 200 {
		b.Fatalf("reused %d files in periodic scan", stats.Reused)
	}
}
