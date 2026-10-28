package agent

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

type resultMCPClient struct {
	MCPClient
	result  *mcp.CallToolResult
	closed  bool
	started bool
}

func (c *resultMCPClient) Start(context.Context) error { c.started = true; return nil }

func (c *resultMCPClient) Initialize(context.Context, mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	return &mcp.InitializeResult{}, nil
}

func (c *resultMCPClient) CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return c.result, nil
}

func (c *resultMCPClient) Close() error {
	c.closed = true
	return nil
}

func TestMCPToolPreservesContentAndError(t *testing.T) {
	for _, isError := range []bool{false, true} {
		client := &resultMCPClient{result: &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{Type: "text", Text: "first block"},
				mcp.TextContent{Type: "text", Text: "second block"},
			},
			IsError: isError,
		}}
		response, err := runTool(context.Background(), client, "test", `{}`)
		require.NoError(t, err)
		require.Equal(t, "first block\nsecond block", response.Content)
		require.Equal(t, isError, response.IsError)
		require.True(t, client.closed)
		require.True(t, client.started)
	}
}
