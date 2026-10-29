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

func TestExpandedLanguageServerPresetsRequireProjectMarkers(t *testing.T) {
	root := t.TempDir()
	_, err := config.Load(root, false)
	require.NoError(t, err)
	cfg := config.Get()
	old := cfg.WorkingDir
	cfg.WorkingDir = root
	t.Cleanup(func() { cfg.WorkingDir = old })
	for _, test := range []struct{ command, file, marker string }{
		{"ruby-lsp", "ledger.rb", "Gemfile"}, {"intelephense", "ledger.php", "composer.json"},
		{"kotlin-language-server", "ledger.kt", "build.gradle.kts"}, {"sourcekit-lsp", "ledger.swift", "Package.swift"},
		{"bash-language-server", "ledger.sh", ".shellcheckrc"}, {"lua-language-server", "ledger.lua", ".luarc.json"},
		{"dart", "ledger.dart", "pubspec.yaml"}, {"vue-language-server", "Ledger.vue", "package.json"},
		{"svelteserver", "Ledger.svelte", "svelte.config.js"}, {"terraform-ls", "main.tf", "main.tf"},
		{"taplo", "ledger.toml", "taplo.toml"},
	} {
		t.Run(test.command, func(t *testing.T) {
			entry := config.LSPConfig{Command: test.command}
			file := filepath.Join(root, test.file)
			require.False(t, ServerMatches(entry, file))
			require.NoError(t, os.WriteFile(filepath.Join(root, test.marker), []byte("fixture"), 0600))
			require.True(t, ServerMatches(entry, file))
			require.False(t, ServerMatches(entry, filepath.Join(root, "other.unknown")))
			require.NoError(t, os.Remove(filepath.Join(root, test.marker)))
		})
	}
}
