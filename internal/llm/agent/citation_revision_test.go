package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func citationRevisionFixture(t *testing.T) (*agent, *controlledProvider, *memoryMessages, <-chan AgentEvent, generationCall, string, string) {
	t.Helper()
	a, p, saved, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n// inspected\n"), 0600))
	a.tools = []tools.BaseTool{tools.NewViewTool(nil)}
	done, err := a.Run(context.Background(), "s", "Explain source.go without editing it.")
	require.NoError(t, err)
	replyTools(nextGeneration(t, p), toolCall("view", `{"file_path":"source.go"}`))
	nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "Uncited original explanation.", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	originalID := ""
	for _, item := range repair.history {
		if item.Content().Text == "Uncited original explanation." {
			originalID = item.ID
			require.Equal(t, message.CitationRevising, item.CitationValidation())
		}
	}
	require.NotEmpty(t, originalID)
	return a, p, saved, done, repair, originalID, root
}

func TestCitationRevisionLinksOnlyVerifiedDirectReplacement(t *testing.T) {
	_, _, saved, done, repair, originalID, _ := citationRevisionFixture(t)
	require.Eventually(t, func() bool {
		history, err := saved.List(context.Background(), "s")
		if err != nil {
			return false
		}
		for _, item := range history {
			if item.RevisionOf() == originalID && item.Content().Text == "" {
				return item.CitationValidation() == message.CitationPending && !item.IsFinished()
			}
		}
		return false
	}, time.Second, time.Millisecond, "link must exist before any correction tokens are generated")
	repair.reply <- provider.ProviderResponse{Content: "The package is declared in source.go:1.", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Equal(t, originalID, result.Message.RevisionOf())
	require.Equal(t, message.CitationValid, result.Message.CitationValidation())
	history, err := saved.List(context.Background(), "s")
	require.NoError(t, err)
	for _, item := range history {
		if item.ID == originalID {
			require.Equal(t, message.CitationSuperseded, item.CitationValidation())
			require.Equal(t, "Uncited original explanation.", item.Content().Text)
		}
	}
}

func TestCitationRevisionTruncatedCandidateCannotSupersedeOriginal(t *testing.T) {
	_, p, saved, done, repair, originalID, _ := citationRevisionFixture(t)
	repair.reply <- provider.ProviderResponse{Content: "The package is declared in source.go:1.", FinishReason: message.FinishReasonMaxTokens}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Equal(t, message.FinishReasonMaxTokens, result.Message.FinishReason())
	require.Equal(t, originalID, result.Message.RevisionOf(), "a truncated correction remains a failed linked attempt, never a second primary answer")
	require.Equal(t, message.CitationValid, result.Message.CitationValidation(), "source provenance may validate even though the answer is incomplete")
	requireOriginalInvalid(t, saved, originalID)
	select {
	case <-p.calls:
		t.Fatal("truncation must not create an extra citation repair attempt")
	default:
	}
}

func TestWorkflowRepairDetachesCitationCandidateBeforeResolvingStatus(t *testing.T) {
	a, _, saved, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n"), 0600))
	history, _ := citationHistory(t, "view", `{"file_path":"source.go"}`)
	original, err := saved.Create(context.Background(), "s", message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Uncited explanation.", CitationStatus: message.CitationRevising}}})
	require.NoError(t, err)
	candidate, err := saved.Create(context.Background(), "s", message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "source.go:1", CitationStatus: message.CitationPending, RevisionOf: original.ID}}})
	require.NoError(t, err)
	run := &generationRun{requiresCitation: true, mode: workflow.Ask, selected: map[string]bool{}, toolLeases: map[string]int{}, requiredCalls: map[string]int{"bash": 1}, citationRevisionOf: original.ID, citationRevision: &citationRevision{target: original, turnID: history[0].ID}}
	g := generationState{agent: a, ctx: context.Background(), sessionID: "s", run: run, history: append(history, original, candidate)}
	retry, err := g.validateCompletion(candidate)
	require.NoError(t, err)
	require.True(t, retry)
	require.Nil(t, run.citationRevision)
	current, err := saved.List(context.Background(), "s")
	require.NoError(t, err)
	for _, item := range current {
		if item.ID == candidate.ID {
			require.Empty(t, item.RevisionOf(), "status resolution must not restore an old link from a stale local copy")
			require.Equal(t, message.CitationValid, item.CitationValidation())
		}
		if item.ID == original.ID {
			require.Equal(t, message.CitationInvalid, item.CitationValidation())
		}
	}
}

func TestCitationRevisionFailureRestoresInvalidOriginal(t *testing.T) {
	_, _, saved, done, repair, originalID, root := citationRevisionFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package changed\n// changed\n"), 0600))
	repair.reply <- provider.ProviderResponse{Content: "The package is declared in source.go:1.", FinishReason: message.FinishReasonEndTurn}
	require.ErrorIs(t, finishGeneration(t, done).Error, ErrSourceCitation)
	requireOriginalInvalid(t, saved, originalID)
}

func TestCitationRevisionCancellationRestoresInvalidOriginal(t *testing.T) {
	a, _, saved, done, _, originalID, _ := citationRevisionFixture(t)
	a.Cancel("s")
	require.Error(t, finishGeneration(t, done).Error)
	requireOriginalInvalid(t, saved, originalID)
}

func TestInterruptedToolRevisionCannotRetainCorrectionLink(t *testing.T) {
	a, _, saved, _ := runtimeAgent(t, workflow.Ask)
	item, err := saved.Create(context.Background(), "s", message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Independent tool progress.", CitationStatus: message.CitationPending, RevisionOf: "original"}, toolCall("view", `{"file_path":"source.go"}`)}})
	require.NoError(t, err)
	failure := context.Canceled
	var result *message.Message
	a.preserveInterruptedStream("s", &item, &result, &failure)
	require.Empty(t, item.RevisionOf())
	require.Equal(t, message.CitationInvalid, item.CitationValidation())
	require.NotNil(t, result)
}

func TestCitationRevisionToolCallsBecomeIndependentProgress(t *testing.T) {
	_, p, saved, done, repair, originalID, _ := citationRevisionFixture(t)
	repair.reply <- provider.ProviderResponse{Content: "Checking available tools.", ToolCalls: []message.ToolCall{toolCall("request_tools", `{"names":["get_context_status"]}`)}, FinishReason: message.FinishReasonToolUse}
	last := nextGeneration(t, p)
	for _, item := range last.history {
		if item.Content().Text == "Checking available tools." {
			require.Empty(t, item.RevisionOf())
		}
	}
	last.reply <- provider.ProviderResponse{Content: "The package is declared in source.go:1.", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Empty(t, result.Message.RevisionOf())
	requireOriginalInvalid(t, saved, originalID)
}

func TestCitationRevisionNativeRereadKeepsOnlyFinalAnswerLinked(t *testing.T) {
	_, p, saved, done, repair, originalID, _ := citationRevisionFixture(t)
	repair.reply <- provider.ProviderResponse{Content: "Reading the source again.", ToolCalls: []message.ToolCall{toolCall("view", `{"file_path":"source.go"}`)}, FinishReason: message.FinishReasonToolUse}
	last := nextGeneration(t, p)
	for _, item := range last.history {
		if item.ID == originalID {
			require.Equal(t, message.CitationRevising, item.CitationValidation())
		}
		if item.Content().Text == "Reading the source again." {
			require.Empty(t, item.RevisionOf(), "tool narration remains an independent public message")
		}
	}
	last.reply <- provider.ProviderResponse{Content: "The package is declared in source.go:1.", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Equal(t, originalID, result.Message.RevisionOf())
	history, err := saved.List(context.Background(), "s")
	require.NoError(t, err)
	for _, item := range history {
		if item.ID == originalID {
			require.Equal(t, message.CitationSuperseded, item.CitationValidation())
		}
	}
}

func TestCitationRevisionRereadFailureOrChangedHashCannotPromote(t *testing.T) {
	for _, scenario := range []string{"failed read", "changed after read"} {
		t.Run(scenario, func(t *testing.T) {
			_, p, saved, done, repair, originalID, root := citationRevisionFixture(t)
			path := filepath.Join(root, "source.go")
			if scenario == "failed read" {
				require.NoError(t, os.Remove(path))
			}
			repair.reply <- provider.ProviderResponse{Content: "Reading the source again.", ToolCalls: []message.ToolCall{toolCall("view", `{"file_path":"source.go"}`)}, FinishReason: message.FinishReasonToolUse}
			last := nextGeneration(t, p)
			if scenario == "changed after read" {
				require.NoError(t, os.WriteFile(path, []byte("package changed\n"), 0600))
			}
			last.reply <- provider.ProviderResponse{Content: "The package is declared in source.go:1.", FinishReason: message.FinishReasonEndTurn}
			require.ErrorIs(t, finishGeneration(t, done).Error, ErrSourceCitation)
			requireOriginalInvalid(t, saved, originalID)
		})
	}
}

type opaqueCitationRead struct{ tools.BaseTool }

func (*opaqueCitationRead) OpaqueTool() bool { return true }

func TestCitationRevisionRetainsOnlyActualNativeSourceReadBatches(t *testing.T) {
	view := tools.NewViewTool(nil)
	run := &generationRun{available: []tools.BaseTool{view, tools.NewReadPageTool()}}
	require.True(t, citationReadOnlyBatch(run, []message.ToolCall{toolCall("view", `{}`), toolCall("read_lines", `{}`)}))
	for _, calls := range [][]message.ToolCall{nil, {toolCall("read_symbol", `{}`)}, {toolCall("view", `{}`), toolCall("ask_user_question", `{}`)}, {toolCall("view", `{}`), toolCall("request_tools", `{}`)}, {toolCall("view", `{}`), toolCall("write", `{}`)}} {
		require.False(t, citationReadOnlyBatch(run, calls))
	}
	run.available = []tools.BaseTool{&opaqueCitationRead{BaseTool: view}}
	require.False(t, citationReadOnlyBatch(run, []message.ToolCall{toolCall("view", `{}`)}), "remote tools cannot impersonate a native source reader")
}

func TestCitationRevisionSteeringCannotReplacePriorHumanTurn(t *testing.T) {
	a, p, saved, done, repair, originalID, _ := citationRevisionFixture(t)
	_, queued, err := a.Submit(context.Background(), "s", "Agora responda OK, sem referências.")
	require.NoError(t, err)
	require.True(t, queued)
	repair.reply <- provider.ProviderResponse{Content: "The package is declared in source.go:1.", FinishReason: message.FinishReasonEndTurn}
	last := nextGeneration(t, p)
	last.reply <- provider.ProviderResponse{Content: "OK", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Empty(t, result.Message.RevisionOf())
	requireOriginalInvalid(t, saved, originalID)
	history, err := saved.List(context.Background(), "s")
	require.NoError(t, err)
	for _, item := range history {
		require.Empty(t, item.RevisionOf(), "a superseded request must not retain an apparent correction link")
		require.NotEqual(t, message.CitationPending, item.CitationValidation(), "detached steering output is no longer awaiting a citation revision")
	}
}

func TestCitationRevisionBudgetExitCannotStayRevising(t *testing.T) {
	a, p, saved, root := runtimeAgent(t, workflow.Ask)
	config.Get().Runtime.MaxSteps = 2
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n"), 0600))
	a.tools = []tools.BaseTool{tools.NewViewTool(nil)}
	done, err := a.Run(context.Background(), "s", "Explain source.go without editing it.")
	require.NoError(t, err)
	replyTools(nextGeneration(t, p), toolCall("view", `{"file_path":"source.go"}`))
	nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "Uncited original explanation.", FinishReason: message.FinishReasonEndTurn}
	require.ErrorContains(t, finishGeneration(t, done).Error, "step limit")
	history, err := saved.List(context.Background(), "s")
	require.NoError(t, err)
	for _, item := range history {
		require.NotEqual(t, message.CitationRevising, item.CitationValidation())
	}
}

type failedRevisionWrite struct {
	message.Service
	target string
	fail   bool
}

func (s *failedRevisionWrite) Update(ctx context.Context, item message.Message) error {
	if item.ID == s.target && item.CitationValidation() == message.CitationInvalid && s.fail {
		s.fail = false
		return errors.New("injected revision write failure")
	}
	return s.Service.Update(ctx, item)
}

func TestCitationRevisionFailedCleanupPreservesSnapshotForRetry(t *testing.T) {
	a, _, saved, _ := runtimeAgent(t, workflow.Ask)
	original, err := saved.Create(context.Background(), "s", message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Original diagnosis.", CitationStatus: message.CitationRevising}}})
	require.NoError(t, err)
	a.messages = &failedRevisionWrite{Service: saved, target: original.ID, fail: true}
	run := &generationRun{citationRevisionOf: original.ID, citationRevision: &citationRevision{target: original}}
	g := generationState{agent: a, ctx: context.Background(), sessionID: "s", run: run, history: []message.Message{original}}
	require.ErrorContains(t, g.abandonCitationRevision(false), "injected revision write failure")
	require.NotNil(t, run.citationRevision)
	require.Equal(t, message.CitationRevising, run.citationRevision.target.CitationValidation())
	require.NoError(t, g.abandonCitationRevision(false))
	require.Nil(t, run.citationRevision)
	requireOriginalInvalid(t, saved, original.ID)
}

func requireOriginalInvalid(t *testing.T, saved *memoryMessages, id string) {
	t.Helper()
	history, err := saved.List(context.Background(), "s")
	require.NoError(t, err)
	found := false
	for _, item := range history {
		require.NotEqual(t, message.CitationRevising, item.CitationValidation())
		if item.ID == id {
			found = true
			require.Equal(t, message.CitationInvalid, item.CitationValidation())
		}
	}
	require.True(t, found)
}
