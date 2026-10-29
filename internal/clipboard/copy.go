// Package clipboard routes explicit copies without interpreting text as code.
package clipboard

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const MaxBytes = 64 * 1024

func Remote() bool { return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" }
func OSC52(text string) (string, error) {
	if len(text) > MaxBytes {
		return "", fmt.Errorf("copy exceeds 64 KiB; use text selection")
	}
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07", nil
}

// Copy never starts a clipboard process on the SSH server. OSC 52 has no receipt.
func Copy(ctx context.Context, text string, terminal io.Writer) (confirmed bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if len(text) > MaxBytes {
		return false, fmt.Errorf("copy exceeds 64 KiB; use text selection")
	}
	copyToTerminal := func() (bool, error) {
		if terminal == nil {
			return false, fmt.Errorf("terminal output unavailable; use text selection")
		}
		sequence, _ := OSC52(text)
		_, err = io.WriteString(terminal, sequence)
		return false, err
	}
	if Remote() {
		return copyToTerminal()
	}
	name, args := nativeHelper()
	if name == "" {
		if terminal != nil {
			return copyToTerminal()
		}
		return false, fmt.Errorf("native clipboard unavailable; use text selection")
	}
	nativeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	command := exec.CommandContext(nativeCtx, name, args...)
	command.Stdin = strings.NewReader(text)
	if command.Run() != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if terminal != nil {
			return copyToTerminal()
		}
		return false, fmt.Errorf("native clipboard failed or timed out; use text selection")
	}
	return true, nil
}
func nativeHelper() (string, []string) {
	choices := [][]string{}
	switch runtime.GOOS {
	case "darwin":
		choices = [][]string{{"pbcopy"}}
	case "windows":
		choices = [][]string{{"clip.exe"}}
	default:
		if os.Getenv("WSL_DISTRO_NAME") != "" {
			choices = append(choices, []string{"clip.exe"})
		}
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			choices = append(choices, []string{"wl-copy"})
		}
		choices = append(choices, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
	}
	for _, choice := range choices {
		if path, e := exec.LookPath(choice[0]); e == nil {
			return path, choice[1:]
		}
	}
	return "", nil
}

func ValidOSC52(sequence string) bool {
	if !strings.HasPrefix(sequence, "\x1b]52;c;") || !strings.HasSuffix(sequence, "\x07") || len(sequence) > 12+base64.StdEncoding.EncodedLen(MaxBytes) {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b]52;c;"), "\x07"))
	return err == nil && len(raw) <= MaxBytes
}
