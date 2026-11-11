package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

// Live gemma4:12b on a 16K window wrote a 3.6K-token file and then failed with
// a context-budget error on the next step: the body rode along in the request.
func TestAppliedWriteBodyLeavesTheRequestButFailedOnesStay(t *testing.T) {
	body := strings.Repeat("    1,\n", 800)
	input, err := json.Marshal(map[string]string{"file_path": "numbers.py", "content": body})
	require.NoError(t, err)
	history := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Crie numbers.py"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{
			message.ToolCall{ID: "ok", Name: "write", Input: string(input)},
			message.ToolCall{ID: "bad", Name: "write", Input: string(input)},
		}},
		{Role: message.Tool, Parts: []message.ContentPart{
			message.ToolResult{ToolCallID: "ok", Content: "written"},
			message.ToolResult{ToolCallID: "bad", Content: "denied", IsError: true},
		}},
	}
	out, changed := compactAppliedEdits(history)
	require.Equal(t, 1, changed)
	calls := out[1].ToolCalls()
	var fields map[string]string
	require.NoError(t, json.Unmarshal([]byte(calls[0].Input), &fields), "the shortened input is still JSON")
	require.Equal(t, "numbers.py", fields["file_path"])
	require.Less(t, len(fields["content"]), 512)
	require.Contains(t, fields["content"], "already applied to disk")
	require.Equal(t, string(input), calls[1].Input, "a failed write keeps its body so the model can retry")
	require.Equal(t, string(input), history[1].ToolCalls()[0].Input, "the saved conversation is not rewritten")
}
