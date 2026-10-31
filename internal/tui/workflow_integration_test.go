package tui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

type workflowUIAgent struct {
	terminalAgent
	store *workflow.Store
}

func (a *workflowUIAgent) SetMode(ctx context.Context, id string, mode workflow.Mode) error {
	_, err := a.store.SetMode(ctx, id, mode)
	return err
}

func TestHeadlessPlanModeAndUserQuestionPreserveDraft(t *testing.T) {
	if config.Get() == nil {
		root := t.TempDir()
		path := filepath.Join(root, "config.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"providers":{"openai":{"apiKey":"unit-test-key"}}}`), 0600))
		viper.SetConfigFile(path)
		_, err := config.Load(root, false)
		require.NoError(t, err)
	}
	isolateTUIStorage(t)
	previousLanguage := config.Get().TUI.Language
	config.Get().TUI.Language = "pt"
	t.Cleanup(func() { config.Get().TUI.Language = previousLanguage })
	store := workflow.New(nil, t.TempDir(), workflow.Execute)
	questions := interaction.NewQuestions()
	_, err := store.UpdatePlan(context.Background(), "s", "Corrigir e validar", []workflow.Step{{Title: "Investigar", Status: "completed"}, {Title: "Validar", Status: "in_progress"}}, []string{"Teste passa"}, nil)
	require.NoError(t, err)
	application := &app.App{Workflows: store, Questions: questions, Sessions: &terminalSessions{}, Messages: &terminalMessages{}, Permissions: permission.NewPermissionService(), CoderAgent: &workflowUIAgent{store: store}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	harness := &terminalUIHarness{model: New(application, session.Session{ID: "s", Title: "Workflow"}), frames: make(chan string, 128)}
	program := tea.NewProgram(harness, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	defer program.Quit()
	wait := func(text string) {
		t.Helper()
		lastFrame := ""
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case frame := <-harness.frames:
				lastFrame = frame
				if strings.Contains(frame, text) {
					return
				}
			case <-timer.C:
				t.Fatalf("UI did not render %q; last frame:\n%s", text, lastFrame)
			}
		}
	}
	wait("Executar")
	program.Send(chat.SessionSelectedMsg(session.Session{ID: "s", Title: "Workflow"}))
	program.Send(page.WorkflowCommandMsg{Command: "plan"})
	wait("Ajustar plano")
	program.Send(tea.KeyPressMsg{Code: tea.KeyEsc, Mod: 0})
	program.Send(testTextKey(string([]rune("/mod")), 0))
	wait("Escolher modo de trabalho")
	program.Send(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: 0})
	wait("somente leitura")
	program.Send(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: 0})
	wait("Perguntar")
	program.Send(testTextKey(string([]rune("Rascunho intocado")), 0))
	wait("Rascunho intocado")
	answer := make(chan string, 1)
	go func() {
		text, err := questions.Ask(ctx, interaction.Question{SessionID: "s", Question: "Qual banco?", Choices: []string{"SQLite", "Postgres"}})
		if err != nil {
			answer <- err.Error()
		} else {
			answer <- text
		}
	}()
	wait("Qual banco?")
	program.Send(testTextKey(string([]rune("Minha escolha")), 0))
	program.Send(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: 0})
	select {
	case text := <-answer:
		require.Equal(t, "Minha escolha", text)
	case <-ctx.Done():
		t.Fatal("question was not answered")
	}
	wait("Rascunho intocado")
	require.False(t, application.Permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: "s", ToolName: "bash", Action: "execute", Path: config.WorkingDirectory()}), "answering a question never grants command permission")
	program.Quit()
	require.NoError(t, <-done)
}
