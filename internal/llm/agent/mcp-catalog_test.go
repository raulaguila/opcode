package agent

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func catalogConfiguration(t *testing.T) {
	t.Helper()
	_, _, _ = steeringAgent(t)
	cfg := config.Get()
	previous := cfg.MCPServers
	cfg.MCPServers = map[string]config.MCPServer{"docs": {Type: config.MCPHTTP, URL: "http://example.invalid/mcp", HeadersEnv: map[string]string{"Authorization": "MISSING_MCP_STARTUP_VARIABLE"}}}
	t.Cleanup(func() { cfg.MCPServers = previous })
}
func catalogContext() context.Context {
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "catalog-session")
	return context.WithValue(ctx, tools.MessageIDContextKey, "catalog-message")
}
func TestMCPStartupAndServerListHaveNoTransportOrCredentialResolution(t *testing.T) {
	catalogConfiguration(t)
	permissions := &resourcePermissionsStub{}
	built := GetMcpTools(context.Background(), permissions)
	require.Len(t, built, 2)
	require.Equal(t, "list_mcp_tools", built[0].Info().Name)
	require.Equal(t, "call_mcp_tool", built[1].Info().Name)
	require.Equal(t, 0, permissions.requests)
	catalog := built[0].(*mcpCatalogTool)
	catalog.factory = func(config.MCPServer) (MCPClient, error) {
		t.Fatal("listing server names must not initialize a transport")
		return nil, nil
	}
	response, err := catalog.Run(catalogContext(), tools.ToolCall{Input: `{}`})
	require.NoError(t, err)
	require.Contains(t, response.Content, "docs")
	require.Equal(t, 0, permissions.requests)
	require.True(t, tools.IsOpaqueTool(built[1]))
}
func TestMCPLazyCatalogDenialCancelAndApprovedPage(t *testing.T) {
	catalogConfiguration(t)
	permissions := &resourcePermissionsStub{}
	calls := 0
	client := &resourceMCPStub{}
	tool := &mcpCatalogTool{permissions: permissions, factory: func(config.MCPServer) (MCPClient, error) { calls++; return client, nil }}
	_, err := tool.Run(catalogContext(), tools.ToolCall{Input: `{"server":"docs"}`})
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.Zero(t, calls)
	permissions.allowed = true
	response, err := tool.Run(catalogContext(), tools.ToolCall{Input: `{"server":"docs"}`})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Contains(t, response.Content, "read_document")
	require.Contains(t, response.Content, "untrusted_catalog_data")
	require.Equal(t, 1, calls)
	require.True(t, client.started)
	require.True(t, client.closed)
	ctx, cancel := context.WithCancel(catalogContext())
	cancel()
	_, err = tool.Run(ctx, tools.ToolCall{Input: `{"server":"docs"}`})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}
func TestMCPLazyCallUsesExecutionPermissionAndPreservesServerError(t *testing.T) {
	catalogConfiguration(t)
	permissions := &resourcePermissionsStub{}
	calls := 0
	client := &resultMCPClient{result: &mcp.CallToolResult{Content: []mcp.Content{mcp.TextContent{Type: "text", Text: "server validation failed"}}, IsError: true}}
	tool := &mcpCallTool{permissions: permissions, factory: func(config.MCPServer) (MCPClient, error) { calls++; return client, nil }}
	input := tools.ToolCall{Input: `{"server":"docs","tool":"read_document","arguments":{"id":"guide"}}`}
	_, err := tool.Run(catalogContext(), input)
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.Zero(t, calls)
	permissions.allowed = true
	response, err := tool.Run(catalogContext(), input)
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Equal(t, "server validation failed", response.Content)
	require.Equal(t, 1, calls)
	require.True(t, client.closed)
}
