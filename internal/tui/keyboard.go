package tui

import (
	"io"
	"sync"

	"github.com/charmbracelet/x/term"
)

// TerminalWriter serializes renderer output and explicit terminal writes such
// as OSC 52. Bubble Tea owns keyboard decoding and terminal mode lifecycle.
type TerminalWriter struct {
	mu     sync.Mutex
	output io.Writer
}

var _ term.File = (*TerminalWriter)(nil)

func NewTerminalWriter(output io.Writer) *TerminalWriter {
	return &TerminalWriter{output: output}
}

// Close satisfies terminal capability detection without owning the supplied file.
func (*TerminalWriter) Close() error { return nil }

func (w *TerminalWriter) Fd() uintptr {
	if file, ok := w.output.(interface{ Fd() uintptr }); ok {
		return file.Fd()
	}
	return ^uintptr(0)
}

func (w *TerminalWriter) Read(raw []byte) (int, error) {
	if reader, ok := w.output.(io.Reader); ok {
		return reader.Read(raw)
	}
	return 0, io.EOF
}

func (w *TerminalWriter) Write(raw []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.output.Write(raw)
}
