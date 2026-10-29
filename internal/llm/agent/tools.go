package agent

import (
	"context"

	"github.com/raulaguila/opcode/internal/history"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
)

func CoderAgentTools(
	permissions permission.Service,
	sessions session.Service,
	messages message.Service,
	history history.Service,
	lspClients map[string]*lsp.Client,
	managers ...*tools.ProcessManager,
) []tools.BaseTool {
	var manager *tools.ProcessManager
	owned := len(managers) == 0 || managers[0] == nil
	if owned {
		manager = tools.NewProcessManager()
	} else {
		manager = managers[0]
	}
	bash := tools.NewBashTool(permissions, manager)
	if owned {
		bash = &ownedProcessTool{BaseTool: bash, manager: manager}
	}
	ctx := context.Background()
	otherTools := append([]tools.BaseTool{}, GetMcpTools(ctx, permissions)...)
	otherTools = append(otherTools, newMCPResourceTools(permissions)...)
	otherTools = append(otherTools, tools.NewDiagnosticsTool(lspClients))
	otherTools = append(otherTools, tools.NewNavigationTools(lspClients)...)
	otherTools = append(otherTools, tools.NewGitTools()...)
	otherTools = append(otherTools, tools.NewProjectTools(lspClients)...)
	otherTools = append(otherTools, tools.NewArtifactTools()...)
	otherTools = append(otherTools, tools.NewValidationTools()...)
	otherTools = append(otherTools, tools.NewProductivityTools(permissions, manager)...)
	otherTools = append(otherTools, newExploreTool(lspClients, permissions))
	otherTools = append(otherTools, tools.NewAdvancedNavigationTools(lspClients, permissions)...)
	otherTools = append(otherTools, tools.NewChecksTool(permissions, manager))
	otherTools = append(otherTools, tools.NewProcessTools(manager, permissions)...)
	result := append(
		[]tools.BaseTool{
			bash,
			tools.NewEditTool(lspClients, permissions, history),
			tools.NewFetchTool(permissions),
			tools.NewWebSearchTool(permissions),
			tools.NewGlobTool(),
			tools.NewGrepTool(),
			tools.NewLsTool(),
			tools.NewSourcegraphTool(permissions),
			tools.NewViewTool(lspClients),
			tools.NewPatchTool(lspClients, permissions, history),
			tools.NewWriteTool(lspClients, permissions, history),
			NewAgentTool(sessions, messages, lspClients, permissions),
		}, otherTools...,
	)
	for i, tool := range result {
		result[i] = tools.SecureTool(tool, permissions)
	}
	return result
}

func TaskAgentTools(lspClients map[string]*lsp.Client, services ...permission.Service) []tools.BaseTool {
	var permissions permission.Service
	if len(services) > 0 {
		permissions = services[0]
	}
	result := []tools.BaseTool{
		tools.NewGlobTool(),
		tools.NewGrepTool(),
		tools.NewLsTool(),
		tools.NewSourcegraphTool(permissions),
		tools.NewViewTool(lspClients),
	}
	result = append(result, tools.NewNavigationTools(lspClients)...)
	result = append(result, tools.NewGitTools()...)
	result = append(result, tools.NewProjectTools(lspClients)...)
	result = append(result, tools.NewArtifactTools()...)
	result = append(result, tools.NewValidationTools()...)
	result = append(result, tools.NewCodeIndexTool(), tools.NewReadPageTool(), tools.NewLanguageSupportTool())
	for i, tool := range result {
		result[i] = tools.SecureTool(tool, permissions)
	}
	return result
}

// One factory-created tool owns the shared manager. All other tools borrow it;
// a manager supplied by App is closed only by App.Shutdown.
type ownedProcessTool struct {
	tools.BaseTool
	manager *tools.ProcessManager
}

func (t *ownedProcessTool) Close() error                 { return t.manager.Close() }
func (t *ownedProcessTool) CancelSession(session string) { t.manager.CancelSession(session) }
