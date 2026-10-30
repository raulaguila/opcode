package tools

import (
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/lsp/protocol"
	"github.com/stretchr/testify/require"
)

func TestIndexedUTF16OffsetsRetainPositionValidation(t *testing.T) {
	for _, text := range []string{"", "abc", "a\r\n😀b\n", "αβ\n𐐷z\r\n"} {
		positions := []protocol.Position{{}, {Character: 1}, {Character: 2}, {Line: 1}, {Line: 1, Character: 1}, {Line: 1, Character: 2}, {Line: 2}, {Line: 9}}
		edits := make([]protocol.TextEdit, len(positions))
		for i, position := range positions {
			edits[i].Range.Start, edits[i].Range.End = position, position
		}
		index := newUTF16LineIndex(text, edits)
		for _, position := range positions {
			want, wantErr := utf16Offset(text, position)
			got, gotErr := utf16EditOffset(text, index, position)
			require.Equal(t, want, got)
			if wantErr != nil {
				require.EqualError(t, gotErr, wantErr.Error())
				continue
			}
			require.NoError(t, gotErr)
		}
	}
}

func BenchmarkUTF16ReplacementOffsets(b *testing.B) {
	text := strings.Repeat(strings.Repeat("a", 99)+"\n", 1000)
	edits := make([]protocol.TextEdit, 1000)
	for i := range edits {
		edits[i] = protocol.TextEdit{Range: protocol.Range{Start: protocol.Position{Line: uint32(i), Character: 1}, End: protocol.Position{Line: uint32(i), Character: 2}}, NewText: "b"}
	}
	for _, test := range []struct {
		name string
		run  func(string, []protocol.TextEdit) ([]utf16Replacement, error)
	}{{"scan", scanUTF16ReplacementOffsets}, {"indexed", utf16Replacements}} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if _, err := test.run(text, edits); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Baseline range conversion before indexing; retain it only for the benchmark.
func scanUTF16ReplacementOffsets(text string, edits []protocol.TextEdit) ([]utf16Replacement, error) {
	var pending []utf16Replacement
	for _, edit := range edits {
		start, err := utf16Offset(text, edit.Range.Start)
		if err != nil {
			return nil, err
		}
		end, err := utf16Offset(text, edit.Range.End)
		if err != nil {
			return nil, err
		}
		pending = append(pending, utf16Replacement{start, end, edit.NewText})
	}
	return pending, nil
}
