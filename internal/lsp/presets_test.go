package lsp

import (
	"context"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestLazyServerMatchingNeedsKnownTypeAndRootMarker(t *testing.T) {
	root := t.TempDir()
	_, err := config.Load(root, false)
	require.NoError(t, err)
	file := filepath.Join(root, "service.py")
	require.False(t, ServerMatches(config.LSPConfig{Command: "pyright-langserver"}, file))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname='test'"), 0600))
	require.True(t, ServerMatches(config.LSPConfig{Command: "pyright-langserver"}, file))
	require.False(t, ServerMatches(config.LSPConfig{Command: "gopls"}, file))
	require.False(t, ServerMatches(config.LSPConfig{Command: "bash"}, file))
	clients := map[string]*Client{}
	called := 0
	unregister := RegisterLazyClients(clients, func(context.Context, string) error { called++; return nil })
	require.NoError(t, EnsureClients(context.Background(), clients, file))
	require.Equal(t, 1, called)
	unregister()
	require.NoError(t, EnsureClients(context.Background(), clients, file))
	require.Equal(t, 1, called)
}
