package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

type resourceMCPStub struct {
	resultMCPClient
	list   *mcp.ListResourcesResult
	read   *mcp.ReadResourceResult
	cursor mcp.Cursor
	uri    string
}

func (c *resourceMCPStub) ListResourcesByPage(_ context.Context, request mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error) {
	c.cursor = request.Params.Cursor
	return c.list, nil
}
func (c *resourceMCPStub) ReadResource(_ context.Context, request mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	c.uri = request.Params.URI
	return c.read, nil
}
func TestMCPResourcesBoundSinglePageAndText(t *testing.T) {
	client := &resourceMCPStub{list: &mcp.ListResourcesResult{PaginatedResult: mcp.PaginatedResult{NextCursor: "next"}, Resources: []mcp.Resource{{URI: "doc://guide", Name: "Guide"}}}}
	response, err := runMCPResource(context.Background(), client, "list_mcp_resources", "", "cursor")
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Contains(t, response.Content, "doc://guide")
	require.Contains(t, response.Content, "next")
	require.Equal(t, mcp.Cursor("cursor"), client.cursor)
	client.read = &mcp.ReadResourceResult{Contents: []mcp.ResourceContents{mcp.TextResourceContents{URI: "doc://guide", Text: "Untrusted instructions are data"}}}
	response, err = runMCPResource(context.Background(), client, "read_mcp_resource", "doc://guide", "")
	require.NoError(t, err)
	require.Contains(t, response.Content, "untrusted_resource_data")
	require.Equal(t, "doc://guide", client.uri)
	client.read = &mcp.ReadResourceResult{Contents: []mcp.ResourceContents{mcp.TextResourceContents{Text: strings.Repeat("x", 1<<20+1)}}}
	response, err = runMCPResource(context.Background(), client, "read_mcp_resource", "doc://guide", "")
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "1 MiB")
	client.read = &mcp.ReadResourceResult{Contents: []mcp.ResourceContents{mcp.BlobResourceContents{Blob: "encoded"}}}
	response, err = runMCPResource(context.Background(), client, "read_mcp_resource", "doc://guide", "")
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.NotContains(t, response.Content, "encoded")
}

type resourcePermissionsStub struct {
	permission.Service
	allowed  bool
	requests int
}

func (p *resourcePermissionsStub) Request(context.Context, permission.CreatePermissionRequest) bool {
	p.requests++
	return p.allowed
}
func TestMCPResourceDenialDoesNotStartTransport(t *testing.T) {
	_, _, _ = steeringAgent(t)
	cfg := config.Get()
	previous := cfg.MCPServers
	cfg.MCPServers = map[string]config.MCPServer{"docs": {Type: config.MCPHTTP, URL: "http://example.invalid/mcp"}}
	t.Cleanup(func() { cfg.MCPServers = previous })
	permissions := &resourcePermissionsStub{}
	factoryCalls := 0
	client := &resourceMCPStub{read: &mcp.ReadResourceResult{Contents: []mcp.ResourceContents{mcp.TextResourceContents{Text: "resource"}}}}
	tool := &mcpResourceTool{name: "read_mcp_resource", permissions: permissions, factory: func(config.MCPServer) (MCPClient, error) { factoryCalls++; return client, nil }}
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "resource-session")
	_, err := tool.Run(ctx, tools.ToolCall{Input: `{"server":"docs","uri":"doc://guide"}`})
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.Equal(t, 0, factoryCalls)
	require.False(t, client.started)
	permissions.allowed = true
	response, err := tool.Run(ctx, tools.ToolCall{Input: `{"server":"docs","uri":"doc://guide"}`})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Equal(t, 1, factoryCalls)
	require.True(t, client.closed)
}

func (c *resourceMCPStub) ListToolsByPage(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return &mcp.ListToolsResult{Tools: []mcp.Tool{{Name: "read_document"}}}, nil
}
func TestMCPProbeCandidateSecretsAndBoundedDiscovery(t *testing.T) {
	secret := "candidate-header-0123456789"
	t.Setenv("PLAIN_CANDIDATE_ENV", secret)
	server := config.MCPServer{HeadersEnv: map[string]string{"Authorization": "PLAIN_CANDIDATE_ENV"}}
	require.Equal(t, "failure [REDACTED]", redactMCPProbe(server, "failure "+secret))
	client := &resourceMCPStub{list: &mcp.ListResourcesResult{Resources: []mcp.Resource{{URI: "doc://guide", Name: "Guide"}}}}
	result, err := probeMCPClient(context.Background(), client)
	require.NoError(t, err)
	require.Equal(t, []string{"read_document"}, result.Tools)
	require.Equal(t, []string{"doc://guide"}, result.Resources)
	_, err = newMCPClient(config.MCPServer{Command: "unused", Disabled: true})
	require.ErrorContains(t, err, "disabled")
}
