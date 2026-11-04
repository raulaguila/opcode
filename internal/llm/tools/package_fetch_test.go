package tools

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/stretchr/testify/require"
)

func TestPackageFetchAcceptsOnlyDeclaredDependencies(t *testing.T) {
	for _, command := range []string{"go mod download", "go  mod   tidy", "npm ci", "npm install", "  cargo fetch "} {
		require.True(t, packageFetchCommand(command), command)
	}
	for _, command := range []string{"", "curl x", "npm install evil", "npm ci; rm a", "npm ci && curl x", "go mod tidy\nrm a", "pip install evil", "go get evil.example/x", "go mod tidy | sh"} {
		require.False(t, packageFetchCommand(command), command)
	}
}

func TestFullAccessIsTheOnlyWayOutOfTheSandbox(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil || shell.AlreadySandboxed() {
		t.Skip("needs bubblewrap outside a sandbox")
	}
	_, err := config.Load(t.TempDir(), false)
	require.NoError(t, err)
	config.Get().Security.Sandbox = "bubblewrap"
	cwd := config.WorkingDirectory()
	confined, err := shell.Command(context.Background(), cwd, "true")
	require.NoError(t, err)
	require.Equal(t, "bwrap", filepath.Base(confined.Path))
	free, err := shell.Command(shell.WithoutSandbox(context.Background()), cwd, "true")
	require.NoError(t, err)
	require.NotEqual(t, "bwrap", filepath.Base(free.Path))
}
