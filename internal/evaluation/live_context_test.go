//go:build integration

package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appconfig "github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

// Unlike the fixed-window quality fixtures, this exercises automatic discovery,
// a real first question and the actual allocation made by the production binary.
func TestLiveOllamaFirstQuestionUsesAdvertisedContext(t *testing.T) {
	testLiveOllamaFirstQuestion(t, false)
}

func TestLiveOllamaLargeRepositoryFitsSixteenKContext(t *testing.T) {
	testLiveOllamaFirstQuestion(t, true)
}

func testLiveOllamaFirstQuestion(t *testing.T, fixedWindow bool) {
	t.Helper()
	if appconfig.Environment("OPCODE_LIVE_TESTS") != "1" || appconfig.Environment("OPCODE_LIVE_OLLAMA") != "1" {
		t.Skip("requires explicit local Ollama inference opt-in")
	}
	binary := appconfig.Environment("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary), "set OPCODE_EVAL_BINARY to a freshly built production executable")
	endpoint := os.Getenv("LOCAL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434/v1"
	}
	parsed, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1", "::1"}, parsed.Hostname())
	modelName := appconfig.Environment("OPCODE_LIVE_OLLAMA_MODEL")
	if modelName == "" {
		modelName = "llama3.1:8b"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	selected := installedLiveOllamaModel(t, ctx, endpoint, modelName)
	root := t.TempDir()
	home, workspace := filepath.Join(root, "home"), filepath.Join(root, "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".opcode"), 0700))
	require.NoError(t, os.Mkdir(workspace, 0700))
	files := map[string]string{
		"README.md": "# ContextProbe\nA Go program for checking temperature sensors.\n" + strings.Repeat("Sensor readings are displayed in the terminal.\n", 120),
		"go.mod":    "module example.invalid/contextprobe\n\ngo 1.26\n",
		"main.go":   "package main\nimport \"fmt\"\nfunc main() { fmt.Println(\"sensor ready\") }\n",
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(workspace, name), []byte(content), 0600))
	}
	wantWindow, mode, maxTokens := selected.ContextWindow, "ask", 256
	if fixedWindow {
		wantWindow, mode, maxTokens = 16384, "execute", 2048
		for i := range 600 {
			path := filepath.Join(workspace, "internal", fmt.Sprintf("package_%03d", i), "implementation.go")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
			require.NoError(t, os.WriteFile(path, []byte("package example\n"), 0600))
		}
	}
	configuration := map[string]any{
		"providers": map[string]any{"local": map[string]any{"baseURL": endpoint, "api": "ollama", "defaultMaxTokens": maxTokens}},
		"models":    map[string]any{string(selected.ID): map[string]any{"provider": "local", "api": "ollama", "api_model": modelName}},
		"agents":    map[string]any{"coder": map[string]any{"model": selected.ID}, "title": map[string]any{"model": selected.ID}, "summarizer": map[string]any{"model": selected.ID}, "task": map[string]any{"model": selected.ID}},
	}
	if fixedWindow {
		configuration["models"].(map[string]any)[string(selected.ID)].(map[string]any)["context_window"] = wantWindow
	}
	raw, err := json.Marshal(configuration)
	require.NoError(t, err)
	configPath := filepath.Join(home, ".opcode", "config.json")
	require.NoError(t, os.WriteFile(configPath, raw, 0600))
	command := exec.CommandContext(ctx, binary, "--cwd", workspace, "--mode", mode, "--prompt", "O que este projeto faz? Responda em até duas frases com base nos arquivos, sem executar comandos ou alterar arquivos.", "--quiet", "--output-format", "json", "--max-steps", "8", "--max-seconds", "220", "--first-token-timeout", "180")
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "SHELL=/bin/bash", "LANG=C.UTF-8"}
	output, err := command.CombinedOutput()
	require.NoError(t, err, "production first question failed: %s", output)
	var response struct {
		Response string `json:"response"`
	}
	require.NoError(t, json.Unmarshal(output, &response))
	require.NotEmpty(t, strings.TrimSpace(response.Response))
	require.Contains(t, strings.ToLower(response.Response), "sensor", "answer must reflect the real README")
	requireLiveOllamaAllocation(t, ctx, endpoint, modelName, wantWindow)
	for name, expected := range files {
		actual, err := os.ReadFile(filepath.Join(workspace, name))
		require.NoError(t, err)
		require.Equal(t, expected, string(actual))
	}
	saved, err := os.ReadFile(configPath)
	require.NoError(t, err)
	if fixedWindow {
		var original, persisted map[string]any
		require.NoError(t, json.Unmarshal(raw, &original))
		require.NoError(t, json.Unmarshal(saved, &persisted))
		for _, key := range []string{"models", "providers", "agents"} {
			require.Equal(t, original[key], persisted[key], "manual model and connection settings remain unchanged")
		}
	} else {
		require.NotContains(t, string(saved), "context_window")
	}
	t.Logf("First project question passed with %d tokens allocated, mode %s and unchanged source files", wantWindow, mode)
}

func installedLiveOllamaModel(t *testing.T, ctx context.Context, endpoint, modelName string) models.Model {
	t.Helper()
	catalog, err := models.DiscoverLocalModels(ctx, endpoint, "")
	require.NoError(t, err)
	var selected models.Model
	for _, model := range catalog {
		if model.APIModel == modelName {
			selected = model
			break
		}
	}
	require.NotEmpty(t, selected.ID, "the model must already be installed; this test never downloads weights")
	require.Contains(t, []string{"ollama:model", "ollama:parameter"}, selected.ContextSource)

	return selected
}

func requireLiveOllamaAllocation(t *testing.T, ctx context.Context, endpoint, modelName string, wantWindow int64) {
	t.Helper()
	allocated, err := models.InspectOllama(ctx, endpoint, "")
	require.NoError(t, err)
	matched := false
	for _, model := range allocated {
		if model.Name == modelName || model.Name == modelName+":latest" {
			matched = true
			require.Equal(t, wantWindow, model.Context, "native request must honor discovery or the explicit manual limit")
		}
	}
	require.True(t, matched)
}
