package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/history"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

type verificationFileHistory struct{ history.Service }

func (*verificationFileHistory) GetByPathAndSession(_ context.Context, path, _ string) (history.File, error) {
	return history.File{Path: path}, nil
}
func (*verificationFileHistory) CreateVersion(_ context.Context, _, path, content string) (history.File, error) {
	return history.File{Path: path, Content: content}, nil
}

func fileEffectAgent(t *testing.T, approve bool) (*agent, *controlledProvider, string) {
	t.Helper()
	a, p, _, root := runtimeAgent(t, workflow.Execute)
	config.Get().Checks = nil
	config.Get().Data.Directory = t.TempDir()
	config.Get().Security.Sandbox = "none"
	artifact, err := os.Create(filepath.Join(root, "opcode"))
	require.NoError(t, err)
	require.NoError(t, artifact.Truncate(17<<20))
	require.NoError(t, artifact.Close())
	permissions := permission.NewPermissionService()
	if approve {
		permissions.AutoApproveSession("s")
	}
	files := &verificationFileHistory{}
	a.tools = []tools.BaseTool{tools.SecureTool(tools.NewWriteTool(nil, permissions, files), permissions), tools.SecureTool(tools.NewEditTool(nil, permissions, files), permissions), tools.SecureTool(tools.NewPatchTool(nil, permissions, files), permissions), tools.SecureTool(tools.NewViewTool(nil), permissions), tools.SecureTool(tools.NewBashTool(permissions), permissions)}
	a.tools = append(a.tools, tools.NewValidationTools()...)
	return a, p, root
}

func TestScopedCreateEditAndPatchFinishBesideLargeArtifactWithoutClaimingTests(t *testing.T) {
	a, p, root := fileEffectAgent(t, true)
	done, err := a.Run(context.Background(), "s", "Crie um arquivo de texto")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	replyTools(call, toolCall("request_tools", `{"names":["write","edit","patch","view","get_validation_report"]}`))
	call = nextGeneration(t, p)
	for _, operation := range []message.ToolCall{
		toolCall("write", `{"file_path":"notes.txt","content":"one\n"}`),
		toolCall("view", `{"file_path":"notes.txt"}`),
		toolCall("edit", `{"file_path":"notes.txt","old_string":"one","new_string":"two"}`),
		toolCall("view", `{"file_path":"notes.txt"}`),
		toolCall("patch", `{"patch_text":"*** Begin Patch\n*** Delete File: notes.txt\n*** Add File: final.txt\n+finished\n*** End Patch"}`),
	} {
		replyTools(call, operation)
		call = nextGeneration(t, p)
		result := call.history[len(call.history)-1].ToolResults()
		require.Len(t, result, 1)
		require.False(t, result[0].IsError, "%s: %s", operation.Name, result[0].Content)
	}
	replyTools(call, toolCall("get_validation_report", `{}`))
	call = nextGeneration(t, p)
	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(call.history[len(call.history)-1].ToolResults()[0].Content), &report))
	require.Equal(t, true, report["affected_files_verified"])
	require.Equal(t, false, report["file_verification_is_test_validation"])
	require.Empty(t, report["workspace_verification_error"])
	replyTools(call, toolCall("finish_task", `{"status":"completed","summary":"Arquivo salvo e conteúdo verificado; testes não configurados"}`))
	call = nextGeneration(t, p)
	require.False(t, call.history[len(call.history)-1].ToolResults()[0].IsError)
	call.reply <- provider.ProviderResponse{Content: "Arquivo salvo e conteúdo verificado. Testes não configurados.", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Equal(t, "completed", result.Outcome)
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Len(t, task.FileChanges, 2)
	require.Empty(t, task.Validation)
	require.Empty(t, task.Digest, "a file receipt must not become a workspace/test fingerprint")
	raw, err := os.ReadFile(filepath.Join(root, "final.txt"))
	require.NoError(t, err)
	require.Equal(t, "finished", string(raw))
	_, err = os.Stat(filepath.Join(root, "notes.txt"))
	require.ErrorIs(t, err, os.ErrNotExist)
	done, err = a.Run(context.Background(), "s", "Obrigado")
	require.NoError(t, err)
	call = nextGeneration(t, p)
	call.reply <- provider.ProviderResponse{Content: "Disponha.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error, "a new conversational turn must not revalidate a previous write as a new implementation")
	task, err = a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Empty(t, task.FileChanges)
	require.False(t, task.Changed)
}

func TestAffectedFileChangedAfterToolCannotComplete(t *testing.T) {
	a, p, root := fileEffectAgent(t, true)
	done, err := a.Run(context.Background(), "s", "Crie um arquivo de texto")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	replyTools(call, toolCall("request_tools", `{"names":["write"]}`))
	call = nextGeneration(t, p)
	replyTools(call, toolCall("write", `{"file_path":"notes.txt","content":"one"}`))
	call = nextGeneration(t, p)
	require.False(t, call.history[len(call.history)-1].ToolResults()[0].IsError, "%s", call.history[len(call.history)-1].ToolResults()[0].Content)
	path := filepath.Join(root, "notes.txt")
	stamp, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("two"), 0600))
	require.NoError(t, os.Chtimes(path, stamp.ModTime(), stamp.ModTime()))
	call.reply <- provider.ProviderResponse{Content: "Arquivo criado.", FinishReason: message.FinishReasonEndTurn}
	require.ErrorContains(t, finishGeneration(t, done).Error, "affected-file verification failed")
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Empty(t, task.Validation)
}

func TestScopedCreationWithoutExplicitFinishDoesNotPoisonTheNextTurn(t *testing.T) {
	a, p, _ := fileEffectAgent(t, true)
	done, err := a.Run(context.Background(), "s", "Crie um arquivo de texto")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	replyTools(call, toolCall("request_tools", `{"names":["write"]}`))
	call = nextGeneration(t, p)
	replyTools(call, toolCall("write", `{"file_path":"notes.txt","content":"one"}`))
	call = nextGeneration(t, p)
	require.False(t, call.history[len(call.history)-1].ToolResults()[0].IsError)
	call.reply <- provider.ProviderResponse{Content: "Arquivo criado e conteúdo verificado.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	done, err = a.Run(context.Background(), "s", "Obrigado")
	require.NoError(t, err)
	call = nextGeneration(t, p)
	call.reply <- provider.ProviderResponse{Content: "Disponha.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Empty(t, task.FileChanges)
	require.False(t, task.Changed)
}

func TestFileReceiptsDoNotBypassWorkspaceVerificationAfterCommands(t *testing.T) {
	a, p, _ := fileEffectAgent(t, true)
	done, err := a.Run(context.Background(), "s", "Crie um arquivo de texto")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	replyTools(call, toolCall("request_tools", `{"names":["write","bash"]}`))
	call = nextGeneration(t, p)
	replyTools(call, toolCall("write", `{"file_path":"notes.txt","content":"one"}`))
	call = nextGeneration(t, p)
	replyTools(call, toolCall("bash", `{"command":"printf command-ran"}`))
	call = nextGeneration(t, p)
	require.False(t, call.history[len(call.history)-1].ToolResults()[0].IsError)
	call.reply <- provider.ProviderResponse{Content: "Arquivo criado.", FinishReason: message.FinishReasonEndTurn}
	require.ErrorContains(t, finishGeneration(t, done).Error, "workspace exceeds validation fingerprint limit")
}

func TestVerifiedFilesRemainSeparateWhenConfiguredChecksCannotBeVerified(t *testing.T) {
	a, p, _ := fileEffectAgent(t, true)
	config.Get().Checks = map[string]config.CheckConfig{"test": {Command: "go test ./..."}}
	done, err := a.Run(context.Background(), "s", "Crie um arquivo de texto")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	replyTools(call, toolCall("request_tools", `{"names":["write"]}`))
	call = nextGeneration(t, p)
	replyTools(call, toolCall("write", `{"file_path":"notes.txt","content":"one"}`))
	call = nextGeneration(t, p)
	require.False(t, call.history[len(call.history)-1].ToolResults()[0].IsError)
	call.reply <- provider.ProviderResponse{Content: "Arquivo criado; testes pendentes.", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Equal(t, "partial", result.Outcome)
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.True(t, task.HasFileChanges())
	require.Empty(t, task.Validation)
	require.Contains(t, task.Summary, "affected contents verified")
	require.Contains(t, task.Summary, "check validation unavailable")
	require.NotEmpty(t, task.ValidationProblem(checkCommands()))
}

func TestScopedCreationStillRequiresPermission(t *testing.T) {
	a, p, root := fileEffectAgent(t, false)
	done, err := a.Run(context.Background(), "s", "Crie um arquivo de texto")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	replyTools(call, toolCall("request_tools", `{"names":["write"]}`))
	call = nextGeneration(t, p)
	replyTools(call, toolCall("write", `{"file_path":"notes.txt","content":"one"}`))
	call = nextGeneration(t, p)
	denied := false
	for _, msg := range call.history {
		for _, result := range msg.ToolResults() {
			denied = denied || result.IsError && result.Content == "Permission denied"
		}
	}
	require.True(t, denied)
	replyTools(call, toolCall("finish_task", `{"status":"partial","summary":"Sem autorização para criar o arquivo"}`))
	call = nextGeneration(t, p)
	call.reply <- provider.ProviderResponse{Content: "Arquivo não criado; autorização negada.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	_, err = os.Stat(filepath.Join(root, "notes.txt"))
	require.ErrorIs(t, err, os.ErrNotExist)
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Empty(t, task.FileChanges)
}
