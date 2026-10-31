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

func TestProjectOverviewReadsImplementationBeforeFirstModelRequest(t *testing.T) {
	for _, path := range []string{"service.go", "service.py", "service.ts"} {
		t.Run(path, func(t *testing.T) {
			a, p, _, root := runtimeAgent(t, workflow.Ask)
			require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# River\nThe documentation describes a dashboard.\n"), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("// Application service handler\n// VERIFIED_IMPLEMENTATION_FACT: receives sensor samples\n"), 0600))
			read := &citationReadTool{BaseTool: tools.NewViewTool(nil)}
			a.tools = []tools.BaseTool{read, tools.NewCodeIndexTool()}
			done, err := a.Run(context.Background(), "s", "o que pode me falar sobre esse projeto?")
			require.NoError(t, err)
			call := nextGeneration(t, p)
			raw, err := json.Marshal(call.history)
			require.NoError(t, err)
			require.Contains(t, string(raw), "VERIFIED_IMPLEMENTATION_FACT")
			require.Contains(t, toolNames(call), "view")
			require.Contains(t, toolNames(call), "search_code_index")
			require.LessOrEqual(t, read.count.Load(), int32(3), "overview preparation must stay bounded")
			call.reply <- provider.ProviderResponse{Content: "A documentação descreve um dashboard (README.md:2); a implementação recebe amostras de sensores (" + path + ":2).", FinishReason: message.FinishReasonEndTurn}
			require.NoError(t, finishGeneration(t, done).Error)
		})
	}
}
