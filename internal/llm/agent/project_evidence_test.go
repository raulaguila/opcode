package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestEncodedChoicesWaitForHumanWithoutRepairGeneration(t *testing.T) {
	for _, mode := range []workflow.Mode{workflow.Ask, workflow.Execute} {
		t.Run(string(mode), func(t *testing.T) {
			a, p, _, _ := runtimeAgent(t, mode)
			a.questions.SetInteractive(true)
			done, err := a.Run(context.Background(), "s", "me faça uma pergunta")
			require.NoError(t, err)
			replyTools(nextGeneration(t, p), toolCall("ask_user_question", `{"question":"Qual área você quer revisar?","choices":"[\"UI\",\"Contexto\"]","explanation":""}`))
			var q interaction.Question
			require.Eventually(t, func() bool { var ok bool; q, ok = a.questions.Pending("s"); return ok }, time.Second, time.Millisecond)
			require.Equal(t, []string{"UI", "Contexto"}, q.Choices)
			select {
			case <-p.calls:
				t.Fatal("model called again before human answered")
			default:
			}
			require.NoError(t, a.questions.Answer("s", q.ID, "Contexto"))
			last := nextGeneration(t, p)
			require.Contains(t, last.history[len(last.history)-1].ToolResults()[0].Content, "Contexto")
			last.reply <- provider.ProviderResponse{Content: "Vamos revisar o contexto.", FinishReason: message.FinishReasonEndTurn}
			require.NoError(t, finishGeneration(t, done).Error)
			task, err := a.workflow.Get(context.Background(), "s")
			require.NoError(t, err)
			require.False(t, task.Changed)
		})
	}
}

func TestGenericProjectQuestionsRequireCitationsInEveryMode(t *testing.T) {
	for _, mode := range []workflow.Mode{workflow.Ask, workflow.Plan, workflow.Execute} {
		for _, question := range []string{"o que pode me falar do projeto atual?", "qual linguagens o projeto suporta?", "mas o projeto possui suporte a outras linguagens.."} {
			run := &generationRun{mode: mode, selected: map[string]bool{}}
			(&agent{}).prefetchTools(run, question)
			require.True(t, run.requiresCitation, "%s: %s", mode, question)
			if strings.Contains(question, "linguagens") {
				require.True(t, run.selected["get_language_support"])
			}
		}
	}
}

func TestBootstrapReusesOnlyCurrentVisibleSourcesAndProjectsOnce(t *testing.T) {
	a, p, saved, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# UNIQUE_REPOSITORY_FACT\nRepository evidence.\n"), 0600))
	read := &citationReadTool{BaseTool: tools.NewViewTool(nil)}
	a.tools = []tools.BaseTool{read}
	for i := 0; i < 3; i++ {
		if i == 2 {
			require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# UNIQUE_REPOSITORY_FACT\nChanged evidence.\n"), 0600))
		}
		done, err := a.Run(context.Background(), "s", "O que pode me falar do projeto atual?")
		require.NoError(t, err)
		call := nextGeneration(t, p)
		raw, err := json.Marshal(call.history)
		require.NoError(t, err)
		if i == 1 {
			require.EqualValues(t, 1, read.count.Load(), "unchanged evidence must not invoke view again")
			require.Equal(t, 1, countSourceBodies(call.history, "UNIQUE_REPOSITORY_FACT"), "do not duplicate source bodies in the provider request")
			history, err := saved.List(context.Background(), "s")
			require.NoError(t, err)
			require.Equal(t, 2, countSourceBodies(history, "UNIQUE_REPOSITORY_FACT"), "persisted transcript is intact")
			var turn string
			for _, msg := range history {
				if msg.Role == message.User && !msg.IsApplication() {
					turn = msg.ID
				}
			}
			receipts, _ := sourceReadReceipts(&generationRun{turnMessageID: turn}, history)
			require.NotEmpty(t, receipts)
		}
		if i == 2 {
			require.EqualValues(t, 2, read.count.Load(), "changed source requires a fresh read")
			require.Contains(t, string(raw), "Changed evidence")
		}
		call.reply <- provider.ProviderResponse{Content: "O projeto está descrito em README.md:1-2.", FinishReason: message.FinishReasonEndTurn}
		require.NoError(t, finishGeneration(t, done).Error)
	}
}

func countSourceBodies(history []message.Message, needle string) int {
	count := 0
	for _, msg := range history {
		for _, r := range msg.ToolResults() {
			count += strings.Count(r.Content, needle)
		}
	}
	return count
}

func TestTopicalBootstrapSearchesAndReadsFeatureSource(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Execute)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# Go implementation\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "language_registry.py"), []byte("# Supported languages, independent of implementation language\nlanguages = ['Python', 'Rust']\n"), 0600))
	read := &citationReadTool{BaseTool: tools.NewViewTool(nil)}
	a.tools = []tools.BaseTool{read, tools.NewCodeIndexTool(), tools.NewLanguageSupportTool()}
	done, err := a.Run(context.Background(), "s", "quais linguagens o projeto suporta?")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	raw, _ := json.Marshal(call.history)
	require.Contains(t, string(raw), "language_registry.py")
	require.Contains(t, string(raw), "Python")
	require.Contains(t, toolNames(call), "get_language_support")
	receipts, _ := sourceReadReceipts(&generationRun{}, call.history)
	require.NotEmpty(t, receipts, "index previews alone cannot satisfy citations")
	require.LessOrEqual(t, read.count.Load(), int32(2))
	call.reply <- provider.ProviderResponse{Content: "Suporta Python e Rust (language_registry.py:2).", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

func TestProjectFactsWithoutReadEvidenceFailClosedAfterOneCitationRepair(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	done, err := a.Run(context.Background(), "s", "quais linguagens o projeto suporta?")
	require.NoError(t, err)
	nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "Suporta tudo.", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	require.Contains(t, repair.history[len(repair.history)-1].Content().Text, "No verifiable current interval")
	repair.reply <- provider.ProviderResponse{Content: "Suporta tudo em imaginary.go:1.", FinishReason: message.FinishReasonEndTurn}
	require.ErrorContains(t, finishGeneration(t, done).Error, "citation remains missing")
}

func TestProviderCannotAssignContextProvenance(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Ask)
	a.tools = []tools.BaseTool{&countedTool{name: "view", output: "result"}}
	done, err := a.Run(context.Background(), "s", "Investigue")
	require.NoError(t, err)
	call := toolCall("view", `{}`)
	call.Origin = "context-reused"
	replyTools(nextGeneration(t, p), call)
	last := nextGeneration(t, p)
	for _, msg := range last.history {
		for _, call := range msg.ToolCalls() {
			require.Empty(t, call.Origin)
		}
	}
	last.reply <- provider.ProviderResponse{Content: "Concluído.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

func TestReusedSourceDoesNotSatisfyExplicitToolCall(t *testing.T) {
	run := &generationRun{requiredCalls: map[string]int{"view": 1}, startMessageID: "user"}
	history := []message.Message{
		{ID: "user", Role: message.User},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "reused", Name: "view", Origin: "context-reused"}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "reused", Content: "verified source"}}},
	}
	require.NotEmpty(t, missingRequiredCalls(run, history))
	history = append(history,
		message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "actual", Name: "view"}}},
		message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "actual", Content: "fresh read"}}})
	require.Empty(t, missingRequiredCalls(run, history))
}

func TestOpcodeLanguageBootstrapUsesCompactInventoryAndParserDefinition(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/raulaguila/opcode\n"), 0600))
	path := "internal/llm/tools/languages.go"
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("package fixture\nvar localParsers = map[string]int{\"python\":1, \"rust\":2}\n"), 0600))
	a.tools = []tools.BaseTool{tools.NewViewTool(nil), tools.NewCodeIndexTool(), tools.NewLanguageSupportTool()}
	done, err := a.Run(context.Background(), "s", "quais linguagens o projeto suporta?")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	var inventory, definition bool
	for _, msg := range call.history {
		for _, result := range msg.ToolResults() {
			inventory = inventory || strings.Contains(result.Content, `"profile_count":42`)
		}
		for _, tool := range msg.ToolCalls() {
			if tool.Name == "search_code_index" {
				require.Contains(t, tool.Input, "localParsers")
			}
			if tool.Name == "view" && strings.Contains(tool.Input, path) {
				definition = true
				var params tools.ViewParams
				require.NoError(t, json.Unmarshal([]byte(tool.Input), &params))
				require.LessOrEqual(t, params.Limit, 5, "read the short declaration, not sixty unrelated lines")
			}
		}
	}
	require.True(t, inventory)
	require.True(t, definition)
	call.reply <- provider.ProviderResponse{Content: "O inventário do Opcode é separado das linguagens de implementação. A definição está em internal/llm/tools/languages.go:2.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}
