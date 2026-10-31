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

func TestReviewPreparationReadsImplementationAndTestsBeforeFirstRequest(t *testing.T) {
	for _, withTests := range []bool{false, true} {
		t.Run(map[bool]string{false: "tests unavailable", true: "tests available"}[withTests], func(t *testing.T) {
			a, p, _, root := runtimeAgent(t, workflow.Ask)
			files := map[string]string{
				"agent.go":   "package fixture\ntype Service interface { SetMode(string) error }\n",
				"runtime.go": "package fixture\ntype Agent struct { busy bool }\nfunc (a *Agent) SetMode(mode string) error {\n if a.busy { return ErrSessionBusy }\n return nil\n}\n",
				"README.md":  "# Historical review\nMISSING_MODE_GUARD: a stale claim, not implementation evidence.\n",
			}
			if withTests {
				files["runtime_test.go"] = "package fixture\nimport \"testing\"\nfunc TestModeCannotChangeDuringRequest(t *testing.T) {\n a := &Agent{busy:true}\n if a.SetMode(\"execute\") != ErrSessionBusy { t.Fatal(\"EXISTING_BUSY_REGRESSION\") }\n}\n"
			}
			for path, content := range files {
				require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0600))
			}
			read := &citationReadTool{BaseTool: tools.NewViewTool(nil)}
			a.tools = []tools.BaseTool{read, tools.NewCodeIndexTool()}
			done, err := a.Run(context.Background(), "s", "Sugira melhorias no código SetMode deste projeto.")
			require.NoError(t, err)
			call := nextGeneration(t, p)
			raw, err := json.Marshal(call.history)
			require.NoError(t, err)
			require.Contains(t, string(raw), "if a.busy")
			require.True(t, containsReviewRead(call.history, "runtime.go"))
			require.False(t, containsReviewRead(call.history, "agent.go"), "a declaration must not replace the available implementation")
			require.Equal(t, withTests, containsReviewRead(call.history, "runtime_test.go"))
			require.LessOrEqual(t, read.count.Load(), int32(4))
			require.NotContains(t, string(raw), "MISSING_MODE_GUARD")
			call.reply <- provider.ProviderResponse{Content: "A proteção já existe no corpo de SetMode (runtime.go:3-5). Não executei testes.", FinishReason: message.FinishReasonEndTurn}
			require.NoError(t, finishGeneration(t, done).Error)
		})
	}
}

func containsReviewRead(history []message.Message, path string) bool {
	for _, msg := range history {
		for _, result := range msg.ToolResults() {
			if receipt, ok := tools.VisibleSourceReadReceipt(result.Name, result.Content, result.Metadata); ok && filepath.Base(receipt.Path) == path {
				return true
			}
		}
	}
	return false
}

func TestReviewPreparationKeepsBasicContextWhenSearchFindsNoSource(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# REVIEW_FALLBACK_CONTEXT\n"), 0600))
	read := &citationReadTool{BaseTool: tools.NewViewTool(nil)}
	a.tools = []tools.BaseTool{read, &countedTool{name: "search_code_index", output: `{"results":[]}`}}
	done, err := a.Run(context.Background(), "s", "Sugira melhorias de lógica e redução de complexidade no código do projeto")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	require.True(t, containsReviewRead(call.history, "README.md"))
	require.LessOrEqual(t, read.count.Load(), int32(3))
	call.reply <- provider.ProviderResponse{Content: "Inspecionei apenas a documentação (README.md:1); faltam implementações e testes para justificar mudanças.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

func TestReviewPreparationPreservesSpecificLanguageTopic(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/raulaguila/opcode\n"), 0600))
	path := "internal/llm/tools/languages.go"
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("package fixture\nvar localParsers = map[string]int{\"python\":1}\n"), 0600))
	a.tools = []tools.BaseTool{tools.NewViewTool(nil), tools.NewCodeIndexTool(), tools.NewLanguageSupportTool()}
	done, err := a.Run(context.Background(), "s", "Analise as linguagens do projeto")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	raw, err := json.Marshal(call.history)
	require.NoError(t, err)
	require.Contains(t, string(raw), "get_language_support")
	for _, msg := range call.history {
		for _, tool := range msg.ToolCalls() {
			if tool.Name == "search_code_index" {
				var params struct{ Query string }
				require.NoError(t, json.Unmarshal([]byte(tool.Input), &params))
				require.Equal(t, "localParsers", params.Query)
			}
		}
	}
	call.reply <- provider.ProviderResponse{Content: "O inventário e a definição foram inspecionados (internal/llm/tools/languages.go:2).", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}
