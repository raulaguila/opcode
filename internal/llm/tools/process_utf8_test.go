package tools

import (
	"testing"

	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/stretchr/testify/require"
)

func TestTerminalOutputPreservesFragmentedUTF8(t *testing.T) {
	data := []byte("ação 漢字 👩‍💻 ┌─▾\r\n\x1b[3;1H\x1b[1mFinished\x1b[m")
	want := vt10x.New(vt10x.WithSize(60, 8))
	_, err := want.Write(data)
	require.NoError(t, err)
	for split := 0; split <= len(data); split++ {
		p := &managedProcess{stdout: &shell.Buffer{}, screen: vt10x.New(vt10x.WithSize(60, 8))}
		writer := terminalOutput{process: p}
		_, err := writer.Write(data[:split])
		require.NoError(t, err)
		_, err = writer.Write(data[split:])
		require.NoError(t, err)
		require.Equal(t, want.String(), p.screen.String(), "screen at UTF-8 byte split %d", split)
		require.Equal(t, want.Cursor(), p.screen.Cursor(), "cursor at byte split %d", split)
		require.Empty(t, p.partialRune, "complete input must not leave partial bytes")
		actual, truncated := p.stdout.Snapshot()
		require.False(t, truncated)
		require.Equal(t, string(data), actual, "raw command output must remain byte-for-byte intact")
	}
}
