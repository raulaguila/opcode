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
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func emptySetupConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	if config.Get() == nil {
		dir := t.TempDir()
		file := filepath.Join(dir, "config.json")
		require.NoError(t, os.WriteFile(file, []byte(`{}`), 0600))
		viper.SetConfigFile(file)
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("LOCAL_ENDPOINT", "")
		_, err := config.Load(dir, false)
		require.NoError(t, err)
	}
	c := config.Get()
	previous := *c
	previousPath := viper.ConfigFileUsed()
	previousModels := maps.Clone(models.SupportedModels)
	root := t.TempDir()
	file := filepath.Join(root, "global.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"future":{"keep":true}}`), 0600))
	viper.SetConfigFile(file)
	*c = config.Config{WorkingDir: root, Data: config.Data{Directory: filepath.Join(root, "state")}, Runtime: (config.RuntimeConfig{}).Defaults(), Security: config.SecurityConfig{Sandbox: "none"}, Shell: config.ShellConfig{Path: "/bin/sh"}, Providers: map[models.ModelProvider]config.Provider{}, ProviderProfiles: map[string]config.ProviderProfile{}, Models: map[models.ModelID]models.Model{}, Agents: map[config.AgentName]config.Agent{}}
	t.Cleanup(func() { *c = previous; viper.SetConfigFile(previousPath); models.SupportedModels = previousModels })
	return c, file
}
func TestFirstRunConfiguresNamedProviderAndSelectsInstalledModelThroughKeyboard(t *testing.T) {
	cfg, file := emptySetupConfig(t)
	previousTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	http.DefaultTransport = modelUITransport(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "local.invalid", req.URL.Host)
		status, body := 404, `{}`
		if req.URL.Path == "/v1/models" {
			status, body = 200, `{"data":[{"id":"llama3.1:8b"},{"id":"qwen:7b"}]}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	conn, err := db.Connect()
	require.NoError(t, err)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	application, err := app.New(ctx, conn)
	require.NoError(t, err)
	defer application.Shutdown()
	require.Empty(t, application.CoderAgent.Model().ID)
	harness := &terminalUIHarness{model: New(application), frames: make(chan string, 256)}
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
	last := ""
	wait := func(text string) {
		t.Helper()
		timer := time.NewTimer(4 * time.Second)
		defer timer.Stop()
		for {
			select {
			case frame := <-harness.frames:
				last = frame
				if strings.Contains(frame, text) {
					return
				}
			case <-timer.C:
				t.Fatalf("setup UI did not show %q; last frame:\n%s", text, last)
			}
		}
	}
	wait("Connections and preferences")
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	wait("Add connection")
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	wait("Configure provider")
	// The endpoint is the fourth field; replace its default without ever entering a key.
	for range 3 {
		program.Send(tea.KeyMsg{Type: tea.KeyTab})
	}
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlU})
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("http://local.invalid/v1")})
	wait("http://local.invalid/v1")
	program.Send(tea.KeyMsg{Type: tea.KeyCtrlS})
	wait("Provider catalog · 2 models")
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("llama3.1:8b")})
	wait("Search: llama3.1:8b")
	program.Send(tea.KeyMsg{Type: tea.KeyEnter})
	wait("Model changed to")
	program.Quit()
	require.NoError(t, <-done)
	finished = true
	selected := application.CoderAgent.Model()
	require.Equal(t, "ollama", selected.ProviderProfile)
	require.Equal(t, "ollama", selected.API)
	require.Equal(t, "llama3.1:8b", selected.APIModel)
	require.NoError(t, cfg.ValidateStartup())
	require.Equal(t, selected.ID, cfg.Agents[config.AgentTitle].Model)
	saved, err := os.ReadFile(file)
	require.NoError(t, err)
	var document map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(saved, &document))
	require.Contains(t, document, "providerProfiles")
	require.Contains(t, string(saved), "profile.ollama.llama3.1:8b")
	require.Contains(t, string(saved), "future")
}
func TestSettingsSwitchesAreExclusiveAndCancelPreviousCatalog(t *testing.T) {
	_, _ = emptySetupConfig(t)
	application := &app.App{Sessions: &terminalSessions{}, Messages: &terminalMessages{}, CoderAgent: &modelUIAgent{}}
	model := *New(application).(*appModel)
	model.showModelDialog = true
	updated, cmd := model.Update(dialog.OpenIntegrationDialogMsg{})
	model = updated.(appModel)
	require.False(t, model.showModelDialog)
	require.True(t, model.showIntegrationDialog)
	require.False(t, model.showProviderDialog)
	_ = cmd
	updated, _ = model.Update(dialog.OpenProviderDialogMsg{})
	model = updated.(appModel)
	require.False(t, model.showIntegrationDialog)
	require.True(t, model.showProviderDialog)
	model.showInitDialog = true
	updated, _ = model.Update(page.OpenModelsMsg{})
	model = updated.(appModel)
	require.False(t, model.showProviderDialog)
	require.False(t, model.showIntegrationDialog)
	require.False(t, model.showInitDialog)
	require.True(t, model.showModelDialog)
	model.modelDialog.Close()
}
