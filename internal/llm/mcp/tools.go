package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/llm/tools/shell"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/version"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type MCPClient interface {
	Start(context.Context) error
	Initialize(
		ctx context.Context,
		request mcp.InitializeRequest,
	) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

func runTool(ctx context.Context, c MCPClient, toolName string, input string) (tools.ToolResponse, error) {
	defer c.Close()
	if err := ctx.Err(); err != nil {
		return tools.ToolResponse{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		return tools.NewTextErrorResponse(err.Error()), nil
	}
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "Opcode",
		Version: version.Version,
	}

	_, err := c.Initialize(ctx, initRequest)
	if err != nil {
		return tools.NewTextErrorResponse(err.Error()), nil
	}

	toolRequest := mcp.CallToolRequest{}
	toolRequest.Params.Name = toolName
	var args map[string]any
	if err = json.Unmarshal([]byte(input), &args); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}
	toolRequest.Params.Arguments = args
	result, err := c.CallTool(ctx, toolRequest)
	if err != nil {
		return tools.NewTextErrorResponse(err.Error()), nil
	}

	if result == nil || len(result.Content) > 100 {
		return tools.NewTextErrorResponse("MCP response exceeds 100 content blocks"), nil
	}
	var output []string
	totalBytes := 0
	for _, v := range result.Content {
		if v, ok := v.(mcp.TextContent); ok {
			output = append(output, v.Text)
		} else {
			output = append(output, fmt.Sprintf("%v", v))
		}
		totalBytes += len(output[len(output)-1])
		if totalBytes > 1<<20 {
			return tools.NewTextErrorResponse("MCP response exceeds 1 MiB"), nil
		}
	}

	response := tools.NewTextResponse(strings.Join(output, "\n"))
	response.IsError = result.IsError
	return response, nil
}

func newMCPClient(server config.MCPServer) (MCPClient, error) {
	var err error
	server, err = config.ResolveMCPServer(server)
	if err != nil {
		return nil, err
	}
	if server.OAuth != nil {
		return newOAuthMCPClient(server)
	}
	switch server.Type {
	case "", config.MCPStdio:
		return client.NewClient(transport.NewStdioWithOptions(server.Command, server.Env, server.Args, transport.WithCommandFunc(func(ctx context.Context, command string, env, args []string) (*exec.Cmd, error) {
			quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
			parts := []string{"exec", quote(command)}
			for _, arg := range args {
				parts = append(parts, quote(arg))
			}
			cmd, err := shell.Command(ctx, config.WorkingDirectory(), strings.Join(parts, " "))
			if err == nil {
				cmd.Env = append(cmd.Env, env...)
			}
			return cmd, err
		}))), nil
	case config.MCPSse:
		return client.NewSSEMCPClient(server.URL, client.WithHeaders(server.Headers))
	case config.MCPHTTP:
		return client.NewStreamableHttpClient(server.URL, transport.WithHTTPHeaders(server.Headers))
	default:
		return nil, fmt.Errorf("invalid mcp type: %s", server.Type)
	}
}

// GetMcpTools builds local dispatchers only. Configuring a server never starts
// a process or connects to a remote endpoint during application initialization.
func GetMcpTools(_ context.Context, permissions permission.Service) []tools.BaseTool {
	return []tools.BaseTool{&mcpCatalogTool{permissions: permissions, factory: newMCPClient}, &mcpCallTool{permissions: permissions, factory: newMCPClient}}
}

func newOAuthMCPClient(server config.MCPServer) (MCPClient, error) {

	oauth, store, err := oauthClientConfig(server, "")
	if err != nil {
		return nil, err
	}
	if _, err := store.GetToken(context.Background()); err != nil {
		return nil, fmt.Errorf("MCP OAuth login required: use opcode mcp login <server>")
	}
	if server.Type == config.MCPHTTP {
		return client.NewOAuthStreamableHttpClient(server.URL, oauth, transport.WithHTTPHeaders(server.Headers), transport.WithHTTPBasicClient(oauth.HTTPClient))
	}
	if server.Type == config.MCPSse {
		return client.NewOAuthSSEClient(server.URL, oauth, client.WithHeaders(server.Headers), client.WithHTTPClient(oauth.HTTPClient))
	}
	return nil, fmt.Errorf("OAuth requires HTTP or SSE transport")

}
