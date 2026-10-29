package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestDependentEditWaitsForReadResult(t *testing.T) {
	for _, readFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "read-first", false: "edit-first"}[readFirst], func(t *testing.T) {
			a, p, _, root := runtimeAgent(t, workflow.Execute)
			path := filepath.Join(root, "counter.py")
			require.NoError(t, os.WriteFile(path, []byte("def add(a, b):\n    return a - b\n"), 0600))
			edit := &countedTool{name: "edit", output: "Edit attempted"}
			a.tools = []tools.BaseTool{tools.NewViewTool(nil), edit}
			done, err := a.Run(context.Background(), "s", "Leia counter.py e corrija add")
			require.NoError(t, err)
			first := nextGeneration(t, p)
			read := toolCall("view", `{"file_path":"counter.py"}`)
			wrong := toolCall("edit", `{"file_path":"./counter.py","old_string":"add","new_string":"a + b"}`)
			calls := []message.ToolCall{read, wrong}
			if !readFirst {
				calls = []message.ToolCall{wrong, read}
			}
			replyTools(first, calls...)
			next := nextGeneration(t, p)
			require.Zero(t, edit.count.Load(), "a sibling read cannot justify invented edit arguments")
			results := next.history[len(next.history)-1].ToolResults()
			for i, call := range calls {
				if call.Name == "edit" {
					require.True(t, results[i].IsError)
					require.Contains(t, results[i].Content, "No change applied")
				} else {
					require.False(t, results[i].IsError)
					require.Contains(t, results[i].Content, "return a - b")
				}
			}
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Contains(t, string(raw), "def add(a, b):")
			replyTools(next, toolCall("edit", `{"file_path":"counter.py","old_string":"return a - b","new_string":"return a + b"}`), toolCall("finish_task", `{"status":"partial","summary":"Edit attempted; verification pending"}`))
			last := nextGeneration(t, p)
			require.EqualValues(t, 1, edit.count.Load(), "the normal edit tool is available after the observed result")
			last.reply <- provider.ProviderResponse{Content: "Verification pending", FinishReason: message.FinishReasonEndTurn}
			require.NoError(t, finishGeneration(t, done).Error)
		})
	}
}

func TestSourceDependencyCoversPagedReadsAndPatchesWithoutBlockingIndependentFiles(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Execute)
	for _, read := range []string{"view", "read_lines", "read_symbol"} {
		reads := batchSourceReads([]message.ToolCall{toolCall(read, `{"file_path":"counter.py"}`)})
		for _, write := range []string{"edit", "write", "lsp_replace_symbol", "lsp_rename"} {
			require.True(t, mutationAwaitingSource(toolCall(write, `{"file_path":"`+filepath.Join(root, "counter.py")+`"}`), reads))
			require.False(t, mutationAwaitingSource(toolCall(write, `{"file_path":"unrelated.py"}`), reads))
		}
		require.True(t, mutationAwaitingSource(toolCall("patch", `{"patch_text":"*** Begin Patch\n*** Update File: counter.py\n@@\n-a\n+b\n*** End Patch"}`), reads))
		require.False(t, mutationAwaitingSource(toolCall("bash", `{"command":"echo hello"}`), reads))
	}
}
