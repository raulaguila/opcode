package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpcodeIdentityIsBoundedAndDoesNotTrustCommentsOrLinks(t *testing.T) {
	root := productivityWorkspace(t)
	path := filepath.Join(root, "go.mod")
	for _, test := range []struct {
		source string
		want   bool
	}{
		{"module github.com/raulaguila/opcode\n\ngo 1.26\n", true},
		{"// module wrong\nmodule \"github.com/raulaguila/opcode\" // comment\n", true},
		{"/*\nmodule github.com/raulaguila/opcode\n*/\nmodule example.test/other\n", false},
		{"module github.com/raulaguila/opcode-fake\n", false},
		{strings.Repeat(" ", 16<<10) + "module github.com/raulaguila/opcode", false},
	} {
		require.NoError(t, os.WriteFile(path, []byte(test.source), 0600))
		require.Equal(t, test.want, OpcodeWorkspace(context.Background()))
	}
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "manifest"), path))
	require.False(t, OpcodeWorkspace(context.Background()))
}

func TestLanguageSummarySeparatesProfilesFromDetectedFileTypes(t *testing.T) {
	productivityWorkspace(t)
	tool := NewLanguageSupportTool()
	response, err := tool.Run(toolContext(), ToolCall{Input: `{"summary":true,"limit":3}`})
	require.NoError(t, err)
	require.False(t, response.IsError)
	var summary struct {
		Count int      `json:"profile_count"`
		Names []string `json:"supported_languages"`
		More  bool
		Next  int `json:"next_offset"`
	}
	require.NoError(t, json.Unmarshal([]byte(response.Content), &summary))
	require.Equal(t, 42, summary.Count)
	require.Len(t, summary.Names, 3)
	require.True(t, summary.More)
	require.Equal(t, 3, summary.Next)
	require.Less(t, len(response.Content), 1000)
	response, err = tool.Run(toolContext(), ToolCall{Input: `{"summary":true,"detect_project":true}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	ctx, cancel := context.WithCancel(toolContext())
	cancel()
	_, err = tool.Run(ctx, ToolCall{Input: `{"summary":true}`})
	require.ErrorIs(t, err, context.Canceled)
}
