package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestFlowBootstrapReadsBoundedDownstreamImplementation(t *testing.T) {
	for _, question := range []string{"quero que analise o fluxo de chat e chamada de tools", "Analyze the flow starting at chat.go"} {
		t.Run(question, func(t *testing.T) { assertFlowBootstrap(t, question) })
	}
}

func assertFlowBootstrap(t *testing.T, question string) {
	t.Helper()
	a, p, _, root := runtimeAgent(t, workflow.Ask)
	for path, content := range map[string]string{
		"chat.go":     "package fixture\nfunc chatTools() string { return dispatchBatch() }\n",
		"dispatch.go": "package fixture\nfunc dispatchBatch() string { return \"VERIFIED_DOWNSTREAM_RESULT\" }\n",
		"README.md":   "# Historical chat tool architecture\nOLD_DOCUMENT_DOES_NOT_PROVE_EXECUTION\n",
		"noise.go":    "package fixture\nfunc unrelated() string { return \"quero analise fluxo chat chamada tools DECOY_QUERY\" }\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0600))
	}
	read := &citationReadTool{BaseTool: tools.NewViewTool(nil)}
	a.tools = []tools.BaseTool{read, tools.NewCodeIndexTool()}
	done, err := a.Run(context.Background(), "s", question)
	require.NoError(t, err)
	call := nextGeneration(t, p)
	require.Contains(t, toolNames(call), "search_code_index")
	require.LessOrEqual(t, read.count.Load(), int32(4))
	raw, err := json.Marshal(call.history)
	require.NoError(t, err)
	require.Contains(t, string(raw), "VERIFIED_DOWNSTREAM_RESULT")
	receipts, _ := sourceReadReceipts(&generationRun{}, call.history)
	require.NotEmpty(t, receipts, "previews never count as successful source reads")
	require.Contains(t, flowResponseGuidance(&generationRun{turnMessageID: call.history[0].ID}, call.history), "Interfaces, signatures")
	call.reply <- provider.ProviderResponse{Content: "O chat delega ao lote (chat.go:2); o resultado vem de dispatchBatch (dispatch.go:2). Esta leitura não executou o fluxo.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

func TestFlowCallHintSkipsDeclarationsAndComments(t *testing.T) {
	require.Equal(t, "dispatchBatch", flowCallHint("1|// fakeCall()\n2|func chatTools() string { return dispatchBatch() }\n"))
	require.Equal(t, "dispatch_batch", flowCallHint("1|def chat_tools():\n2|    return dispatch_batch()\n"))
	require.Equal(t, "dispatchBatch", flowCallHint("1|export function chatTools() {\n2| return dispatchBatch();\n"))
}

func TestFlowGuidancePreservesExecutableEvidenceAndEveryRequestedBranchDuringRepair(t *testing.T) {
	run := &generationRun{turnMessageID: "original"}
	history := []message.Message{
		{ID: "original", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Qual handler é chamado neste projeto? Leia o caminho de execução e explique todos os retornos."}}},
		{ID: "repair", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Application validation: fix references", Origin: "application"}}},
	}
	guidance := flowResponseGuidance(run, history)
	for _, phrase := range []string{"executable call/return statement lines", "not only a declaration header", "every input/result explicitly requested", "all requested branches when repairing", "complete original answer"} {
		require.Contains(t, guidance, phrase)
	}
	require.Empty(t, flowResponseGuidance(&generationRun{turnMessageID: "repair"}, history))
}
