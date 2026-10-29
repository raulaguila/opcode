//go:build !windows

package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Exercises only the Unix PTY primitive with an inert shell fixture. Application
// command permissions and Bubblewrap remain independent mandatory gates.
func TestNativeSmokeUnixPTYPrimitive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", "-c", `printf 'NATIVE_PTY_READY\n'; IFS= read -r answer; printf 'NATIVE_PTY_ACK:%s\n' "$answer"`)
	command.Env = Environment()
	command.Dir = t.TempDir()
	terminal, err := StartPTY(command, 80, 24)
	require.NoError(t, err)
	var output Buffer
	output.Limit = 8192
	ready := make(chan struct{})
	readDone := make(chan error, 1)
	waited, readerFinished := false, false
	defer func() {
		cancel()
		_ = terminal.Close()
		if !waited {
			_ = command.Wait()
		}
		if !readerFinished {
			select {
			case <-readDone:
			case <-time.After(time.Second):
				t.Error("PTY reader did not stop during cleanup")
			}
		}
	}()
	go func() {
		buffer := make([]byte, 512)
		total := 0
		announced := false
		for total < 8192 {
			n, readErr := terminal.Read(buffer[:min(len(buffer), 8192-total)])
			if n > 0 {
				total += n
				_, _ = output.Write(buffer[:n])
				text, _ := output.Snapshot()
				if !announced && strings.Contains(text, "NATIVE_PTY_READY") {
					close(ready)
					announced = true
				}
			}
			if readErr != nil {
				readDone <- readErr
				return
			}
		}
		readDone <- fmt.Errorf("PTY fixture output exceeded 8192 bytes")
	}()
	snapshot := func() string { text, _ := output.Snapshot(); return text }
	// Wait for the child to initialize its terminal before sending user input.
	// Readiness is live output, not inferred from exec.Start completing.
	select {
	case <-ready:
	case readErr := <-readDone:
		readerFinished = true
		t.Fatalf("PTY reader stopped before READY: %v; output=%q", readErr, snapshot())
	case <-ctx.Done():
		t.Fatalf("PTY never became READY: %v; output=%q", ctx.Err(), snapshot())
	}
	input := []byte("fixture-input\n")
	n, err := terminal.Write(input)
	require.NoError(t, err, "output=%q", snapshot())
	require.Equal(t, len(input), n)
	waitErr := command.Wait()
	waited = true
	if waitErr != nil {
		var readStatus error
		select {
		case readStatus = <-readDone:
			readerFinished = true
		default:
			readStatus = fmt.Errorf("reader still active")
		}
		t.Fatalf("PTY command failed: %v; reader=%v; output=%q", waitErr, readStatus, snapshot())
	}
	select {
	case readErr := <-readDone:
		readerFinished = true
		// Unix PTYs report peer closure as EOF (Darwin) or EIO (Linux).
		require.True(t, errors.Is(readErr, io.EOF) || errors.Is(readErr, syscall.EIO), "reader=%v; output=%q", readErr, snapshot())
	case <-ctx.Done():
		t.Fatalf("PTY output did not close: %v; output=%q", ctx.Err(), snapshot())
	}
	require.Contains(t, snapshot(), "NATIVE_PTY_READY")
	require.Contains(t, snapshot(), "NATIVE_PTY_ACK:fixture-input")
}
