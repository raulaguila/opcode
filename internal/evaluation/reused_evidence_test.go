package evaluation

import (
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReusedEvidenceIsNotAnExecutedToolInEvaluation(t *testing.T) {
	calls, count := observedToolCalls([]message.Message{{Role: message.Assistant, Parts: []message.ContentPart{
		message.ToolCall{Name: "view", Origin: "context-reused"},
		message.ToolCall{Name: "search_code_index", Origin: "context"},
	}}})
	require.False(t, calls["view"])
	require.True(t, calls["search_code_index"])
	require.Equal(t, 1, count)
}
