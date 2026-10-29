//go:build !windows

package shell

import (
	"context"
	"io"
	"os/exec"
	"strings"
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
	defer terminal.Close()
	output := make(chan string, 1)
	go func() { raw, _ := io.ReadAll(io.LimitReader(terminal, 8192)); output <- string(raw) }()
	_, err = terminal.Write([]byte("fixture-input\n"))
	require.NoError(t, err)
	require.NoError(t, command.Wait())
	select {
	case data := <-output:
		require.True(t, strings.Contains(data, "NATIVE_PTY_READY"))
		require.Contains(t, data, "NATIVE_PTY_ACK:fixture-input")
	case <-ctx.Done():
		t.Fatal("PTY output did not close within fixture timeout")
	}
}
