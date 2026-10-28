package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestMCPEnvironmentReferencesAreCopiedResolvedAndNeverPersisted(t *testing.T) {
	isolateModelConfig(t)
	t.Setenv("TEST_MCP_HEADER", "Bearer header-private-value")
	t.Setenv("TEST_MCP_CHILD", "child-private-value")
	raw := MCPServer{Type: MCPHTTP, URL: "https://server.invalid/mcp", Headers: map[string]string{"authorization": "stale"}, HeadersEnv: map[string]string{"Authorization": "TEST_MCP_HEADER"}, Env: []string{"ACCESS_TOKEN=stale"}, EnvVars: map[string]string{"ACCESS_TOKEN": "TEST_MCP_CHILD"}}
	resolved, err := ResolveMCPServer(raw)
	require.NoError(t, err)
	require.Equal(t, "Bearer header-private-value", resolved.Headers["Authorization"])
	require.NotContains(t, resolved.Headers, "authorization")
	require.Equal(t, []string{"ACCESS_TOKEN=child-private-value"}, resolved.Env)
	require.Equal(t, "stale", raw.Headers["authorization"])
	require.Equal(t, []string{"ACCESS_TOKEN=stale"}, raw.Env)
	path := filepath.Join(t.TempDir(), "global.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"unknown":"keep"}`), 0600))
	viper.SetConfigFile(path)
	cfg = &Config{WorkingDir: t.TempDir()}
	require.NoError(t, SaveIntegration(Integration{Kind: "mcp", Name: "docs", MCP: raw}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(data), "header-private-value")
	require.NotContains(t, string(data), "child-private-value")
	require.Contains(t, string(data), "TEST_MCP_HEADER")
	require.Contains(t, string(data), "unknown")
	require.Equal(t, raw, cfg.MCPServers["docs"])
	raw.HeadersEnv["Authorization"] = "CHANGED_BY_CALLER"
	raw.EnvVars["ACCESS_TOKEN"] = "CHANGED_BY_CALLER"
	require.Equal(t, "TEST_MCP_HEADER", cfg.MCPServers["docs"].HeadersEnv["Authorization"], "saved configuration owns its maps")
	require.Equal(t, "TEST_MCP_CHILD", cfg.MCPServers["docs"].EnvVars["ACCESS_TOKEN"])
}
func TestMCPEnvironmentValidationDoesNotRevealValues(t *testing.T) {
	t.Setenv("TEST_MCP_INVALID", "secret\r\nInjected: yes")
	_, err := ResolveMCPServer(MCPServer{Type: MCPHTTP, URL: "https://server.invalid/mcp", HeadersEnv: map[string]string{"Authorization": "TEST_MCP_INVALID"}})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
	for _, candidate := range []Integration{
		{Kind: "mcp", Name: "bad", MCP: MCPServer{Type: MCPHTTP, URL: "https://user:private@server.invalid/mcp"}},
		{Kind: "mcp", Name: "bad", MCP: MCPServer{Type: MCPHTTP, URL: "https://server.invalid/mcp?token=private"}},
		{Kind: "mcp", Name: "bad", MCP: MCPServer{Command: "server", HeadersEnv: map[string]string{"bad\nheader": "TEST_MCP_INVALID"}}},
		{Kind: "lsp", Name: "bad", LSP: LSPConfig{Command: "server\x00"}},
	} {
		err = ValidateIntegration(candidate)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private")
	}
}
func TestIntegrationProjectSaveAndFailedSavePreserveRuntimeConfiguration(t *testing.T) {
	isolateModelConfig(t)
	root := t.TempDir()
	path := filepath.Join(root, ".opcode.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"lsp":{"go":{"command":"old","future":true}},"future":9007199254740993}`), 0600))
	global := filepath.Join(t.TempDir(), "global.json")
	require.NoError(t, os.WriteFile(global, []byte(`{}`), 0600))
	viper.SetConfigFile(global)
	cfg = &Config{WorkingDir: root, LSP: map[string]LSPConfig{"go": {Command: "old"}}}
	require.NoError(t, SaveIntegration(Integration{Kind: "lsp", Name: "go", LSP: LSPConfig{Command: "gopls", Disabled: true}}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "future")
	require.Contains(t, string(data), "9007199254740993")
	require.Contains(t, string(data), `"disabled": true`)
	globalData, err := os.ReadFile(global)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(globalData))
	cfg.WorkingDir = t.TempDir()
	viper.SetConfigFile(filepath.Join(t.TempDir(), "missing", "settings.json"))
	require.Error(t, SaveIntegration(Integration{Kind: "lsp", Name: "go", LSP: LSPConfig{Command: "other"}}))
	require.Equal(t, "gopls", cfg.LSP["go"].Command)
}
