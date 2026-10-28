package tui

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

type modelUIAgent struct{ terminalAgent }

func (a *modelUIAgent) SelectModel(name config.AgentName, m models.Model) (models.Model, error) {
	if err := config.UpdateAgentModelDefinition(name, m); err != nil {
		return models.Model{}, err
	}
	return m, nil
}

type modelUITransport func(*http.Request) (*http.Response, error)

func (f modelUITransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestUserClicksActiveModelSearchesProviderAndPersistsSelection(t *testing.T) {
	if config.Get() == nil {
		dir := t.TempDir()
		file := filepath.Join(dir, "config.json")
		require.NoError(t, os.WriteFile(file, []byte(`{"providers":{"openai":{"apiKey":"unit-test-key"}}}`), 0600))
		viper.SetConfigFile(file)
		working, err := os.Getwd()
		require.NoError(t, err)
		_, err = config.Load(working, false)
		require.NoError(t, err)
	}
	cfg := config.Get()
	old := *cfg
	registry := maps.Clone(models.SupportedModels)
	transport := http.DefaultTransport
	t.Cleanup(func() { *cfg = old; models.SupportedModels = registry; http.DefaultTransport = transport })
	root := t.TempDir()
	cfg.WorkingDir = root
	cfg.Data.Directory = filepath.Join(root, "state")
	selected := models.Model{ID: "local.old:8b", Name: "Old local", Provider: models.ProviderLocal, APIModel: "old:8b", API: "ollama", ContextWindow: 16384, DefaultMaxTokens: 2048}
	cfg.Providers = map[models.ModelProvider]config.Provider{models.ProviderLocal: {BaseURL: "http://local.invalid/v1", API: "ollama", ContextWindow: 16384, DefaultMaxTokens: 2048}}
	cfg.Agents = maps.Clone(cfg.Agents)
	cfg.Agents[config.AgentCoder] = config.Agent{Model: selected.ID, MaxTokens: 2048}
	cfg.Models = map[models.ModelID]models.Model{selected.ID: selected}
	models.SupportedModels[selected.ID] = selected
	data, err := json.Marshal(map[string]any{"providers": cfg.Providers, "agents": cfg.Agents, "models": cfg.Models})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".opcode.json"), data, 0600))
	http.DefaultTransport = modelUITransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "local.invalid", req.URL.Host)
		status, body := 404, `{}`
		if req.URL.Path == "/v1/models" {
			status, body = 200, `{"data":[{"id":"old:8b"},{"id":"new.model:7b"}]}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	application := &app.App{Sessions: &terminalSessions{}, Messages: &terminalMessages{}, Permissions: permission.NewPermissionService(), CoderAgent: &modelUIAgent{}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	harness := &terminalUIHarness{model: New(application), frames: make(chan string, 128)}
	program := tea.NewProgram(harness, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	finished := false
	defer func() {
		if !finished {
			program.Quit()
			<-done
		}
	}()
	lastFrame := ""
	sized := false
	wait := func(text string) string {
		t.Helper()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case frame := <-harness.frames:
				lastFrame = frame
				if strings.Contains(frame, text) && (sized || lipgloss.Width(frame) == 110) {
					return frame
				}
			case <-timer.C:
				t.Fatalf("model UI did not render %q; last frame:\n%s", text, lastFrame)
				return ""
			}
		}
	}
	program.Send(tea.WindowSizeMsg{Width: 110, Height: 32})
	frame := wait("Old local")
	sized = true
	// Locate the rendered footer, then deliver an actual Bubble Tea mouse event.
	lines := strings.Split(frame, "\n")
	last := lines[len(lines)-1]
	x := strings.Index(last, "Old local")
	require.GreaterOrEqual(t, x, 0)
	program.Send(tea.MouseMsg{X: x + 1, Y: len(lines) - 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	wait("Provider catalog · 2 models")
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new.model")})
	frame = wait("Search: new.model")
	lines = strings.Split(frame, "\n")
	y := -1
	x = -1
	for i, line := range lines {
		if strings.Contains(line, "new.model:7b") {
			y = i
			x = strings.Index(line, "new.model:7b")
			break
		}
	}
	require.GreaterOrEqual(t, y, 0)
	program.Send(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	wait("Model changed to")
	program.Quit()
	require.NoError(t, <-done)
	finished = true
	require.Equal(t, models.ModelID("local.new.model:7b"), cfg.Agents[config.AgentCoder].Model)
	require.Equal(t, "ollama", models.SupportedModels[cfg.Agents[config.AgentCoder].Model].API)
	saved, err := os.ReadFile(filepath.Join(root, ".opcode.json"))
	require.NoError(t, err)
	require.Contains(t, string(saved), "local.new.model:7b")
}
