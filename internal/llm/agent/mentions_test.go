package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMentionedFilesResolveFromTheWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), nil, 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "cmd"), 0o700))
	require.Equal(t, []string{"main.go", "cmd"}, mentionedFiles("@main.go o que faz? veja @cmd, @nada.go e @../fora", root))
	require.Empty(t, mentionedFiles("email a@main.go", root))
}
