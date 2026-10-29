package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestCitationReusesHistoricalNativeReadsWithoutRereading(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\nfunc hello() {\n println(\"hello\")\n}\n"), 0600))
	for _, test := range []struct{ name, input, ref string }{
		{"view", `{"file_path":"source.go","offset":1,"limit":2}`, "source.go:2-3"},
		{"read_lines", `{"file_path":"source.go","offset":1,"limit":2}`, "source.go:2-3"},
		{"read_symbol", `{"file_path":"source.go","symbol":"hello","offset":1,"limit":1}`, "source.go:3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			history, _ := citationHistory(t, test.name, test.input)
			history = append(history, message.Message{ID: "later", Role: message.User})
			run := &generationRun{requiresCitation: true, turnMessageID: "later"}
			require.Empty(t, missingSourceCitation(run, history, test.ref))
			require.Len(t, currentCitationChoices(run, history), 1)
			// Historical evidence may validate a reference, but must not impose
			// a new reference requirement on an otherwise unrelated answer.
			require.Empty(t, missingSourceCitation(run, history, "Obrigado!"))
			receipts, attempted := sourceReadReceipts(run, history)
			require.Empty(t, receipts)
			require.False(t, attempted)
		})
	}
}

func TestHistoricalCitationStillRequiresOriginalPairVisibleLinesAndSession(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n// inspected\n// unread\n"), 0600))
	original, _ := citationHistory(t, "read_lines", `{"file_path":"source.go","offset":1,"limit":1}`)
	for i := range original {
		original[i].SessionID = "same"
	}
	run := &generationRun{requiresCitation: true, turnMessageID: "later"}
	for _, test := range []struct {
		name   string
		change func([]message.Message)
	}{
		{"failed read", func(h []message.Message) {
			r := h[2].ToolResults()[0]
			r.IsError = true
			h[2].Parts = []message.ContentPart{r}
		}},
		{"missing receipt", func(h []message.Message) {
			r := h[2].ToolResults()[0]
			r.Metadata = ""
			h[2].Parts = []message.ContentPart{r}
		}},
		{"summary only", func(h []message.Message) {
			h[2].Role = message.Assistant
		}},
		{"unpaired result", func(h []message.Message) {
			r := h[2].ToolResults()[0]
			r.ToolCallID = "unknown"
			h[2].Parts = []message.ContentPart{r}
		}},
		{"user supplied call", func(h []message.Message) { h[1].Role = message.User }},
		{"different session", func(h []message.Message) { h[1].SessionID = "other"; h[2].SessionID = "other" }},
		{"call crosses human turn", func(h []message.Message) { h[1], h[0] = h[0], h[1] }},
		{"search prose", func(h []message.Message) {
			c := h[1].ToolCalls()[0]
			c.Name = "search_code_index"
			h[1].Parts = []message.ContentPart{c}
		}},
		{"wrong call path", func(h []message.Message) {
			c := h[1].ToolCalls()[0]
			c.Input = `{"file_path":"other.go"}`
			h[1].Parts = []message.ContentPart{c}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := append([]message.Message(nil), original...)
			test.change(h)
			h = append(h, message.Message{ID: "later", SessionID: "same", Role: message.User})
			require.NotEmpty(t, missingSourceCitation(run, h, "source.go:2"))
			require.Empty(t, currentCitationChoices(run, h))
		})
	}
	history := append(original, message.Message{ID: "later", SessionID: "same", Role: message.User})
	require.Empty(t, missingSourceCitation(run, history, "source.go:2"))
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:1-3"), "unread lines remain invalid")
	run.requiresProjectEvidence = true
	run.primaryEvidence = map[string]bool{normalizedToolPath("topic.go"): true}
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:2"), "historical evidence cannot bypass topic requirements")
}

func TestHistoricalCitationRevalidatesHashPolicyAndCancellation(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	path := filepath.Join(root, "source.go")
	raw := []byte("package source\n// inspected\n")
	require.NoError(t, os.WriteFile(path, raw, 0600))
	history, _ := citationHistory(t, "read_lines", `{"file_path":"source.go"}`)
	history = append(history, message.Message{ID: "later", Role: message.User})
	run := &generationRun{requiresCitation: true, turnMessageID: "later"}
	require.Empty(t, missingSourceCitation(run, history, "source.go:2"))
	require.NoError(t, os.WriteFile(path, []byte("package source\n// changed\n"), 0600))
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:2"))
	require.Empty(t, currentCitationChoices(run, history))
	require.NoError(t, os.Remove(path))
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:2"))
	outside := filepath.Join(t.TempDir(), "external.go")
	require.NoError(t, os.WriteFile(outside, raw, 0600))
	require.NoError(t, os.Symlink(outside, path))
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:2"), "a matching hash cannot authorize a symlink")
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.WriteFile(path, raw, 0600))
	require.Empty(t, missingSourceCitation(run, history, "source.go:2"))
	cfg := config.Get()
	dataDirectory := cfg.Data.Directory
	cfg.Data.Directory = root
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:2"), "a formerly readable path now under protected app data must fail closed")
	require.Empty(t, currentCitationChoices(run, history))
	cfg.Data.Directory = dataDirectory
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run.ctx = ctx
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:2"))
	require.Empty(t, currentCitationChoices(run, history))
}

func TestHistoricalCitationMergesOnlyContiguousSameSnapshotPages(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n// two\n// three\n"), 0600))
	history, _ := citationHistory(t, "read_lines", `{"file_path":"source.go","limit":1}`)
	history = append(history, message.Message{ID: "later", Role: message.User})
	last, _ := citationHistory(t, "read_lines", `{"file_path":"source.go","offset":2,"limit":1}`)
	history = append(history, last[1:]...)
	run := &generationRun{requiresCitation: true, turnMessageID: "later"}
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:1-3"))
	middle, _ := citationHistory(t, "read_lines", `{"file_path":"source.go","offset":1,"limit":1}`)
	history = append(history, middle[1:]...)
	require.Empty(t, missingSourceCitation(run, history, "source.go:1-3"))
	// Revalidation is local and cannot run tools or expand their permissions.
	receipts, _ := sourceReadReceipts(run, history)
	require.Len(t, receipts, 1)
	require.Equal(t, 2, receipts[0].StartLine)
	require.True(t, tools.SourceReadCurrent(context.Background(), receipts[0]))
}

func TestHistoricalCitationLookbackIsBoundedAndSummariesCannotRestoreReceipts(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n// inspected\n"), 0600))
	original, _ := citationHistory(t, "read_lines", `{"file_path":"source.go"}`)
	run := &generationRun{requiresCitation: true, turnMessageID: "later"}
	summary := message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Previously read source.go:1-2. " + original[2].ToolResults()[0].Metadata}}}
	history := []message.Message{original[0], summary, {ID: "later", Role: message.User}}
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:2"))
	require.Empty(t, currentCitationChoices(run, history))
	history = append([]message.Message(nil), original...)
	for range 256 {
		history = append(history, summary)
	}
	history = append(history, message.Message{ID: "later", Role: message.User})
	require.NotEmpty(t, missingSourceCitation(run, history, "source.go:2"), "an out-of-window source needs a fresh native read")
	require.Empty(t, currentCitationChoices(run, history))
	history = append(history, original[1:]...)
	require.Empty(t, missingSourceCitation(run, history, "source.go:2"), "the historical limit must not discard a new read")
}
