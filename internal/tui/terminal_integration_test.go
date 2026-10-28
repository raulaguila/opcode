package tui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

type terminalSessions struct{ session.Service }

func (*terminalSessions) Create(context.Context, string) (session.Session, error) {
	return session.Session{ID: "terminal-session"}, nil
}

type terminalMessages struct{ message.Service }

func (*terminalMessages) List(context.Context, string) ([]message.Message, error) { return nil, nil }

type terminalAgent struct{ agent.Service }

func (*terminalAgent) Model() models.Model       { return models.SupportedModels[models.GPT41] }
func (*terminalAgent) IsBusy() bool              { return false }
func (*terminalAgent) IsSessionBusy(string) bool { return false }

type terminalUIHarness struct {
	model  tea.Model
	frames chan string
}

func (m *terminalUIHarness) Init() tea.Cmd {
	return tea.Batch(m.model.Init(), func() tea.Msg { return tea.WindowSizeMsg{Width: 100, Height: 30} })
}
func (m *terminalUIHarness) View() string { return m.model.View() }
func (m *terminalUIHarness) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(dialog.ShowInitDialogMsg); ok {
		msg = dialog.ShowInitDialogMsg{Show: false}
	}
	updated, cmd := m.model.Update(msg)
	m.model = updated
	frame := ansi.Strip(updated.View())
	select {
	case m.frames <- frame:
	default:
	}
	return m, cmd
}

func isolateTUIStorage(t *testing.T) {
	t.Helper()
	cfg := config.Get()
	require.NotNil(t, cfg)
	originalRoot, originalData := cfg.WorkingDir, cfg.Data.Directory
	root := t.TempDir()
	cfg.WorkingDir = root
	cfg.Data.Directory = filepath.Join(root, "state")
	t.Cleanup(func() { cfg.WorkingDir = originalRoot; cfg.Data.Directory = originalData })
}

func TestUserApprovesTerminalEntersInputAndReturnsToChat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interactive PTYs require Unix")
	}
	t.Setenv("LOCAL_ENDPOINT", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if config.Get() == nil {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"security":{"sandbox":"none"},"shell":{"path":"/bin/bash","args":["--noprofile","--norc"]},"providers":{"openai":{"apiKey":"unit-test-key"}}}`), 0600))
		viper.SetConfigFile(path)
		_, err := config.Load(dir, false)
		require.NoError(t, err)
	}
	isolateTUIStorage(t)
	original := config.Get().Security
	config.Get().Security = config.SecurityConfig{Sandbox: "none"}
	t.Cleanup(func() { config.Get().Security = original })
	manager := tools.NewProcessManager()
	defer manager.Close()
	application := &app.App{Processes: manager, Permissions: permission.NewPermissionService(), Sessions: &terminalSessions{}, Messages: &terminalMessages{}, CoderAgent: &terminalAgent{}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	harness := &terminalUIHarness{model: New(application), frames: make(chan string, 128)}
	program := tea.NewProgram(harness, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())
	requests := application.Permissions.Subscribe(ctx)
	go func() {
		for request := range requests {
			program.Send(request)
		}
	}()
	done := make(chan error, 1)
	finished := false
	go func() { _, err := program.Run(); done <- err }()
	t.Cleanup(func() {
		if finished {
			return
		}
		program.Quit()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})
	waitFrame := func(text string) {
		t.Helper()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case frame := <-harness.frames:
				if strings.Contains(frame, text) {
					return
				}
			case <-deadline.C:
				t.Fatalf("UI never showed %q", text)
			}
		}
	}
	waitFrame("Opcode")
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	waitFrame("Allow (a)")
	require.Empty(t, manager.List("terminal-session"), "no process starts before approval")
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	waitFrame("Terminal • running")
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("printf 'PTY_%s\\n' OK")})
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitFrame("PTY_OK")
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	waitFrame("Terminal running")
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlC}) // Must interrupt the child, not open Quit.
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("exit")})
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitFrame("Terminal • exit")
	program.Quit()
	select {
	case err := <-done:
		finished = true
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("UI did not shut down")
	}
}
