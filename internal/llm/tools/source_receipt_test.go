package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeSourceReceiptsTrackSnapshotAndDeliveredIntervals(t *testing.T) {
	root := productivityWorkspace(t)
	path := filepath.Join(root, "main.go")
	raw := []byte("package main\nfunc hello() {\n println(\"hello\")\n}\n")
	require.NoError(t, os.WriteFile(path, raw, 0600))
	for _, test := range []struct {
		tool        BaseTool
		input       string
		first, last int
	}{
		{NewViewTool(nil), `{"file_path":"main.go","offset":1,"limit":2}`, 2, 3},
		{NewReadPageTool(), `{"file_path":"main.go","offset":1,"limit":2}`, 2, 3},
		{&projectTool{name: "read_symbol"}, `{"file_path":"main.go","symbol":"hello","offset":1,"limit":1}`, 3, 3},
	} {
		t.Run(test.tool.Info().Name, func(t *testing.T) {
			result, err := SecureTool(test.tool, nil).Run(toolContext(), ToolCall{Input: test.input})
			require.NoError(t, err)
			require.False(t, result.IsError, result.Content)
			receipt, ok := VisibleSourceReadReceipt(test.tool.Info().Name, result.Content, result.Metadata)
			require.True(t, ok, result.Metadata)
			require.Equal(t, test.first, receipt.StartLine)
			require.Equal(t, test.last, receipt.EndLine)
			require.Equal(t, indexDigest(string(raw)), receipt.Hash)
			require.True(t, SourceReadCurrent(context.Background(), receipt))
		})
	}
}

func TestSourceReceiptVerifierCannotGrantAccess(t *testing.T) {
	root := productivityWorkspace(t)
	path := filepath.Join(root, "source.go")
	require.NoError(t, os.WriteFile(path, []byte("package source\n"), 0600))
	receipt := SourceReadReceipt{Path: path, Hash: indexDigest("package source\n"), StartLine: 1, EndLine: 1}
	require.True(t, SourceReadCurrent(context.Background(), receipt))
	require.NoError(t, os.WriteFile(path, []byte("changed\n"), 0600))
	require.False(t, SourceReadCurrent(context.Background(), receipt), "changed source cannot retain current evidence")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("not-a-real-secret\n"), 0600))
	receipt.Path, receipt.Hash = filepath.Join(root, ".env"), indexDigest("not-a-real-secret\n")
	require.False(t, SourceReadCurrent(context.Background(), receipt))
	external := filepath.Join(t.TempDir(), "outside.go")
	require.NoError(t, os.WriteFile(external, []byte("source"), 0600))
	receipt.Path, receipt.Hash = external, indexDigest("source")
	require.False(t, SourceReadCurrent(context.Background(), receipt))
	link := filepath.Join(root, "alias.go")
	require.NoError(t, os.Symlink(path, link))
	receipt.Path, receipt.Hash = link, indexDigest("changed\n")
	require.False(t, SourceReadCurrent(context.Background(), receipt))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	receipt.Path = path
	require.False(t, SourceReadCurrent(ctx, receipt))
}

func TestSourceReceiptCannotCiteTruncatedOutputOrMetadata(t *testing.T) {
	root := productivityWorkspace(t)
	path := filepath.Join(root, "source.go")
	require.NoError(t, os.WriteFile(path, []byte("package source\n// second\n// third\n"), 0600))
	result, err := NewViewTool(nil).Run(toolContext(), ToolCall{Input: `{"file_path":"source.go"}`})
	require.NoError(t, err)
	// Number padding depends on total lines, so truncate by the source text.
	cut := strings.Index(result.Content, "// third")
	cut = strings.LastIndex(result.Content[:cut], "\n")
	receipt, ok := VisibleSourceReadReceipt("view", result.Content[:cut], result.Metadata)
	require.True(t, ok)
	require.Equal(t, 2, receipt.EndLine)
	_, ok = VisibleSourceReadReceipt("search_code_index", result.Content, result.Metadata)
	require.False(t, ok)
	_, ok = VisibleSourceReadReceipt("view", "source.go:1-3", result.Metadata)
	require.False(t, ok)
	_, ok = ParseSourceReadReceipt(strings.Repeat("x", 4097))
	require.False(t, ok)
}

func TestLargeViewKeepsSmallNativeReceiptMetadata(t *testing.T) {
	root := productivityWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.txt"), []byte(strings.Repeat("line of source\n", 500)), 0600))
	result, err := NewViewTool(nil).Run(toolContext(), ToolCall{Input: `{"file_path":"source.txt"}`})
	require.NoError(t, err)
	require.Less(t, len(result.Metadata), 4096)
	_, ok := ParseSourceReadReceipt(result.Metadata)
	require.True(t, ok)
}

func TestSourceVerifierChecksEveryIntervalAndExpiresWithItsPhase(t *testing.T) {
	root := productivityWorkspace(t)
	path := filepath.Join(root, "source.txt")
	text := strings.Repeat("x", 32767) + "\nlast\n"
	require.NoError(t, os.WriteFile(path, []byte(text), 0600))
	receipt := SourceReadReceipt{Path: path, Hash: indexDigest(text), StartLine: 1, EndLine: 3}
	verifier := NewSourceReadVerifier(context.Background())
	incorrect := receipt
	incorrect.Hash = indexDigest("not the source")
	require.False(t, verifier.Current(incorrect))
	require.True(t, verifier.Current(receipt), "count newlines across buffer boundaries")
	receipt.EndLine = 4
	require.False(t, verifier.Current(receipt), "a cached hash cannot validate an unread line")
	receipt.EndLine = 3
	require.True(t, verifier.Current(receipt), "a rejected interval cannot poison valid pages")

	stamp, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(text, "last", "next", 1)), 0600))
	require.NoError(t, os.Chtimes(path, stamp.ModTime(), stamp.ModTime()))
	require.False(t, NewSourceReadVerifier(context.Background()).Current(receipt), "the next phase must hash again despite matching size/mtime")
	ctx, cancel := context.WithCancel(context.Background())
	canceled := NewSourceReadVerifier(ctx)
	cancel()
	require.False(t, canceled.Current(receipt))
}
