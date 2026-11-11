package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// The real permission service, the real bash tool and the real screen: the
// command waits for the card, Enter runs it and Esc leaves the disk untouched.
func TestApprovalCardGatesARealCommand(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(settings, []byte(`{}`), 0600))
	oldPath := viper.ConfigFileUsed()
	viper.SetConfigFile(settings)
	t.Cleanup(func() { viper.SetConfigFile(oldPath) })
	_, err := config.Load(root, false)
	require.NoError(t, err)
	useWorkspace(t, root)
	sandbox := config.Get().Security.Sandbox
	config.Get().Security.Sandbox = "none"
	t.Cleanup(func() { config.Get().Security.Sandbox = sandbox })

	perms := permission.NewPermissionService()
	manager := tools.NewProcessManager()
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	bash := tools.SecureTool(tools.NewBashTool(perms, manager), perms)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	requests := perms.Subscribe(ctx)
	ctx = context.WithValue(context.WithValue(ctx, tools.SessionIDContextKey, "s"), tools.MessageIDContextKey, "m")

	m := New(nil)
	m.app = &app.App{Permissions: perms}
	m.session = session.Session{ID: "s"}
	m.width, m.height = 120, 40

	type outcome struct {
		response tools.ToolResponse
		err      error
	}
	run := func(command, key string) outcome {
		done := make(chan outcome, 1)
		go func() {
			response, err := bash.Run(ctx, tools.ToolCall{ID: "call-" + key, Name: "bash", Input: `{"command":"` + command + `"}`})
			done <- outcome{response, err}
		}()
		select {
		case event := <-requests:
			m.Update(event)
		case <-ctx.Done():
			t.Fatal("the command never asked for approval")
		}
		view := ansi.Strip(m.View().Content)
		require.Contains(t, view, command, "the card shows what will run")
		select {
		case <-done:
			t.Fatal("the command ran before an answer")
		case <-time.After(50 * time.Millisecond):
		}
		m.key(tea.KeyPressMsg{Code: map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape}[key]})
		require.Nil(t, m.approval, "the answer closes the card")
		select {
		case result := <-done:
			return result
		case <-ctx.Done():
			t.Fatal("the answer never reached the command")
			return outcome{}
		}
	}

	denied := run("touch denied.txt", "esc")
	require.ErrorIs(t, denied.err, permission.ErrorPermissionDenied)
	require.NoFileExists(t, filepath.Join(root, "denied.txt"))

	allowed := run("touch allowed.txt", "enter")
	require.NoError(t, allowed.err)
	require.False(t, allowed.response.IsError, allowed.response.Content)
	require.FileExists(t, filepath.Join(root, "allowed.txt"))
}
