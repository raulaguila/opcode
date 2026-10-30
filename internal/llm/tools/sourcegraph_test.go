package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func sourcegraphFixture(content string, matches []any) map[string]any {
	return map[string]any{"data": map[string]any{"search": map[string]any{"results": map[string]any{
		"matchCount": float64(len(matches)), "resultCount": float64(1),
		"results": []any{map[string]any{"__typename": "FileMatch", "repository": map[string]any{"name": "project"}, "file": map[string]any{"path": "main.go", "url": "https://example.test/file", "content": content}, "lineMatches": matches}},
	}}}}
}

func TestSourcegraphContextFormatting(t *testing.T) {
	result := sourcegraphFixture("first\nsecond\nthird\nfourth", []any{map[string]any{"lineNumber": float64(2), "preview": "second"}, nil})
	text, err := formatSourcegraphResults(result, 1)
	require.NoError(t, err)
	require.Equal(t, "# Sourcegraph Search Results\n\nFound 2 matches across 1 results\n\n## Result 1: project/main.go\n\nURL: https://example.test/file\n\n```\n1| first\n2|  second\n3| third\n```\n\n", text)
}

func TestSourcegraphMalformedLineDoesNotPanic(t *testing.T) {
	result := sourcegraphFixture("first\nsecond", []any{map[string]any{"lineNumber": float64(-1), "preview": "untrusted preview"}})
	text, err := formatSourcegraphResults(result, 2)
	require.NoError(t, err)
	require.Contains(t, text, "untrusted preview")
}

func BenchmarkSourcegraphContextFormatting(b *testing.B) {
	matches := make([]any, 100)
	for i := range matches {
		matches[i] = map[string]any{"lineNumber": float64(500 + i), "preview": "matching source"}
	}
	result := sourcegraphFixture(strings.Repeat("example source line\n", 2000), matches)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := formatSourcegraphResults(result, 2); err != nil {
			b.Fatal(err)
		}
	}
}
