//go:build integration

package tui

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"
)

func TestPTYUTF8ReadBoundariesPreserveFrameAndRaw(t *testing.T) {
	const display = "\x1b[H\x1b[2J│ ação 漢字 🙂 👩‍💻\r\n\x1b[7mSelecionado\x1b[27m\x1b[3;4Hdone"
	input := []byte(ansi.RequestKittyKeyboard + display + ansi.PushKittyKeyboard(1) + ansi.PopKittyKeyboard(1))
	reference := vt10x.New(vt10x.WithSize(40, 8), vt10x.WithWriter(io.Discard))
	_, err := reference.Write([]byte(display))
	require.NoError(t, err)
	for _, setup := range []bool{false, true} {
		name := "live"
		if setup {
			name = "setup"
		}
		for split := 0; split <= len(input); split++ {
			t.Run(fmt.Sprintf("%s/split-%d", name, split), func(t *testing.T) {
				assertPTYUTF8Chunks(t, input, [][]byte{input[:split], input[split:]}, reference, setup)
			})
		}
		t.Run(name+"/single-byte", func(t *testing.T) {
			chunks := make([][]byte, len(input))
			for i := range input {
				chunks[i] = input[i : i+1]
			}
			assertPTYUTF8Chunks(t, input, chunks, reference, setup)
		})
	}
}

func assertPTYUTF8Chunks(t *testing.T, input []byte, chunks [][]byte, reference vt10x.Terminal, setup bool) {
	t.Helper()
	screen := vt10x.New(vt10x.WithSize(40, 8), vt10x.WithWriter(io.Discard))
	var raw bytes.Buffer
	var mu sync.Mutex
	var pending []byte
	filter := ptyUIControlFilter{}
	controls := []string{ansi.RequestKittyKeyboard, ansi.PushKittyKeyboard(1), ansi.PopKittyKeyboard(1)}
	for _, chunk := range chunks {
		if setup {
			testBinaryFirstRunStateN(screen, &mu, &raw, chunk, controls, &pending, len(chunk))
			continue
		}
		raw.Write(chunk)
		_, err := screen.Write(filter.write(chunk))
		require.NoError(t, err)
	}
	require.Equal(t, string(input), raw.String(), "raw PTY output must remain byte-for-byte intact")
	require.Empty(t, filter.pending)
	require.Empty(t, pending)
	require.Equal(t, reference.String(), screen.String(), "read boundaries must not change visible Unicode or cursor positioning")
	require.Equal(t, reference.Cursor(), screen.Cursor())
	require.Equal(t, reference.Cell(0, 1), screen.Cell(0, 1), "reverse selection must survive fragmented SGR and Unicode")
	require.Contains(t, screen.String(), "│ ação 漢字 🙂 👩‍💻")
}
