package cmd

import (
	"bytes"
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
