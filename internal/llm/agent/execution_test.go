package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestTestRequestCanDiscoverAndExecuteWithoutConfiguredChecks(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Execute)
	config.Get().Checks = nil
	config.Get().Security.Sandbox = "none"
	require.NoError(t, os.WriteFile(filepath.Join(root, "Makefile"), []byte("test:\n\tsh test.sh\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "test.sh"), []byte("test 2 -eq 2 && printf 'fixture test passed'\n"), 0600))
	permissions := permission.NewPermissionService()
	permissions.AutoApproveSession("s")
	manager := tools.NewProcessManager()
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	a.tools = []tools.BaseTool{tools.NewChecksTool(permissions, manager), tools.NewBashTool(permissions, manager)}
	a.tools = append(a.tools, tools.NewValidationTools()...)
	for i, tool := range a.tools {
		a.tools[i] = tools.SecureTool(tool, permissions)
	}
	done, err := a.Run(context.Background(), "s", "Rode os testes")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	require.Subset(t, toolNames(first), []string{"run_checks", "discover_checks", "bash"})
	replyTools(first, toolCall("run_checks", `{}`))
	discover := nextGeneration(t, p)
	require.Contains(t, discover.history[len(discover.history)-1].ToolResults()[0].Content, "no checks configured")
	replyTools(discover, toolCall("discover_checks", `{}`))
	execute := nextGeneration(t, p)
	require.Contains(t, execute.history[len(execute.history)-1].ToolResults()[0].Content, "make test")
	replyTools(execute, toolCall("bash", `{"command":"sh test.sh"}`))
	final := nextGeneration(t, p)
	result := final.history[len(final.history)-1].ToolResults()[0]
	require.False(t, result.IsError)
	require.Contains(t, result.Content, "fixture test passed")
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Empty(t, task.Validation, "bash output must not forge a configured check receipt")
	final.reply <- provider.ProviderResponse{Content: "Comando concluído.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

func TestTestRequestRespectsModesAndVerifyContract(t *testing.T) {
	for _, mode := range []workflow.Mode{workflow.Ask, workflow.Plan, workflow.Execute} {
		t.Run(string(mode), func(t *testing.T) {
			a, _, _, _ := runtimeAgent(t, mode)
			a.tools = []tools.BaseTool{&countedTool{name: "bash"}, &countedTool{name: "run_checks"}}
			for _, contract := range []*workflow.Contract{nil, {Kind: "verify"}} {
				run := &generationRun{mode: mode, contract: contract, selected: map[string]bool{}, available: a.tools}
				prefetchCommandTools(run, "Run the tests")
				canCommand := mode == workflow.Execute && contract == nil
				require.Equal(t, canCommand, executionConstraints(run)["bash_allowed"])
				require.Equal(t, mode == workflow.Execute, executionConstraints(run)["run_checks_allowed"])
				names := toolNames(generationCall{exposed: a.exposedTools(run)})
				require.Equal(t, canCommand, slices.Contains(names, "bash"))
			}
		})
	}
}

func TestTestRequestDoesNotApproveBash(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Execute)
	permissions := permission.NewPermissionService()
	manager := tools.NewProcessManager()
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	a.tools = []tools.BaseTool{tools.SecureTool(tools.NewBashTool(permissions, manager), permissions)}
	done, err := a.Run(context.Background(), "s", "Rode os testes")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	require.Contains(t, toolNames(first), "bash")
	replyTools(first, toolCall("bash", `{"command":"touch must-not-exist"}`))
	require.NoError(t, finishGeneration(t, done).Error)
	_, err = os.Stat(filepath.Join(root, "must-not-exist"))
	require.True(t, os.IsNotExist(err))
}

func TestRequestHistoryIncludesOnlyExecutionPolicyMetadata(t *testing.T) {
	a, _, _, _ := runtimeAgent(t, workflow.Execute)
	config.Get().Security.Sandbox = "none"
	config.Get().Security.AllowNetwork = false
	config.Get().Security.EnvAllow = []string{"OPCODE_FIXTURE_SECRET"}
	t.Setenv("OPCODE_FIXTURE_SECRET", "private-fixture-value")
	a.tools = []tools.BaseTool{&countedTool{name: "bash"}}
	run := &generationRun{mode: workflow.Execute, available: a.tools, selected: map[string]bool{"bash": true}}
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Rode os testes"}}}}
	projected, err := a.requestHistory(context.Background(), "s", history, run)
	require.NoError(t, err)
	text := projected[0].Content().String()
	require.Contains(t, text, `"bash_allowed":true`)
	require.Contains(t, text, `"host_network_without_sandbox":true`)
	require.Contains(t, text, `"default_command_timeout_ms":60000`)
	require.Contains(t, text, `"max_command_timeout_ms":600000`)
	require.NotContains(t, text, "private-fixture-value")
	require.NotContains(t, text, "OPCODE_FIXTURE_SECRET")
	require.Equal(t, "Rode os testes", history[0].Content().String(), "projection must not mutate the saved user request")
	config.Get().Security.Sandbox = "bubblewrap"
	config.Get().Security.AllowNetwork = true
	run.contract = &workflow.Contract{Kind: "verify"}
	raw, err := json.Marshal(executionConstraints(run))
	require.NoError(t, err)
	require.Contains(t, string(raw), `"bash_allowed":false`)
	require.Contains(t, string(raw), `"sandbox_network_enabled":true`)
	require.Contains(t, string(raw), `"host_network_without_sandbox":false`)
}

func TestPlanKeepsFinishedWorkWhenASmallModelRewritesIt(t *testing.T) {
	previous := []workflow.Step{{Title: "Ler", Status: "completed"}, {Title: "Editar", Status: "in_progress"}}
	steps := []workflow.Step{{Title: "Editar", Status: "pending"}, {Title: "Testar", Status: "in_progress"}, {Title: "Publicar", Status: "pending"}}
	require.Equal(t, []workflow.Step{{Title: "Ler", Status: "completed"}, {Title: "Editar", Status: "completed"}, {Title: "Testar", Status: "in_progress"}, {Title: "Publicar", Status: "pending"}}, carryPlanProgress(previous, steps), "steps behind the current one are done, and a dropped finished step stays")

	reopened := carryPlanProgress(previous[:1], []workflow.Step{{Title: "Ler", Status: "in_progress"}})
	require.Equal(t, "in_progress", reopened[0].Status, "a finished step can be reopened on purpose")
	resent := carryPlanProgress(previous[:1], []workflow.Step{{Title: "Ler", Status: "pending"}})
	require.Equal(t, "completed", resent[0].Status)
}
