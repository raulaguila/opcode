package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

type fullAccessPermissions struct{ permission.Service }

func (fullAccessPermissions) ApprovalPolicy(string) permission.ApprovalPolicy {
	return permission.ApprovalFull
}
func (fullAccessPermissions) SetApprovalPolicy(string, permission.ApprovalPolicy, string) error {
	return nil
}

func TestAgentIsToldWhenCommandsCannotRun(t *testing.T) {
	if cfg := config.Get(); cfg != nil {
		old := cfg.Security.Sandbox
		cfg.Security.Sandbox = "bubblewrap"
		t.Cleanup(func() { cfg.Security.Sandbox = old })
	}
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "bwrap"), []byte("#!/bin/sh\necho 'bwrap: setting up uid map: Permission denied' >&2\nexit 1\n"), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	a := &agent{}
	require.True(t, a.commandsRefused("s"))
	constraints := a.executionConstraints(&generationRun{}, "s")
	require.Equal(t, "unavailable", constraints["sandbox"])
	require.Equal(t, false, constraints["bash_allowed"])
	require.Equal(t, false, constraints["run_checks_allowed"])

	require.Contains(t, (&auditReport{noSandbox: true}).render(true, ""), "/sandbox", "an audit says what did not run")

	a.permissions = fullAccessPermissions{}
	require.False(t, a.commandsRefused("s"), "full access runs commands without the sandbox")
}
