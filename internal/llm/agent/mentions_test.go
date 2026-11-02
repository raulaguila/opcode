package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMentionsBecomePathsFromTheWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), nil, 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "cmd"), 0o700))
	require.Equal(t,
		"./main.go o que faz? veja ./cmd/, @nada.go e @../fora",
		resolveMentions("@main.go o que faz? veja @cmd, @nada.go e @../fora", root))
	require.Equal(t, "email a@main.go", resolveMentions("email a@main.go", root))
}
