package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluateOfflineCommandProducesPrivateNonQualityReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	command := newEvaluateCommand()
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetArgs([]string{"--output", path})
	require.NoError(t, command.Execute())
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"mode": "offline_fixture_validation"`)
	require.Contains(t, string(raw), `"passed": null`)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.EqualValues(t, 0600, info.Mode().Perm())
}

func TestEvaluateExperimentCommandPlansWithoutLeakingProviderConfig(t *testing.T) {
	dir := t.TempDir()
	suite := []byte(`{"version":1,"cases":[{"id":"one","mode":"ask","files":{"README.md":"fixture"},"prompts":["OK"],"criteria":[{"kind":"response_contains","value":"OK"}]}]}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "suite.json"), suite, 0600))
	provider := []byte(`{"providers":{"local":{"apiKey":"private-fixture-key"}},"models":{"local.m":{"provider":"local","api_model":"m","api":"ollama"}},"agents":{"coder":{"model":"local.m"}}}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "provider.json"), provider, 0600))
	binary, err := os.Executable()
	require.NoError(t, err)
	adapter, _ := json.Marshal(map[string]any{"name": "codaro", "command": binary})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "adapter.json"), adapter, 0600))
	sum := sha256.Sum256(suite)
	manifest, _ := json.Marshal(map[string]any{"version": 1, "name": "plan-test", "suite": "suite.json", "suite_sha256": hex.EncodeToString(sum[:]), "provider_config": "provider.json", "conditions": map[string]any{"api_model": "m", "protocol": "ollama", "context_window": 16384, "output_reserve": 2048, "temperature": 0}, "repeats": 2, "cases": []string{"one"}, "applications": []any{map[string]any{"name": "opcode"}, map[string]any{"name": "codaro", "adapter": "adapter.json"}}})
	path := filepath.Join(dir, "manifest.json")
	require.NoError(t, os.WriteFile(path, manifest, 0600))
	command := newExperimentCommand()
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetArgs([]string{path, "--output-dir", filepath.Join(dir, "not-created")})
	require.NoError(t, command.Execute())
	require.Contains(t, out.String(), `"mode": "experiment_plan"`)
	require.NotContains(t, out.String(), "private-fixture-key")
	require.NotContains(t, out.String(), "provider.json")
	_, err = os.Stat(filepath.Join(dir, "not-created"))
	require.True(t, os.IsNotExist(err))
}
