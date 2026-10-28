package agent

import (
	"context"
	"encoding/json"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
)

const AgentToolName = "agent"

type AgentParams struct {
	Prompt string `json:"prompt"`
}

// agent remains a compatibility alias with the same shared budgets and isolation
// as explore_code. It cannot start an unbounded independent worker.
type agentTool struct{ explorer tools.BaseTool }

func (b *agentTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: AgentToolName, Description: "Compatibility alias for bounded read-only explore_code. Investigate local code in separate context; no commands, writes, MCP, hooks or nested agents. Shares the parent step, duration and result budgets.", Parameters: map[string]any{"prompt": map[string]any{"type": "string", "maxLength": 4000}}, Required: []string{"prompt"}}
}
func (b *agentTool) Run(ctx context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	var input AgentParams
	if len(call.Input) > 8192 || json.Unmarshal([]byte(call.Input), &input) != nil {
		return tools.NewTextErrorResponse("invalid exploration prompt"), nil
	}
	raw, _ := json.Marshal(map[string]string{"question": input.Prompt})
	call.Input = string(raw)
	return b.explorer.Run(ctx, call)
}
func NewAgentTool(_ session.Service, _ message.Service, clients map[string]*lsp.Client, services ...permission.Service) tools.BaseTool {
	var permissions permission.Service
	if len(services) > 0 {
		permissions = services[0]
	}
	return &agentTool{explorer: newExploreTool(clients, permissions)}
}
