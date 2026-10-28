//go:build integration

package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

// Exercises the shipped binary in a real 80x24 PTY, with a fresh HOME and no
// inherited credentials. Only the local Ollama catalog/status endpoints are
// requested; no generation or model downloads are part of this test.
func TestLiveBinaryFirstRunContractAndWorkspace(t *testing.T) {
	testBinaryFirstRun(t, false)
}

func TestLiveBinaryResetConfigurationAndWorkspace(t *testing.T) {
	testBinaryFirstRun(t, true)
}

func testBinaryFirstRun(t *testing.T, reset bool) {
	t.Helper()
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || os.Getenv("OPCODE_LIVE_OLLAMA") != "1" {
		t.Skip("requires explicit local acceptance-test opt-in")
	}
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux and Bubblewrap")
	}
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary), "build a binary and set OPCODE_EVAL_BINARY")
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workspace := filepath.Join(root, "workspace")
	require.NoError(t, os.MkdirAll(home, 0700))
	require.NoError(t, os.MkdirAll(workspace, 0700))
	args := []string{"--cwd", workspace}
	original := []byte(`{"private":"binary-reset-secret","invalid":`)
	if reset {
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".opcode"), 0700))
		require.NoError(t, os.WriteFile(filepath.Join(home, ".opcode", "config.json"), original, 0600))
		args = append(args, "--reset")
	}
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# LocalAcceptance\nA disposable UI acceptance project.\n"), 0600))
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "README.md"}, {"-c", "user.name=Acceptance", "-c", "user.email=acceptance@example.invalid", "commit", "-qm", "initial"}} {
		git := exec.Command("git", append([]string{"-C", workspace}, args...)...)
		git.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1"}
		output, err := git.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "SHELL=/bin/bash", "LANG=C.UTF-8", "TERM=xterm-256color"}
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 24, Cols: 80})
	require.NoError(t, err)
	defer terminal.Close()
	defer command.Process.Kill()
	screen := vt10x.New(vt10x.WithSize(80, 24), vt10x.WithWriter(terminal))
	var mu sync.Mutex
	var rawOutput bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buffer := make([]byte, 8192)
		// vt10x predates Kitty keyboard controls and treats every CSI-u as
		// cursor restore. Consume only those mode controls in the test display;
		// retain the complete raw stream for the negotiation/cleanup assertions.
		controls := []string{ansi.RequestKittyKeyboard, ansi.PushKittyKeyboard(1), ansi.PopKittyKeyboard(1)}
		var pending []byte
		for {
			n, err := terminal.Read(buffer)
			if n > 0 {
				mu.Lock()
				rawOutput.Write(buffer[:n])
				var display []byte
				for _, b := range buffer[:n] {
					pending = append(pending, b)
					for len(pending) > 0 {
						prefix := false
						for _, control := range controls {
							if strings.HasPrefix(control, string(pending)) {
								prefix = true
								if control == string(pending) {
									pending = nil
								}
								break
							}
						}
						if prefix {
							break
						}
						display = append(display, pending[0])
						pending = pending[1:]
					}
				}
				screen.Write(display)
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	snapshot := func() string { mu.Lock(); defer mu.Unlock(); return screen.String() }
	wait := func(markers ...string) {
		t.Helper()
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			frame := snapshot()
			for _, marker := range markers {
				if strings.Contains(frame, marker) {
					return
				}
			}
			select {
			case <-ticker.C:
			case <-deadline.C:
				t.Fatalf("binary did not show expected UI %v; screen:\n%s", markers, frame)
			case <-ctx.Done():
				t.Fatal("binary acceptance timed out")
			}
		}
	}
	send := func(text string) { t.Helper(); _, err := io.WriteString(terminal, text); require.NoError(t, err) }
	closeDialog := func(title string) {
		t.Helper()
		send("\x1b")
		// The composer remains visible behind modals; its hint alone cannot
		// establish that Escape has been consumed before typing the next command.
		require.Eventually(t, func() bool { return !strings.Contains(snapshot(), title) }, 5*time.Second, 20*time.Millisecond)
		wait("/ commands")
	}
	wait("Connections and preferences")
	send("n")
	wait("Add connection")
	send("\r")
	wait("Configure provider")
	send("\x13")
	wait("Setup · 2/3")
	wait("Provider catalog")
	send("llama3.1:8b")
	wait("llama3.1:8b")
	send("\x04")
	wait("model maximum", "server allocation", "server parameter")
	send("\r")
	wait("Setup · 3/3")
	// Runtime metadata must be visible in the picker without being written into
	// the user's configuration as an apparently manual limit.
	configJSON, err := os.ReadFile(filepath.Join(home, ".opcode", "config.json"))
	require.NoError(t, err)
	example, err := os.ReadFile(filepath.Join(home, "opcode", "config.example.json"))
	require.NoError(t, err)
	require.True(t, json.Valid(example))
	require.Contains(t, string(example), `"apiKeyEnv"`)
	require.NotContains(t, string(example), "binary-reset-secret")
	var saved struct {
		Models map[string]models.Model `json:"models"`
	}
	require.NoError(t, json.Unmarshal(configJSON, &saved))
	foundLocal := false
	for _, model := range saved.Models {
		if model.APIModel == "llama3.1:8b" {
			foundLocal = true
			require.True(t, model.UsesAutomaticContext())
			require.Empty(t, model.ContextSource)
			require.Zero(t, model.ContextWindow)
		}
	}
	require.True(t, foundLocal)
	send("\r") // Primary action starts the conversation without running tests.
	require.Eventually(t, func() bool { return !strings.Contains(snapshot(), "Setup · 3/3") }, 5*time.Second, 20*time.Millisecond)
	wait("/ commands")
	// Acknowledge enhanced-key support, then exercise an encoded Ctrl+O.
	send("\x1b[?0u")
	send("\x1b[111;5u")
	wait("Select main model")
	require.NotContains(t, snapshot(), "Setup · 2/3", "normal model picker must leave the setup presentation behind")
	closeDialog("Select main model")
	frame := strings.Split(snapshot(), "\n")
	if path := os.Getenv("OPCODE_UI_SNAPSHOT"); path != "" {
		require.NoError(t, os.WriteFile(path, []byte(snapshot()), 0600))
	}
	promptRow, footerRow := -1, -1
	for row, line := range frame {
		if strings.TrimSpace(line) == ">" {
			promptRow = row
		}
		if strings.Contains(line, "/ commands") {
			footerRow = row
		}
	}
	require.NotEqual(t, -1, promptRow, "chat composer must be visible")
	require.GreaterOrEqual(t, footerRow-promptRow, 3, "empty composer must retain three writing rows")
	require.NotContains(t, snapshot(), "/mode ask|plan|execute")
	require.NotContains(t, snapshot(), "No diagnostics")
	require.NotContains(t, snapshot(), "Context:")
	send("/contract\r")
	wait("Expected outcome")
	send("\r")
	wait("Configure task: explain")
	send("\x13")
	require.Eventually(t, func() bool { return !strings.Contains(snapshot(), "Configure task: explain") }, 5*time.Second, 20*time.Millisecond)
	send("/local-status\r")
	wait("Allocated context:", "Model is not loaded.")
	closeDialog("Ollama · observed allocation")
	send("/worktrees\r")
	wait("Project · Git and worktrees")
	wait("Branch: main")
	send("\r")
	wait("Git diff")
	closeDialog("Git diff")
	send("first line\x1b[13;2usecond line\x1b\rthird line\nfourth line")
	wait("fourth line")
	lines := strings.Split(snapshot(), "\n")
	previous := -1
	for _, text := range []string{"first line", "second line", "third line", "fourth line"} {
		row := -1
		for index, line := range lines {
			if strings.Contains(line, text) {
				row = index
				break
			}
		}
		require.Greater(t, row, previous, "newline shortcut must keep %q on its own row:\n%s", text, strings.Join(lines, "\n"))
		previous = row
	}
	send("\x1b[99;5u")
	wait("Are you sure you want to quit?")
	send("y")
	require.NoError(t, command.Wait())
	terminal.Close()
	<-readDone
	require.Contains(t, rawOutput.String(), ansi.RequestKittyKeyboard)
	require.Contains(t, rawOutput.String(), ansi.PushKittyKeyboard(1))
	pop := strings.LastIndex(rawOutput.String(), ansi.PopKittyKeyboard(1))
	exitAlt := strings.LastIndex(rawOutput.String(), "\x1b[?1049l")
	require.GreaterOrEqual(t, pop, 0, "keyboard mode must be restored")
	require.Greater(t, exitAlt, pop, "restore keys before leaving the alternate screen")
	data, err := os.ReadFile(filepath.Join(home, ".opcode", "config.json"))
	require.NoError(t, err)
	var configuration map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &configuration))
	require.Contains(t, string(configuration["agents"]), "llama3.1:8b")
	require.Contains(t, string(configuration["security"]), "bubblewrap")
	require.NotContains(t, string(data), `"apiKey":`)
	if reset {
		backups, err := filepath.Glob(filepath.Join(home, ".opcode", "config-backups", "*"))
		require.NoError(t, err)
		require.Len(t, backups, 1)
		backup, err := os.ReadFile(backups[0])
		require.NoError(t, err)
		require.Equal(t, original, backup)
	}
	t.Log("Setup, Ollama metadata, Git diff, multiline shortcuts and keyboard restoration passed in a real 80x24 PTY without inference")
}
