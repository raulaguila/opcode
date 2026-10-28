package cmd

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func configProcess(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	cmd := exec.Command(os.Args[0], "-test.run=^TestConfigSubprocess$")
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "OPCODE_CONFIG_TEST_ARGS="+string(raw))
	out, err := cmd.CombinedOutput()
	return string(out), err
}
func TestConfigSubprocess(t *testing.T) {
	raw := os.Getenv("OPCODE_CONFIG_TEST_ARGS")
	if raw == "" {
		return
	}
	var args []string
	if json.Unmarshal([]byte(raw), &args) != nil {
		os.Exit(3)
	}
	cmd := newConfigCommand()
	cmd.SetArgs(args)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	if cmd.Execute() != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
func TestConfigCommandsDoNotInitializeStateOrEchoSecretsAndMigrateInPlace(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	out, err := configProcess(t, home, "path", "--cwd", workspace)
	require.NoError(t, err)
	require.Contains(t, out, filepath.Join(home, ".opcode", "config.json"))
	_, err = os.Stat(filepath.Join(home, ".opcode"))
	require.True(t, os.IsNotExist(err))
	path := filepath.Join(home, ".opcode.json")
	original := []byte(`{"providers":{"local":{"apiKey":"private-cli-key","baseURL":"http://127.0.0.1:1/v1"}},"future":9007199254740993}`)
	require.NoError(t, os.WriteFile(path, original, 0600))
	out, err = configProcess(t, home, "validate", "--cwd", workspace)
	require.NoError(t, err)
	require.Contains(t, out, "Valid:")
	require.NotContains(t, out, "private-cli-key")
	out, err = configProcess(t, home, "show", "--redacted", "--cwd", workspace)
	require.NoError(t, err)
	require.NotContains(t, out, "private-cli-key")
	require.NotContains(t, out, "127.0.0.1:1")
	require.NotContains(t, out, "9007199254740993")
	out, err = configProcess(t, home, "migrate")
	require.NoError(t, err)
	require.Contains(t, out, "Private backup:")
	require.NotContains(t, out, "private-cli-key")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "9007199254740993")
	require.Contains(t, string(data), `"schemaVersion": 1`)
	backups, err := filepath.Glob(filepath.Join(home, ".opcode", "config-backups", "*"))
	require.NoError(t, err)
	require.Len(t, backups, 1)
	data, err = os.ReadFile(backups[0])
	require.NoError(t, err)
	require.Equal(t, original, data)
	out, err = configProcess(t, home, "migrate")
	require.NoError(t, err)
	require.Contains(t, out, "already current")
	_, err = os.Stat(filepath.Join(home, ".opcode", "config.json"))
	require.True(t, os.IsNotExist(err), "legacy location remains authoritative")
}
func TestConfigValidationReportsFieldOrLocationWithoutValues(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"runtime":{"maxSteps":"private-bad-type"}}`), 0600))
	out, err := configProcess(t, home, "validate", "--file", path)
	require.Error(t, err)
	require.Contains(t, out, "runtime.maxSteps")
	require.NotContains(t, out, "private-bad-type")
	require.NoError(t, os.WriteFile(path, []byte("{\n\"runtime\": private-bad-syntax\n}"), 0600))
	out, err = configProcess(t, home, "validate", "--file", path)
	require.Error(t, err)
	require.Contains(t, out, "line 2")
	require.NotContains(t, out, "private-bad-syntax")
}
