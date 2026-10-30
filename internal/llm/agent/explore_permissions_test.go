package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestExplorationDispatchRetainsFactoryPermissionWrapper(t *testing.T) {
	_, _, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("private-fixture-source"), 0600))
	tool := newExploreTool(nil, nil).(*exploreTool)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "s")
	result := tool.runSourceCall(ctx, message.ToolCall{Name: "view", Input: `{"file_path":".env"}`})
	require.True(t, result.IsError)
	require.NotContains(t, result.Content, "private-fixture-source")
	result = tool.runSourceCall(ctx, message.ToolCall{Name: "bash", Input: `{"command":"touch forbidden"}`})
	require.True(t, result.IsError)
	require.NoFileExists(t, filepath.Join(root, "forbidden"))
}
