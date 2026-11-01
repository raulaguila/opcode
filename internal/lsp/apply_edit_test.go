package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/lsp/protocol"
	"github.com/stretchr/testify/require"
)

func TestHandleApplyEditNeverTouchesFiles(t *testing.T) {
	target := filepath.Join(t.TempDir(), "victim")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))
	params, err := json.Marshal(map[string]any{"edit": map[string]any{"documentChanges": []any{map[string]any{"kind": "delete", "uri": "file://" + target}}}})
	require.NoError(t, err)

	result, err := HandleApplyEdit(params)
	require.NoError(t, err)
	require.False(t, result.(protocol.ApplyWorkspaceEditResult).Applied)
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "keep", string(content))
}
