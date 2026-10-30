package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
)

func allocationWorkspace(b *testing.B, count int) string {
	b.Helper()
	cfg := config.Get()
	previousWD, previousData, previousSecurity := cfg.WorkingDir, cfg.Data, cfg.Security
	root := b.TempDir()
	cfg.WorkingDir, cfg.Data.Directory = root, filepath.Join(b.TempDir(), "private")
	cfg.Security = config.SecurityConfig{Sandbox: "none"} // Same explicit process fixture as productivityV2Workspace.
	b.Cleanup(func() { cfg.WorkingDir, cfg.Data, cfg.Security = previousWD, previousData, previousSecurity })
	for i := range count {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("source%05d.txt", i)), []byte(strings.Repeat("audit text ", 100)), 0600); err != nil {
			b.Fatal(err)
		}
	}
	return root
}

func BenchmarkWorkspaceDigest(b *testing.B) {
	for _, count := range []int{200, 2000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			allocationWorkspace(b, count)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := WorkspaceDigest(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkWarmCodeIndexSearch(b *testing.B) {
	for _, count := range []int{200, 2000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) { benchmarkWarmIndexSearch(b, count) })
	}
}
func benchmarkWarmIndexSearch(b *testing.B, count int) {
	root := allocationWorkspace(b, count)
	ctx := toolContext()
	db, err := openCodeIndex(ctx, root)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	if _, err = refreshCodeIndex(ctx, db, root); err != nil {
		b.Fatal(err)
	}
	watcher := StartIndexWatcher(ctx)
	defer watcher.Close()
	// Excluded startup: initial watcher refresh and directory coverage settle before timing.
	time.Sleep(time.Second)
	input := codeIndexInput{Query: "audit", Limit: 6}
	if _, err = captureIndexSearch(ctx, db, root, ".", input); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		snapshot, err := captureIndexSearch(ctx, db, root, ".", input)
		if err != nil {
			b.Fatal(err)
		}
		if len(snapshot.candidates) == 0 {
			b.Fatal("indexed evidence missing")
		}
	}
}

func BenchmarkCitationCurrent(b *testing.B) {
	root := allocationWorkspace(b, 0)
	raw := strings.Repeat("a", 1<<20)
	path := filepath.Join(root, "large.txt")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		b.Fatal(err)
	}
	receipt := SourceReadReceipt{Path: path, Hash: indexDigest(raw), StartLine: 1, EndLine: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if !SourceReadCurrent(context.Background(), receipt) {
			b.Fatal("current source rejected")
		}
	}
}
