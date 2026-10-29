package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLanguageInventorySeparatesParserServerAndChecksWithoutExecuting(t *testing.T) {
	root := productivityWorkspace(t)
	bin := t.TempDir()
	marker := filepath.Join(root, "server-was-started")
	require.NoError(t, os.WriteFile(filepath.Join(bin, "ruby-lsp"), []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700))
	t.Setenv("PATH", bin) // native discovery fallback; the available server is never executed.
	cfg := config.Get()
	oldLSP, oldChecks := cfg.LSP, cfg.Checks
	cfg.LSP = map[string]config.LSPConfig{"ruby": {Command: "ruby-lsp", Disabled: true}, "kotlin": {Command: "missing-kotlin-server", FileTypes: []string{"kt"}}}
	cfg.Checks = map[string]config.CheckConfig{"unit": {Command: "touch " + marker}}
	t.Cleanup(func() { cfg.LSP, cfg.Checks = oldLSP, oldChecks })
	for name, content := range map[string]string{"service.rb": "class Ledger; end", "Ledger.kt": "class Ledger\n", ".env": "not a credential", ".opcodeignore": "ignored.swift\n", "ignored.swift": "class Hidden {}", "unknown.xyz": "opaque"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0600))
	}
	require.NoError(t, os.Mkdir(filepath.Join(root, "node_modules"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "node_modules", "vendor.php"), []byte("<?php"), 0600))
	require.NoError(t, os.Symlink(filepath.Join(root, "service.rb"), filepath.Join(root, "alias.rb")))
	report, err := InspectLanguageSupport(context.Background(), true)
	require.NoError(t, err)
	entries := map[string]LanguageSupportEntry{}
	for _, entry := range report.Languages {
		entries[entry.Language] = entry
	}
	require.True(t, entries["ruby"].LocalParser)
	require.Equal(t, 1, entries["ruby"].ProjectFiles)
	require.Equal(t, []LanguageServerState{{Name: "ruby", ExecutableAvailable: true, Disabled: true}}, entries["ruby"].ConfiguredServers)
	require.Equal(t, []string{"unit"}, entries["ruby"].ConfiguredChecks)
	require.Equal(t, []LanguageServerState{{Name: "kotlin"}}, entries["kotlin"].ConfiguredServers)
	require.False(t, entries["text"].LocalParser)
	require.Empty(t, entries["text"].ConfiguredServers)
	require.Zero(t, entries["swift"].ProjectFiles)
	require.Zero(t, entries["php"].ProjectFiles)
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err), "inventory cannot start a server or a configured check")
}

func TestLanguageInventoryPaginationFiltersAndCancellation(t *testing.T) {
	tool := NewLanguageSupportTool()
	result, err := tool.Run(toolContext(), toolInput(map[string]any{"limit": 2}))
	require.NoError(t, err)
	var page struct {
		Languages []LanguageSupportEntry
		Total     int
		More      bool
		Next      int `json:"next_offset"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Content), &page))
	require.Len(t, page.Languages, 2)
	require.Equal(t, 42, page.Total)
	require.True(t, page.More)
	require.Equal(t, 2, page.Next)
	result, err = tool.Run(toolContext(), toolInput(map[string]any{"language": "terraform"}))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(result.Content), &page))
	require.Len(t, page.Languages, 1)
	require.Equal(t, "hcl", page.Languages[0].Language)
	result, err = tool.Run(toolContext(), toolInput(map[string]any{"offset": -1}))
	require.NoError(t, err)
	require.True(t, result.IsError)
	ctx, cancel := context.WithCancel(toolContext())
	cancel()
	_, err = tool.Run(ctx, ToolCall{Input: "{}"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestExpandedLanguageFilters(t *testing.T) {
	for _, test := range []struct{ path, language string }{{"x.rake", "ruby"}, {"x.kts", "kotlin"}, {"x.tfvars", "hcl"}, {"x.vue", "vue"}, {"x.svelte", "svelte"}, {"x.toml", "toml"}, {"x.yaml", "yaml"}} {
		require.Equal(t, test.language, sourceLanguage(test.path))
		scope := retrievalScope{Language: test.language}
		require.NoError(t, scope.validate())
		require.True(t, scope.matches(test.path))
	}
}

func TestNewLanguageAliasesSelectStructuralProfiles(t *testing.T) {
	for _, f := range []struct{ alias, path, language string }{{"f#", "ledger.fs", "fsharp"}, {"hs", "ledger.hs", "haskell"}, {"ps1", "ledger.ps1", "powershell"}, {"clj", "ledger.cljc", "clojure"}, {"ex", "ledger.exs", "elixir"}, {"erl", "ledger.hrl", "erlang"}, {"ml", "ledger.mli", "ocaml"}} {
		scope := retrievalScope{Language: f.alias}
		require.NoError(t, scope.validate())
		require.Equal(t, f.language, scope.Language)
		require.True(t, scope.matches(f.path))
	}
}
