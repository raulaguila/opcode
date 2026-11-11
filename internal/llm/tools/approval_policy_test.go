package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func nativePolicyFixture(t *testing.T) (string, permission.Service) {
	t.Helper()
	root, err := os.MkdirTemp(config.WorkingDirectory(), "project-edits-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	permissions := permission.NewPermissionService()
	require.NoError(t, permissions.(permission.ApprovalPolicies).SetApprovalPolicy("test-session", permission.ApprovalSandboxed, root))
	return root, permissions
}

func runPolicyTool(t *testing.T, tool BaseTool, params any) (ToolResponse, error) {
	t.Helper()
	input, err := json.Marshal(params)
	require.NoError(t, err)
	return tool.Run(toolContext(), ToolCall{Input: string(input)})
}

func TestProjectEditPolicyAllowsNativeCreationAndUpdates(t *testing.T) {
	for _, name := range []string{"write", "edit", "patch"} {
		t.Run(name, func(t *testing.T) {
			root, permissions := nativePolicyFixture(t)
			path := filepath.Join(root, "nested", "file.txt")
			policyFileChange(t, name, permissions, path, false)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, expectedPolicyContent(name, "before"), string(data))
			policyFileChange(t, name, permissions, path, true)
			data, err = os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, expectedPolicyContent(name, "after"), string(data))
		})
	}
}

func expectedPolicyContent(tool, text string) string {
	if tool == "patch" {
		return text
	}
	return text + "\n"
}

func policyFileChange(t *testing.T, name string, permissions permission.Service, path string, existing bool) {
	t.Helper()
	var tool BaseTool
	var params any
	switch name {
	case "write":
		tool = NewWriteTool(nil, permissions)
		content := "before\n"
		if existing {
			content = "after\n"
		}
		params = WriteParams{FilePath: path, Content: content}
	case "edit":
		tool = NewEditTool(nil, permissions)
		edit := EditParams{FilePath: path, NewString: "before\n"}
		if existing {
			edit.OldString, edit.NewString = "before\n", "after\n"
		}
		params = edit
	case "patch":
		tool = NewPatchTool(nil, permissions)
		patch := fmt.Sprintf("*** Begin Patch\n*** Add File: %s\n+before\n*** End Patch", path)
		if existing {
			patch = fmt.Sprintf("*** Begin Patch\n*** Update File: %s\n@@\n-before\n+after\n*** End Patch", path)
		}
		params = PatchParams{PatchText: patch}
	}
	if existing {
		recordFileRead(path)
	}
	response, err := runPolicyTool(t, SecureTool(tool, permissions), params)
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
}

func TestApprovalLevelFreesDeletionButStillAsksForMoves(t *testing.T) {
	for _, kind := range []string{"delete", "command"} {
		t.Run(kind, func(t *testing.T) {
			root, permissions := nativePolicyFixture(t)
			path := filepath.Join(root, "source.txt")
			require.NoError(t, os.WriteFile(path, []byte("before\n"), 0o600))
			recordFileRead(path)
			tool, params := destructivePolicyOperation(kind, permissions, path)
			_, err := runPolicyTool(t, SecureTool(tool, permissions), params)
			require.NoError(t, err, "work inside the project needs no prompt")
		})
	}
	for _, kind := range []string{"move"} {
		t.Run(kind, func(t *testing.T) {
			root, permissions := nativePolicyFixture(t)
			path := filepath.Join(root, "source.txt")
			require.NoError(t, os.WriteFile(path, []byte("before\n"), 0o600))
			recordFileRead(path)
			tool, params := destructivePolicyOperation(kind, permissions, path)
			_, err := runPolicyTool(t, SecureTool(tool, permissions), params)
			require.ErrorIs(t, err, permission.ErrorPermissionDenied)
			content, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "before\n", string(content))
			_, err = os.Stat(path + ".moved")
			require.True(t, os.IsNotExist(err))
		})
	}
}

func destructivePolicyOperation(kind string, permissions permission.Service, path string) (BaseTool, any) {
	if kind == "command" {
		return NewBashTool(permissions), BashParams{Command: "echo blocked"}
	}
	patch := fmt.Sprintf("*** Begin Patch\n*** Delete File: %s\n*** End Patch", path)
	if kind == "move" {
		patch = fmt.Sprintf("*** Begin Patch\n*** Update File: %s\n*** Move to: %s.moved\n@@\n-before\n+after\n*** End Patch", path, path)
	}
	return NewPatchTool(nil, permissions), PatchParams{PatchText: patch}
}

func TestProjectEditPolicyDoesNotBypassNativeReadBeforeWrite(t *testing.T) {
	root, permissions := nativePolicyFixture(t)
	path := filepath.Join(root, "unread.txt")
	require.NoError(t, os.WriteFile(path, []byte("before\n"), 0o600))
	response, err := runPolicyTool(t, NewWriteTool(nil, permissions), WriteParams{FilePath: path, Content: "after\n"})
	require.NoError(t, err)
	require.True(t, response.IsError)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "before\n", string(content))
}
