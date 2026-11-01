package agent

import (
	"context"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/workflow"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/raulaguila/opcode/internal/config"
	mcptools "github.com/raulaguila/opcode/internal/llm/mcp"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/stretchr/testify/require"
)

func TestMCPNameCannotImpersonateNativeReadTool(t *testing.T) {
	tool := tools.SecureTool(mcptools.NewMcpTool("get", mcp.Tool{Name: "project_map"}, nil, config.MCPServer{}), nil)
	require.Equal(t, "get_project_map", tool.Info().Name)
	require.True(t, tools.IsOpaqueTool(tool))
	a, p, _, _ := runtimeAgent(t, workflow.Ask)
	a.tools = []tools.BaseTool{tool}
	done, err := a.Run(context.Background(), "opaque-name", "read code")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	replyTools(first, toolCall("request_tools", `{"names":["get_project_map"]}`))
	next := nextGeneration(t, p)
	require.NotContains(t, toolNames(next), "get_project_map")
	next.reply <- provider.ProviderResponse{Content: "done"}
	require.NoError(t, finishGeneration(t, done).Error)
}
