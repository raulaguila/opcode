package agent

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/version"
)

type MCPProbe struct {
	Tools                                []string
	Resources                            []string
	NextToolsCursor, NextResourcesCursor string
	ResourceError                        string
}
type mcpPagedTools interface {
	ListToolsByPage(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
}

// ProbeMCPServer tests a candidate configuration only after ordinary execution
// approval. It never saves configuration, grants trust or invokes a remote tool.
func ProbeMCPServer(ctx context.Context, name string, server config.MCPServer, permissions permission.Service) (MCPProbe, error) {
	if err := config.ValidateIntegration(config.Integration{Kind: "mcp", Name: name, MCP: server}); err != nil {
		return MCPProbe{}, err
	}
	session, _ := tools.GetContextValues(ctx)
	if permissions == nil || session == "" || !permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: session, ToolName: "mcp_connection_test", Action: "execute", Path: config.WorkingDirectory(), Description: fmt.Sprintf("Connect to MCP server %s and enumerate one page of tools/resources", name), Params: map[string]string{"name": name, "transport": string(server.Type), "command": redactMCPProbe(server, server.Command), "url": redactMCPProbe(server, server.URL)}}) {
		return MCPProbe{}, permission.ErrorPermissionDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	c, err := newMCPClient(server)
	if err != nil {
		return MCPProbe{}, fmt.Errorf("MCP connection: %s", tools.RedactCredentials(err.Error()))
	}
	defer c.Close()
	result, err := probeMCPClient(ctx, c)
	if err != nil {
		return MCPProbe{}, fmt.Errorf("%s", redactMCPProbe(server, err.Error()))
	}
	for i := range result.Tools {
		result.Tools[i] = redactMCPProbe(server, result.Tools[i])
	}
	for i := range result.Resources {
		result.Resources[i] = redactMCPProbe(server, result.Resources[i])
	}
	result.ResourceError = redactMCPProbe(server, result.ResourceError)
	return result, nil
}
func probeMCPClient(ctx context.Context, c MCPClient) (MCPProbe, error) {
	if err := c.Start(ctx); err != nil {
		return MCPProbe{}, fmt.Errorf("MCP start: %s", tools.RedactCredentials(err.Error()))
	}
	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{Name: "Opcode", Version: version.Version}
	if _, err := c.Initialize(ctx, request); err != nil {
		return MCPProbe{}, fmt.Errorf("MCP initialize: %s", tools.RedactCredentials(err.Error()))
	}
	pager, ok := c.(mcpPagedTools)
	if !ok {
		return MCPProbe{}, fmt.Errorf("MCP transport does not support bounded tool discovery")
	}
	listed, err := pager.ListToolsByPage(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return MCPProbe{}, fmt.Errorf("MCP tools: %s", tools.RedactCredentials(err.Error()))
	}
	if listed == nil || len(listed.Tools) > 1000 {
		return MCPProbe{}, fmt.Errorf("MCP tool page exceeds 1000 entries")
	}
	result := MCPProbe{Tools: []string{}, Resources: []string{}, NextToolsCursor: string(listed.NextCursor)}
	for _, tool := range listed.Tools {
		result.Tools = append(result.Tools, tools.RedactCredentials(tool.Name))
	}
	sort.Strings(result.Tools)
	if reader, ok := c.(mcpResourceClient); ok {
		resources, err := reader.ListResourcesByPage(ctx, mcp.ListResourcesRequest{})
		if err != nil {
			result.ResourceError = tools.RedactCredentials(err.Error())
		} else if resources == nil || len(resources.Resources) > 1000 {
			result.ResourceError = "MCP resource page exceeds 1000 entries"
		} else {
			result.NextResourcesCursor = string(resources.NextCursor)
			for _, resource := range resources.Resources {
				result.Resources = append(result.Resources, tools.RedactCredentials(resource.URI))
			}
			sort.Strings(result.Resources)
		}
	}
	return result, nil
}

func redactMCPProbe(server config.MCPServer, text string) string {
	values := []string{}
	for _, secret := range server.Headers {
		values = append(values, secret)
	}
	for _, entry := range server.Env {
		_, value, _ := strings.Cut(entry, "=")
		values = append(values, value)
	}
	for _, name := range server.HeadersEnv {
		values = append(values, os.Getenv(name))
	}
	for _, name := range server.EnvVars {
		values = append(values, os.Getenv(name))
	}
	for _, value := range values {
		if len(value) >= 8 {
			text = strings.ReplaceAll(text, value, "[REDACTED]")
		}
	}
	return tools.RedactCredentials(text)
}
