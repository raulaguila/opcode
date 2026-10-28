package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func environment(values map[string]string) envLookup {
	return func(name string) (string, bool) { value, ok := values[name]; return value, ok }
}
func TestLiveConfigEnvironmentPrecedence(t *testing.T) {
	cfg := liveConfig{Providers: map[string]liveProvider{"openai": {Enabled: true, Model: "file-model", APIKey: "file-secret", MaxTokens: 512}, "anthropic": {Enabled: true, Model: "other-model", APIKey: "other-secret"}}}
	cases, timeout, err := resolveLiveCases(cfg, environment(map[string]string{"OPCODE_LIVE_PROVIDERS": "openai", "OPCODE_TEST_OPENAI_MODEL": "env-model", "OPENAI_API_KEY": "env-secret", "OPCODE_TEST_OPENAI_CAN_REASON": "true", "OPCODE_TEST_OPENAI_REASONING_EFFORT": "low", "OPCODE_TEST_OPENAI_MAX_TOKENS": "2048", "OPCODE_LIVE_TIMEOUT_SECONDS": "60"}))
	require.NoError(t, err)
	require.Equal(t, 60, timeout)
	require.Len(t, cases, 1)
	require.Equal(t, "env-model", cases[0].config.Model)
	require.Equal(t, "env-secret", cases[0].config.APIKey)
	require.EqualValues(t, 2048, cases[0].config.MaxTokens)
	require.True(t, cases[0].config.CanReason)
	require.Equal(t, "low", cases[0].config.ReasoningEffort)
	// An explicitly empty environment credential must not revive the file secret.
	_, _, err = resolveLiveCases(cfg, environment(map[string]string{"OPCODE_LIVE_PROVIDERS": "openai", "OPENAI_API_KEY": ""}))
	require.ErrorContains(t, err, "credential is missing")
}
func TestEnvOnlySelectionNeedsModelNotJustCredentials(t *testing.T) {
	cfg := liveConfig{}
	_, _, err := resolveLiveCases(cfg, environment(map[string]string{"OPENAI_API_KEY": "secret"}))
	require.ErrorContains(t, err, "no live provider selected")
	cases, _, err := resolveLiveCases(cfg, environment(map[string]string{"OPCODE_TEST_OPENAI_RESPONSES_MODEL": "account-model", "OPENAI_API_KEY": "secret", "OPCODE_TEST_OPENAI_RESPONSES_CAN_REASON": "true"}))
	require.NoError(t, err)
	require.Len(t, cases, 1)
	require.Equal(t, "responses", cases[0].spec.api)
	require.EqualValues(t, 4096, cases[0].config.MaxTokens)
	_, _, err = resolveLiveCases(cfg, environment(map[string]string{"OPCODE_LIVE_PROVIDERS": "openai"}))
	require.ErrorContains(t, err, "configure model")
}
func TestCustomKeyVariableAndCloudSettings(t *testing.T) {
	cfg := liveConfig{Providers: map[string]liveProvider{
		"azure":    {Enabled: true, Model: "deployment", APIKey: "file-secret", APIKeyEnv: "PRIVATE_AZURE_KEY", Endpoint: "https://file.test", APIVersion: "v1"},
		"vertexai": {Enabled: true, Model: "gemini-test", Project: "file-project", Location: "global", CredentialsFile: "/synthetic/adc.json"},
		"bedrock":  {Enabled: true, Model: "us.anthropic.test", APIKey: "bedrock-secret", Region: "us-west-2", AWSProfile: "test-profile"},
	}}
	cases, _, err := resolveLiveCases(cfg, environment(map[string]string{"PRIVATE_AZURE_KEY": "custom-secret", "AZURE_OPENAI_ENDPOINT": "https://environment.test", "GOOGLE_CLOUD_PROJECT": "env-project"}))
	require.NoError(t, err)
	require.Len(t, cases, 3)
	byName := map[string]liveCase{}
	for _, c := range cases {
		byName[c.spec.name] = c
	}
	require.Equal(t, "custom-secret", byName["azure"].config.APIKey)
	require.NotContains(t, byName["azure"].environment, "AZURE_OPENAI_ENDPOINT")
	require.Equal(t, "v1", byName["azure"].environment["AZURE_OPENAI_API_VERSION"])
	require.Equal(t, "env-project", byName["vertexai"].environment["VERTEXAI_PROJECT"])
	require.Equal(t, "/synthetic/adc.json", byName["vertexai"].environment["GOOGLE_APPLICATION_CREDENTIALS"])
	require.Equal(t, "bedrock-secret", byName["bedrock"].environment["AWS_BEARER_TOKEN_BEDROCK"])
	require.Equal(t, "test-profile", byName["bedrock"].environment["AWS_PROFILE"])
}
func TestConfigValidationDoesNotEchoValues(t *testing.T) {
	secret := "DO_NOT_PRINT_SECRET"
	cfg := liveConfig{Providers: map[string]liveProvider{"openai": {Enabled: true, Model: "test", APIKey: secret}}}
	for _, values := range []map[string]string{{"OPCODE_LIVE_PROVIDERS": secret}, {"OPCODE_TEST_OPENAI_MAX_TOKENS": secret}, {"OPCODE_TEST_OPENAI_CAN_REASON": secret}, {"OPCODE_TEST_OPENAI_REASONING_EFFORT": secret}, {"OPCODE_TEST_OPENAI_THINKING_MODE": secret}, {"OPCODE_LIVE_TIMEOUT_SECONDS": secret}} {
		_, _, err := resolveLiveCases(cfg, environment(values))
		require.Error(t, err)
		require.NotContains(t, err.Error(), secret)
	}
	for _, raw := range []string{`{"providers":{"` + secret + `":{}}}`, `{"` + secret + `":123}`, `{"providers":{"openai":{"apiKey":"` + secret + `","enabled":"wrong-type"}}}`, `{"providers":{}} {"secret":"` + secret + `"}`} {
		path := filepath.Join(t.TempDir(), "integration.json")
		require.NoError(t, os.WriteFile(path, []byte(raw), 0600))
		c, err := readLiveConfig(path, true)
		if err == nil {
			_, _, err = resolveLiveCases(c, environment(nil))
		}
		require.Error(t, err)
		require.NotContains(t, err.Error(), secret)
	}
}
func TestPrivateConfigFileAndMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "integration.json")
	_, err := readLiveConfig(path, false)
	require.NoError(t, err)
	_, err = readLiveConfig(path, true)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(path, []byte(`{"providers":{"openai":{"enabled":true,"model":"test","apiKey":"secret"}}}`), 0600))
	cfg, err := readLiveConfig(path, true)
	require.NoError(t, err)
	require.Equal(t, "secret", cfg.Providers["openai"].APIKey)
	if runtime.GOOS != "windows" {
		require.NoError(t, os.Chmod(path, 0644))
		_, err = readLiveConfig(path, true)
		require.ErrorContains(t, err, "chmod 600")
		require.NoError(t, os.Chmod(path, 0600))
		link := filepath.Join(t.TempDir(), "link.json")
		require.NoError(t, os.Symlink(path, link))
		_, err = readLiveConfig(link, true)
		require.ErrorContains(t, err, "symlink")
	}
}
func TestLiveConfigPaths(t *testing.T) {
	path, required, err := liveConfigPath(environment(map[string]string{"OPCODE_LIVE_CONFIG": "custom.json"}))
	require.NoError(t, err)
	require.True(t, required)
	require.Equal(t, "custom.json", path)
	path, required, err = liveConfigPath(environment(map[string]string{"XDG_CONFIG_HOME": "/synthetic/config"}))
	require.NoError(t, err)
	require.False(t, required)
	require.Equal(t, filepath.FromSlash("/synthetic/config/opcode/integration.json"), path)
}
func TestExampleStartsDisabled(t *testing.T) {
	raw, err := os.ReadFile("../../../../examples/integration.example.json")
	require.NoError(t, err)
	var cfg liveConfig
	require.NoError(t, json.Unmarshal(raw, &cfg))
	require.Len(t, cfg.Providers, len(providerSpecs))
	_, _, err = resolveLiveCases(cfg, environment(nil))
	require.ErrorContains(t, err, "no live provider selected")
}
func TestSafeLiveErrors(t *testing.T) {
	secret := "DO_NOT_PRINT_SECRET"
	for _, err := range []error{errors.New(secret), fmt.Errorf("%s: %w", secret, &provider.HTTPError{StatusCode: 401, RequestID: secret, Body: secret}), fmt.Errorf("%s: %w", secret, context.DeadlineExceeded)} {
		require.NotContains(t, safeLiveError(err), secret)
	}
	require.Contains(t, safeLiveError(&provider.HTTPError{StatusCode: 429}), "HTTP 429")
}
func TestConfigInitializerIsPrivateAndNeverOverwrites(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX convenience script")
	}
	script, err := filepath.Abs("../../../../scripts/test-providers.sh")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "directory with spaces", "integration.json")
	output, err := exec.Command("sh", script, "--init", "--config", path).CombinedOutput()
	require.NoError(t, err, string(output))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	require.NoError(t, os.WriteFile(path, []byte("keep-existing-config"), 0600))
	output, err = exec.Command("sh", script, "--init", "--config", path).CombinedOutput()
	require.Error(t, err)
	require.NotContains(t, string(output), "keep-existing-config")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "keep-existing-config", string(after))
}

type simulatedProvider struct {
	t              *testing.T
	sends, streams int
	state          *message.ProviderState
}

func (p *simulatedProvider) Model() models.Model { return models.Model{} }
func (p *simulatedProvider) SendMessages(_ context.Context, _ []message.Message, definitions []tools.BaseTool) (*provider.ProviderResponse, error) {
	p.sends++
	if len(definitions) == 0 {
		return &provider.ProviderResponse{Content: responseMarker, FinishReason: message.FinishReasonEndTurn}, nil
	}
	require.Equal(p.t, probeName, definitions[0].Info().Name)
	return &provider.ProviderResponse{ToolCalls: []message.ToolCall{{ID: "synthetic-call", Name: probeName, Input: `{"token":"opcode_integration"}`, Finished: true}}, FinishReason: message.FinishReasonToolUse, ProviderState: p.state}, nil
}
func (p *simulatedProvider) StreamResponse(_ context.Context, history []message.Message, definitions []tools.BaseTool) <-chan provider.ProviderEvent {
	p.streams++
	text := responseMarker
	if len(definitions) > 0 {
		require.Len(p.t, history, 3)
		require.Equal(p.t, p.state, history[1].FinishPart().ProviderState)
		require.Equal(p.t, "synthetic-call", history[2].ToolResults()[0].ToolCallID)
		require.Equal(p.t, toolMarker, history[2].ToolResults()[0].Content)
		text = toolMarker
	}
	events := make(chan provider.ProviderEvent, 2)
	events <- provider.ProviderEvent{Type: provider.EventContentDelta, Content: text}
	events <- provider.ProviderEvent{Type: provider.EventComplete, Response: &provider.ProviderResponse{Content: text, FinishReason: message.FinishReasonEndTurn}}
	close(events)
	return events
}
func TestIntegrationChecksUseOnlyHarmlessProbeAndPreserveState(t *testing.T) {
	client := &simulatedProvider{t: t, state: &message.ProviderState{API: "synthetic", Data: json.RawMessage(`{"opaque":"signed-context"}`)}}
	runLiveChecks(t, context.Background(), client, true)
	require.Equal(t, 2, client.sends)
	require.Equal(t, 2, client.streams)
	_, err := (probeTool{}).Run(context.Background(), tools.ToolCall{Input: `{"token":"wrong"}`})
	require.Error(t, err)
}

func TestLegacyIntegrationEnvironmentReferencesRemainUsable(t *testing.T) {
	lookup := environment(map[string]string{"OPENCODE_LIVE_CONFIG": "old-private.json"})
	path, required, err := liveConfigPath(lookup)
	require.NoError(t, err)
	require.True(t, required)
	require.Equal(t, "old-private.json", path)
	cfg := liveConfig{Providers: map[string]liveProvider{"openai": {Enabled: true, Model: "configured"}}}
	cases, _, err := resolveLiveCases(cfg, environment(map[string]string{"OPENCODE_LIVE_PROVIDERS": "openai", "OPENCODE_TEST_OPENAI_MODEL": "legacy-model", "OPENAI_API_KEY": "fixture-key"}))
	require.NoError(t, err)
	require.Len(t, cases, 1)
	require.Equal(t, "legacy-model", cases[0].config.Model)
	cases, _, err = resolveLiveCases(cfg, environment(map[string]string{"OPENCODE_TEST_OPENAI_MODEL": "legacy-model", "OPCODE_TEST_OPENAI_MODEL": "new-model", "OPENAI_API_KEY": "fixture-key"}))
	require.NoError(t, err)
	require.Equal(t, "new-model", cases[0].config.Model)
}
