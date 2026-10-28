//go:build integration

package tui

import (
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

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

// Exercises the shipped binary in a real 80x24 PTY, with a fresh HOME and no
// inherited credentials. Only the local Ollama catalog/status endpoints are
// requested; no generation or model downloads are part of this test.
func TestLiveBinaryFirstRunContractAndWorkspace(t *testing.T) {
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
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# LocalAcceptance\nA disposable UI acceptance project.\n"), 0600))
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "README.md"}, {"-c", "user.name=Acceptance", "-c", "user.email=acceptance@example.invalid", "commit", "-qm", "initial"}} {
		git := exec.Command("git", append([]string{"-C", workspace}, args...)...)
		git.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1"}
		output, err := git.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "--cwd", workspace)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "SHELL=/bin/bash", "LANG=C.UTF-8", "TERM=xterm-256color"}
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: 24, Cols: 80})
	require.NoError(t, err)
	defer terminal.Close()
	defer command.Process.Kill()
	screen := vt10x.New(vt10x.WithSize(80, 24), vt10x.WithWriter(terminal))
	var mu sync.Mutex
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buffer := make([]byte, 8192)
		for {
			n, err := terminal.Read(buffer)
			if n > 0 {
				mu.Lock()
				screen.Write(buffer[:n])
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
		wait("Enter sends")
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
	wait("model maximum", "server allocation", "server parameter")
	send("\r")
	wait("Setup · 3/3")
	// Runtime metadata must be visible in the picker without being written into
	// the user's configuration as an apparently manual limit.
	configJSON, err := os.ReadFile(filepath.Join(home, ".opcode", "config.json"))
	require.NoError(t, err)
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
	closeDialog("Setup · 3/3")
	send("\x0f")
	wait("Select main model")
	require.NotContains(t, snapshot(), "Setup · 2/3", "normal model picker must leave the setup presentation behind")
	closeDialog("Select main model")
	send("/contract\r")
	wait("Expected outcome")
	send("\r")
	wait("Configure task: explain")
	send("\x13")
	wait("Task: Explain")
	send("/local-status\r")
	wait("Allocated context:", "Model is not loaded.")
	closeDialog("Ollama · observed allocation")
	send("/worktrees\r")
	wait("Project · Git and worktrees")
	wait("Branch: main")
	send("\r")
	wait("Git diff")
	closeDialog("Git diff")
	send("\x03")
	wait("Are you sure you want to quit?")
	send("y")
	require.NoError(t, command.Wait())
	terminal.Close()
	<-readDone
	data, err := os.ReadFile(filepath.Join(home, ".opcode", "config.json"))
	require.NoError(t, err)
	var configuration map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &configuration))
	require.Contains(t, string(configuration["agents"]), "llama3.1:8b")
	require.Contains(t, string(configuration["security"]), "bubblewrap")
	require.NotContains(t, string(data), `"apiKey":`)
	t.Log("Fresh template, provider/model setup, explicit contract, Ollama allocation and Git diff passed in a real 80x24 PTY without inference")
}
