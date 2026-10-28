package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootStartupSubprocess(t *testing.T) {
	args := os.Getenv("OPCODE_STARTUP_TEST_ARGS")
	if args == "" {
		return
	}
	var flags []string
	if json.Unmarshal([]byte(args), &flags) != nil {
		os.Exit(3)
	}
	rootCmd.SetArgs(flags)
	if rootCmd.Execute() != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func rootStartupProcess(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	process := exec.Command(os.Args[0], "-test.run=^TestRootStartupSubprocess$")
	// Do not inherit real provider credentials, configuration or discovery URLs.
	process.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "OPCODE_STARTUP_TEST_ARGS=" + string(raw)}
	output, err := process.CombinedOutput()
	return string(output), err
}

func TestResetFlagRecoversInvalidGlobalBeforeStartingAndKeepsExampleSeparate(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	active := filepath.Join(home, ".opcode", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(active), 0700))
	original := []byte(`{"private":"do-not-print-reset-secret","broken":`)
	require.NoError(t, os.WriteFile(active, original, 0600))
	output, err := rootStartupProcess(t, home, "--reset", "--cwd", project, "--prompt", "not sent")
	// A fresh configuration has no selected model. Reaching startup validation
	// proves --reset recovered the broken JSON without sending a model request.
	require.Error(t, err)
	require.Contains(t, output, "no model configured")
	require.Contains(t, output, "Global configuration reset: "+active)
	require.Contains(t, output, "Private backup:")
	require.NotContains(t, output, "do-not-print-reset-secret")
	backups, err := filepath.Glob(filepath.Join(home, ".opcode", "config-backups", "*"))
	require.NoError(t, err)
	require.Len(t, backups, 1)
	raw, err := os.ReadFile(backups[0])
	require.NoError(t, err)
	require.Equal(t, original, raw)
	example := filepath.Join(home, "opcode", "config.example.json")
	raw, err = os.ReadFile(example)
	require.NoError(t, err)
	require.True(t, json.Valid(raw))
	require.Contains(t, string(raw), "apiKeyEnv")
	require.NotContains(t, string(raw), "do-not-print-reset-secret")
	activeRaw, err := os.ReadFile(active)
	require.NoError(t, err)
	require.Equal(t, raw, activeRaw)

	// Every normal launch recreates a deleted example without resetting edits.
	updated := []byte(`{"runtime":{"mode":"ask"},"future":"keep-user-settings"}`)
	require.NoError(t, os.WriteFile(active, updated, 0600))
	require.NoError(t, os.Remove(example))
	output, err = rootStartupProcess(t, home, "--cwd", project, "--prompt", "not sent")
	require.Error(t, err)
	require.Contains(t, output, "no model configured")
	require.NotContains(t, output, "Global configuration reset:")
	activeRaw, err = os.ReadFile(active)
	require.NoError(t, err)
	require.Equal(t, updated, activeRaw)
	raw, err = os.ReadFile(example)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "keep-user-settings")
}

func TestVersionAndHelpDoNotResetOrCreateConfiguration(t *testing.T) {
	for _, flag := range []string{"--version", "--help"} {
		t.Run(flag, func(t *testing.T) {
			home := t.TempDir()
			_, err := rootStartupProcess(t, home, "--reset", flag)
			require.NoError(t, err)
			for _, name := range []string{"opcode", ".opcode"} {
				_, err := os.Lstat(filepath.Join(home, name))
				require.True(t, os.IsNotExist(err), "informational flags must remain read-only")
			}
		})
	}
}
