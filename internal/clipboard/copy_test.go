package clipboard

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSSHCopyUsesOnlyEncodedTerminalOutput(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "fixture")
	t.Setenv("PATH", t.TempDir())
	var out bytes.Buffer
	confirmed, err := Copy(context.Background(), "ação $(not shell)\x1b]0;x\x07", &out)
	require.NoError(t, err)
	require.False(t, confirmed)
	sequence, err := OSC52("ação $(not shell)\x1b]0;x\x07")
	require.NoError(t, err)
	require.Equal(t, sequence, out.String())
	require.NotContains(t, out.String(), "not shell")
	_, err = OSC52(strings.Repeat("a", MaxBytes+1))
	require.Error(t, err)
	_, err = Copy(context.Background(), "text", nil)
	require.Error(t, err)
}

func TestNativeCopyPassesLiteralStdinAndReportsFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture executable uses a POSIX interpreter")
	}
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WAYLAND_DISPLAY", "fixture")
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	capture := filepath.Join(dir, "capture")
	t.Setenv("CLIPBOARD_TEST_CAPTURE", capture)
	name := "wl-copy"
	if runtime.GOOS == "darwin" {
		name = "pbcopy"
	}
	helper := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(helper, []byte("#!/bin/sh\n/bin/cat > \"$CLIPBOARD_TEST_CAPTURE\"\n"), 0700))
	literal := "ação $(touch unexpected) `not a command`\nsecond line"
	confirmed, err := Copy(context.Background(), literal, nil)
	require.NoError(t, err)
	require.True(t, confirmed)
	raw, err := os.ReadFile(capture)
	require.NoError(t, err)
	require.Equal(t, literal, string(raw))

	require.NoError(t, os.WriteFile(helper, []byte("#!/bin/sh\nexit 1\n"), 0700))
	confirmed, err = Copy(context.Background(), literal, nil)
	require.Error(t, err)
	require.False(t, confirmed)

	require.NoError(t, os.WriteFile(helper, []byte("#!/bin/sh\nexec /bin/sleep 5\n"), 0700))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	confirmed, err = Copy(ctx, literal, nil)
	require.Error(t, err)
	require.False(t, confirmed)
}
