package lsp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
)

var ErrExecutableUnavailable = errors.New("language server executable unavailable")

type executableUnavailableError struct{ label string }

func (e executableUnavailableError) Error() string {
	return fmt.Sprintf(config.Text("%s indisponível; instale-o ou configure /integrations", "%s unavailable; install it or configure /integrations"), e.label)
}
func (e executableUnavailableError) Unwrap() error { return ErrExecutableUnavailable }

// ExecutableLabel never exposes configured paths, arguments or terminal escapes.
func ExecutableLabel(command string) string {
	name := filepath.Base(command)
	if _, known := Presets[name]; known {
		return name
	}
	return config.Text("Servidor LSP", "Language server")
}

// CheckExecutable checks the real server before the shell/sandbox wrapper starts.
// Relative executable paths are resolved in the same workspace as the wrapper.
// This does not install anything or bypass the shell's security policy.
func CheckExecutable(command, workingDir string) error {
	lookup := command
	if strings.ContainsRune(command, filepath.Separator) && !filepath.IsAbs(command) {
		lookup = filepath.Join(workingDir, command)
	}
	if command == "" {
		return executableUnavailableError{ExecutableLabel(command)}
	}
	if _, err := exec.LookPath(lookup); err != nil {
		return executableUnavailableError{ExecutableLabel(command)}
	}
	return nil
}

// StderrStatus contains aggregate diagnostics only. Raw server stderr is never
// retained, logged or sent to the renderer because it may contain credentials.
type StderrStatus struct {
	Bytes      int64
	ReadFailed bool
	Done       bool
}

func (c *Client) StderrStatus() StderrStatus {
	s := StderrStatus{}
	if c.stderrDone != nil {
		select {
		case <-c.stderrDone:
			s.Done = true
		default:
		}
	}
	s.Bytes, s.ReadFailed = c.stderrBytes.Load(), c.stderrReadFailed.Load()
	return s
}

func (c *Client) drainStderr(ctx context.Context) {
	defer close(c.stderrDone)
	defer c.stderr.Close()
	stop := context.AfterFunc(ctx, func() { _ = c.stderr.Close() })
	defer stop()
	// Fixed memory, regardless of line length or output volume. Keep draining
	// after long lines so a noisy server cannot block its protocol responses.
	var buffer [4096]byte
	for {
		if ctx.Err() != nil {
			return
		}
		n, err := c.stderr.Read(buffer[:])
		c.stderrBytes.Add(int64(n))
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				c.stderrReadFailed.Store(true)
			}
			return
		}
	}
}
