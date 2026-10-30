package tools

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrepFormattingPreservesPublicOutput(t *testing.T) {
	for _, test := range []struct {
		name      string
		matches   []grepMatch
		truncated bool
		want      string
	}{
		{"empty", nil, false, "No files found"},
		{"empty truncated", nil, true, "No files found"},
		{"same file", []grepMatch{{path: "a.go", lineNum: 1, lineText: "one"}, {path: "a.go", lineNum: 2, lineText: "two"}}, false, "Found 2 matches\na.go:\n  Line 1: one\n  Line 2: two\n"},
		{"multiple files", []grepMatch{{path: "a.go", lineNum: 1, lineText: "one"}, {path: "b.go", lineNum: 0}}, false, "Found 2 matches\na.go:\n  Line 1: one\n\nb.go:\n  b.go\n"},
		{"truncated", []grepMatch{{path: "a.go", lineNum: 1, lineText: "one"}}, true, "Found 1 matches\na.go:\n  Line 1: one\n\n(Results are truncated. Consider using a more specific path or pattern.)"},
	} {
		t.Run(test.name, func(t *testing.T) { require.Equal(t, test.want, formatGrepMatches(test.matches, test.truncated)) })
	}
}

func BenchmarkGrepFormatting(b *testing.B) {
	matches := make([]grepMatch, 100)
	for i := range matches {
		matches[i] = grepMatch{path: fmt.Sprintf("dir/file_%02d.go", i/4), lineNum: i + 1, lineText: strings.Repeat("line of source ", 14)}
	}
	require.Equal(b, benchmarkConcatGrep(matches), formatGrepMatches(matches, false))
	b.Run("concatenation", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			benchmarkConcatGrep(matches)
		}
	})
	b.Run("builder", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			formatGrepMatches(matches, false)
		}
	})
}

// The previous production formatter, retained only to measure the same output
// against the builder. The tool's filtering, limit and permissions are upstream.
func benchmarkConcatGrep(matches []grepMatch) string {
	output := fmt.Sprintf("Found %d matches\n", len(matches))
	current := ""
	for _, match := range matches {
		if current != match.path {
			if current != "" {
				output += "\n"
			}
			current = match.path
			output += fmt.Sprintf("%s:\n", match.path)
		}
		if match.lineNum > 0 {
			output += fmt.Sprintf("  Line %d: %s\n", match.lineNum, match.lineText)
		} else {
			output += fmt.Sprintf("  %s\n", match.path)
		}
	}
	return output
}
