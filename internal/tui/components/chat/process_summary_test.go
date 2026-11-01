package chat

import (
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestProcessSummarySeparatesPreparationCommandsAndRepairs(t *testing.T) {
	messages := []message.Message{
		{Role: message.Assistant, Parts: []message.ContentPart{
			message.ToolCall{Origin: "context", Name: "view"},
			message.ToolCall{Origin: "context", Name: "search_code_index"},
			message.ToolCall{Origin: "context-reused", Name: "view"},
			message.ToolCall{Name: "bash"},
			message.ToolCall{ID: "context-spoof", Name: "get_task"},
		}},
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Origin: "application", Text: "repair"}}},
	}
	summary := processSummary(messages)
	for _, label := range []string{config.Text("leitura de contexto", "context read"), config.Text("busca de contexto", "context search"), config.Text("fonte reutilizada", "reused source"), config.Text("chamada de terminal", "terminal call"), config.Text("ferramenta", "tool call"), config.Text("reparo", "repair")} {
		require.Contains(t, summary, label)
	}
	require.NotContains(t, summary, "actions")
}

func TestProcessSummaryNamesFewDirectToolCalls(t *testing.T) {
	calls := []message.Message{{Role: message.Assistant, Parts: []message.ContentPart{
		message.ToolCall{ID: "1", Name: "edit", Input: `{"file_path":"/w/store.go","old_string":"a"}`},
		message.ToolCall{ID: "2", Name: "bash", Input: `{"command":"go test ./..."}`},
	}}}
	require.Equal(t, "edit store.go · bash go test ./...", processSummary(calls))
}
