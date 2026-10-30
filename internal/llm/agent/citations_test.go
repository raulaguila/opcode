package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestExplanationRepairsMissingCitationWithoutRepeatingRead(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "counter.py"), []byte("def add(a, b):\n    return a - b"), 0600))
	read := &citationReadTool{BaseTool: tools.NewViewTool(nil)}
	a.tools = []tools.BaseTool{read}
	done, err := a.Run(context.Background(), "s", "Localize a causa do erro; leia counter.py e explique sem editar arquivos.")
	require.NoError(t, err)
	replyTools(nextGeneration(t, p), toolCall("view", `{"file_path":"counter.py"}`))
	answer := nextGeneration(t, p)
	answer.reply <- provider.ProviderResponse{Content: "add usa subtração em vez de soma.", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	require.Contains(t, repair.history[len(repair.history)-1].Content().Text, "counter.py")
	require.Contains(t, repair.history[len(repair.history)-1].Content().Text, "Do not invent citations")
	repair.reply <- provider.ProviderResponse{Content: "Em counter.py:2, add usa a - b em vez de a + b.", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Contains(t, result.Message.Content().Text, "counter.py:2")
	require.EqualValues(t, 1, read.count.Load())
	usage, e := a.Usage(context.Background(), "s")
	require.NoError(t, e)
	require.EqualValues(t, 1, usage.Repairs)
}

func TestInitialSourceReadsPreserveVerifiableReceiptsThroughRepair(t *testing.T) {
	for _, scenario := range []struct{ file, source, request, answer string }{
		{"README.md", "# IgarapeWatch\nSensores MQTT e SQLite.\n", "Explique em português o projeto atual usando os arquivos reais. Cite IgarapeWatch, MQTT, SQLite e o arquivo consultado.", "IgarapeWatch recebe sensores MQTT e armazena SQLite (README.md:1-2)."},
		{"main.py", "def add(a,b):\n    return a - b\n", "Localize no projeto a causa de add(2,3) retornar -1. Leia main.py e explique sem editar arquivos.", "Em main.py:2, add subtrai os argumentos em vez de somá-los."},
	} {
		t.Run(scenario.file, func(t *testing.T) {
			a, p, _, root := runtimeAgent(t, workflow.Ask)
			require.NoError(t, os.WriteFile(filepath.Join(root, scenario.file), []byte(scenario.source), 0600))
			read := &citationReadTool{BaseTool: tools.NewViewTool(nil)}
			a.tools = []tools.BaseTool{read}
			done, err := a.Run(context.Background(), "s", scenario.request)
			require.NoError(t, err)
			first := nextGeneration(t, p)
			receipts, attempted := sourceReadReceipts(&generationRun{}, first.history)
			require.True(t, attempted)
			require.Len(t, receipts, 1)
			require.Equal(t, filepath.Join(root, scenario.file), receipts[0].Path)
			require.True(t, tools.SourceReadCurrent(context.Background(), receipts[0]))
			first.reply <- provider.ProviderResponse{Content: "Explicação sem citação.", FinishReason: message.FinishReasonEndTurn}
			repair := nextGeneration(t, p)
			notice := repair.history[len(repair.history)-1].Content().Text
			require.Contains(t, notice, scenario.file+":1-2")
			require.Contains(t, notice, scenario.request)
			require.Contains(t, notice, "not a new user task")
			repair.reply <- provider.ProviderResponse{Content: scenario.answer, FinishReason: message.FinishReasonEndTurn}
			result := finishGeneration(t, done)
			require.NoError(t, result.Error)
			require.Equal(t, scenario.answer, result.Message.Content().Text)
			require.EqualValues(t, 1, read.count.Load(), "a repair must not replay the initial read")
		})
	}
}

func TestCitationRepairIsBoundedAndDoesNotFabricateReferences(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n"), 0600))
	a.tools = []tools.BaseTool{tools.NewViewTool(nil)}
	done, err := a.Run(context.Background(), "s", "Explain this code")
	require.NoError(t, err)
	replyTools(nextGeneration(t, p), toolCall("view", `{"file_path":"source.go"}`))
	for range 2 {
		nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "Uncited explanation", FinishReason: message.FinishReasonEndTurn}
	}
	failure := finishGeneration(t, done).Error
	require.ErrorContains(t, failure, "citation remains missing")
	require.ErrorContains(t, failure, "No file:line reference was supplied")
	require.ErrorContains(t, failure, "source.go:1-1")
}

func TestCitationPresenceUsesOnlyCurrentSuccessfulReadReceipts(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg/source.go"), []byte("package source\n// inspected line\n"), 0600))
	result, err := tools.NewReadPageTool().Run(context.Background(), tools.ToolCall{Input: `{"file_path":"pkg/source.go"}`})
	require.NoError(t, err)
	run := &generationRun{requiresCitation: true, startMessageID: "prior-request", turnMessageID: "current"}
	history := []message.Message{
		{ID: "prior-request", Role: message.User},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "old", Name: "view", Input: `{"file_path":"old.go"}`}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "old"}}},
		{ID: "current", Role: message.User},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "missing", Name: "view", Input: `{"file_path":"missing.go"}`}, message.ToolCall{ID: "search", Name: "search_code_index", Input: `{"path":"index.go"}`}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "missing", IsError: true}, message.ToolResult{ToolCallID: "search", Content: "source.go"}}},
	}
	require.Empty(t, missingSourceCitation(run, history, "The requested source could not be read."))
	history = append(history, message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "read", Name: "read_lines", Input: `{"file_path":"pkg/source.go"}`}}}, message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "read", Content: result.Content, Metadata: result.Metadata}}})
	require.Contains(t, missingSourceCitation(run, history, "old.go:1, missing.go:2, index.go:1"), "pkg/source.go")
	require.Empty(t, missingSourceCitation(run, history, "pkg/source.go:2"))
	run.requiresCitation = false
	require.Empty(t, missingSourceCitation(run, history, "No code explanation was requested."))
}

func TestCitationFailureDiagnosticIsBoundedAndRedactsCredentials(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n"), 0600))
	a.tools = []tools.BaseTool{tools.NewViewTool(nil)}
	const secret = "synthetic-citation-private-key"
	t.Setenv("OPCODE_TEST_CITATION_API_KEY", secret)
	done, err := a.Run(context.Background(), "s", "Explain this code")
	require.NoError(t, err)
	replyTools(nextGeneration(t, p), toolCall("view", `{"file_path":"source.go"}`))
	answer := secret + strings.Repeat("x", 3000) + ".go:1"
	for range 2 {
		nextGeneration(t, p).reply <- provider.ProviderResponse{Content: answer, FinishReason: message.FinishReasonEndTurn}
	}
	failure := finishGeneration(t, done).Error
	require.ErrorContains(t, failure, "Unverified references")
	require.NotContains(t, failure.Error(), secret)
	require.Less(t, len(failure.Error()), 2000)
}

func TestCitationHeuristicRespectsNegationAndExplicitReferenceOptOut(t *testing.T) {
	for _, request := range []string{"Explique este código", "Localize a causa do erro", "Explain this function", "Where is the bug?"} {
		require.True(t, requiresSourceCitation(request), request)
	}
	for _, request := range []string{"Não explique o código; responda OK", "Do not explain the file", "Don't explain this", "Explique em uma frase sem referências", "Explain the function but do not cite paths"} {
		require.False(t, requiresSourceCitation(request), request)
	}
	a := &agent{}
	run := &generationRun{mode: workflow.Ask}
	a.prefetchTools(run, "Explique a função")
	require.True(t, run.requiresCitation)
	a.prefetchTools(run, "Preserve o idioma português")
	require.True(t, run.requiresCitation)
	a.prefetchTools(run, "Não cite caminhos na resposta")
	require.False(t, run.requiresCitation, "a later user instruction overrides the default reference style")
}

type citationReadTool struct {
	tools.BaseTool
	count atomic.Int32
}

func (t *citationReadTool) Run(ctx context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	t.count.Add(1)
	return t.BaseTool.Run(ctx, call)
}

func citationHistory(t *testing.T, name, input string) ([]message.Message, string) {
	t.Helper()
	var tool tools.BaseTool
	switch name {
	case "view":
		tool = tools.NewViewTool(nil)
	case "read_lines":
		tool = tools.NewReadPageTool()
	case "read_symbol":
		for _, candidate := range tools.NewProjectTools(nil) {
			if candidate.Info().Name == name {
				tool = candidate
			}
		}
	}
	require.NotNil(t, tool)
	result, err := tools.SecureTool(tool, nil).Run(context.Background(), tools.ToolCall{Input: input})
	require.NoError(t, err)
	require.False(t, result.IsError, result.Content)
	return []message.Message{
		{ID: "current", Role: message.User},
		{ID: "read-call", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "read", Name: name, Input: input}}},
		{ID: "read-result", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "read", Content: result.Content, Metadata: result.Metadata}}},
	}, result.Content
}

func TestCitationValidationRejectsUnreadLinesPathsAndStaleHashes(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	path := filepath.Join(root, "source.go")
	require.NoError(t, os.WriteFile(path, []byte("package source\n// read this line\n// another inspected line\n// not inspected\n"), 0600))
	run := &generationRun{requiresCitation: true, turnMessageID: "current"}
	for _, name := range []string{"view", "read_lines"} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(path, []byte("package source\n// read this line\n// another inspected line\n// not inspected\n"), 0600))
			history, _ := citationHistory(t, name, `{"file_path":"source.go","offset":1,"limit":2}`)
			for _, answer := range []string{"source.go", "source.go:1", "source.go:4", "source.go:2-4", "source.go:3-2", "source.go:0", "invented.go:2", "source.go:2 plus invented.go:1", "prefixsource.go:2"} {
				require.NotEmpty(t, missingSourceCitation(run, history, answer), answer)
			}
			for _, answer := range []string{"source.go:2", "source.go:2-3", "`source.go:3`", "[source](source.go#L2-L3)", fmt.Sprintf("[source](%s:2)", path)} {
				require.Empty(t, missingSourceCitation(run, history, answer), answer)
			}
			require.NoError(t, os.WriteFile(path, []byte("package source\n// changed externally\n// another inspected line\n// not inspected\n"), 0600))
			require.NotEmpty(t, missingSourceCitation(run, history, "source.go:2"))
			// A later successful read re-establishes current evidence.
			fresh, _ := citationHistory(t, name, `{"file_path":"source.go","offset":1,"limit":2}`)
			require.Empty(t, missingSourceCitation(run, append(history, fresh[1:]...), "source.go:2"))
		})
	}
}

func TestRepairCitationChoicesNeverCertifyChangedOrUnseenSources(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	path := filepath.Join(root, "source.go")
	require.NoError(t, os.WriteFile(path, []byte("package source\n// inspected\n"), 0600))
	history, _ := citationHistory(t, "view", `{"file_path":"source.go","offset":1,"limit":1}`)
	run := &generationRun{requiresCitation: true, turnMessageID: "current"}
	require.Equal(t, []string{"source.go:2-2"}, currentCitationChoices(run, history))
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:1"))
	require.NotEmpty(t, missingSourceCitation(run, history, "invented.go:2"))
	// A model may select a previously suggested enum value after an external edit.
	// It must still fail the ordinary guard; a schema suggestion grants no proof.
	require.NoError(t, os.WriteFile(path, []byte("package source\n// changed\n"), 0600))
	require.Empty(t, currentCitationChoices(run, history))
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:2-2"))
}

func TestCitationValidationNeedsNativeReceiptNotProseSearchOrHistoricalSummary(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n// inspected\n"), 0600))
	history, _ := citationHistory(t, "read_lines", `{"file_path":"source.go"}`)
	run := &generationRun{requiresCitation: true, turnMessageID: "current"}
	require.Empty(t, missingSourceCitation(run, history, "source.go:2"))
	for _, mutate := range []func([]message.Message){
		func(h []message.Message) {
			r := h[2].ToolResults()[0]
			r.Metadata = ""
			h[2].Parts = []message.ContentPart{r}
		},
		func(h []message.Message) {
			r := h[2].ToolResults()[0]
			r.Content = "source.go:2"
			h[2].Parts = []message.ContentPart{r}
		},
		func(h []message.Message) {
			r := h[2].ToolResults()[0]
			r.Metadata = strings.Repeat("x", 4097)
			h[2].Parts = []message.ContentPart{r}
		},
	} {
		copyHistory := append([]message.Message(nil), history...)
		mutate(copyHistory)
		require.NotEmpty(t, missingSourceCitation(run, copyHistory, "source.go:2"))
	}
	// An error carrying a plausible receipt never establishes evidence.
	failed := append([]message.Message(nil), history...)
	r := failed[2].ToolResults()[0]
	r.IsError = true
	failed[2].Parts = []message.ContentPart{r}
	require.Empty(t, missingSourceCitation(run, failed, "Could not inspect source."))
	// An absent current turn cannot authenticate historical receipts.
	run.turnMessageID = "later"
	require.Empty(t, missingSourceCitation(run, history, "No source read in this turn."))
}

func TestCitationSourceLocatorsSurviveCompactProjectionWithoutBecomingEvidence(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n// inspected\n"), 0600))
	history, _ := citationHistory(t, "read_lines", `{"file_path":"source.go"}`)
	history = append(history,
		message.Message{ID: "call-2", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "next", Name: "ls", Input: `{}`}}},
		message.Message{ID: "result-2", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "next", Content: "other.go"}}},
	)
	compacted, count := compactCompletedBatches(history)
	require.Positive(t, count)
	require.Contains(t, compacted[1].Content().Text, `"source_read"`)
	require.Contains(t, compacted[1].Content().Text, `"start_line":1`)
	require.Contains(t, compacted[1].Content().Text, `"message_id":"read-result"`)
	run := &generationRun{requiresCitation: true, turnMessageID: "current"}
	require.Empty(t, missingSourceCitation(run, history, "source.go:2"), "the original native receipt remains usable while its hash is current")
	receipts, _ := sourceReadReceipts(run, compacted)
	require.Empty(t, receipts, "an assistant continuity summary is not a native receipt")
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("changed"), 0600))
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:2"))
}

func TestCitationPathsWithSpacesAndRemoteLinks(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "my source.go"), []byte("package source\n// inspected\n"), 0600))
	history, _ := citationHistory(t, "read_lines", `{"file_path":"my source.go"}`)
	run := &generationRun{requiresCitation: true, turnMessageID: "current"}
	require.Empty(t, missingSourceCitation(run, history, "[source](my source.go:2) and https://example.com/unread.go:80"))
	require.Empty(t, missingSourceCitation(run, history, "Em `my source.go:2`, está o trecho inspecionado."))
	require.Len(t, sourceReferences("At 12:30, see https://example.com/remote.go:80 and [remote](https://example.com/x.go:3)."), 0)
}

func TestCitationRangesRequireContiguousPagesAndCannotInventReads(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n// two\n// three\n// four\n"), 0600))
	first, _ := citationHistory(t, "read_lines", `{"file_path":"source.go","offset":0,"limit":1}`)
	last, _ := citationHistory(t, "read_lines", `{"file_path":"source.go","offset":2,"limit":1}`)
	// Use independent call IDs, as real sequential native reads do.
	lastCall := last[1].ToolCalls()[0]
	lastCall.ID = "last"
	last[1].Parts = []message.ContentPart{lastCall}
	lastResult := last[2].ToolResults()[0]
	lastResult.ToolCallID = "last"
	last[2].Parts = []message.ContentPart{lastResult}
	history := append(first, last[1:]...)
	run := &generationRun{requiresCitation: true, turnMessageID: "current"}
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:1-3"), "unread line two cannot be filled by merging endpoints")
	middle, _ := citationHistory(t, "read_lines", `{"file_path":"source.go","offset":1,"limit":1}`)
	middleCall := middle[1].ToolCalls()[0]
	middleCall.ID = "middle"
	middle[1].Parts = []message.ContentPart{middleCall}
	middleResult := middle[2].ToolResults()[0]
	middleResult.ToolCallID = "middle"
	middle[2].Parts = []message.ContentPart{middleResult}
	history = append(history, middle[1:]...)
	require.Empty(t, missingSourceCitation(run, history, "source.go:1-3"))
	require.NotEmpty(t, missingSourceCitation(run, []message.Message{{ID: "current", Role: message.User}}, "invented.go:12"), "a file citation without a read must not pass")
}
