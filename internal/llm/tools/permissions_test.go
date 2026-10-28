package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/history"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "opcode-tools-test-")
	if err != nil {
		panic(err)
	}
	configPath := filepath.Join(dir, ".opcode.json")
	if err := os.WriteFile(configPath, []byte(`{"shell":{"path":"/bin/bash","args":["--noprofile","--norc"]}}`), 0o600); err != nil {
		panic(err)
	}
	viper.SetConfigFile(configPath)
	if _, err := config.Load(dir, false); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type denyingPermissions struct {
	permission.Service
	requests []permission.CreatePermissionRequest
	allow    func(permission.CreatePermissionRequest) bool
}

func (p *denyingPermissions) Request(ctx context.Context, opts permission.CreatePermissionRequest) bool {
	p.requests = append(p.requests, opts)
	if p.allow != nil {
		return p.allow(opts)
	}
	return false
}

func toolContext() context.Context {
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")
	return context.WithValue(ctx, MessageIDContextKey, "test-message")
}

func TestBashRequiresPermission(t *testing.T) {
	for _, command := range []string{
		"echo permission-probe",
		"echo permission-probe && true",
		"git status --short",
		"go version",
		"echo denied > permission-probe.txt",
		"echo $(touch permission-probe.txt)",
		"git branch -D permission-probe",
		"go run ./permission-probe.go",
		"kill 12345",
	} {
		t.Run(command, func(t *testing.T) {
			permissions := &denyingPermissions{}
			input, err := json.Marshal(BashParams{Command: command})
			require.NoError(t, err)
			_, err = NewBashTool(permissions).Run(toolContext(), ToolCall{Input: string(input)})
			require.ErrorIs(t, err, permission.ErrorPermissionDenied)
			require.Len(t, permissions.requests, 1)
		})
	}
}

func TestDeniedFileCreationLeavesNoDirectories(t *testing.T) {
	for _, name := range []string{"write", "edit"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "denied", "nested", "file.txt")
			permissions := &denyingPermissions{}
			var tool BaseTool
			var params any
			if name == "write" {
				tool = NewWriteTool(nil, permissions, nil)
				params = WriteParams{FilePath: path, Content: "denied"}
			} else {
				tool = NewEditTool(nil, permissions, nil)
				params = EditParams{FilePath: path, NewString: "denied"}
			}
			input, err := json.Marshal(params)
			require.NoError(t, err)
			_, err = tool.Run(toolContext(), ToolCall{Input: string(input)})
			require.ErrorIs(t, err, permission.ErrorPermissionDenied)
			_, err = os.Stat(filepath.Dir(path))
			require.True(t, os.IsNotExist(err), "denied operation created directories: %v", err)
		})
	}
}

func TestFilePermissionUsesExactDirectory(t *testing.T) {
	root := config.WorkingDirectory()
	for _, dir := range []string{filepath.Join(root, "nested"), root + "-sibling"} {
		t.Run(dir, func(t *testing.T) {
			for _, name := range []string{"write", "edit"} {
				t.Run(name, func(t *testing.T) {
					permissions := &denyingPermissions{}
					path := filepath.Join(dir, "file.txt")
					var tool BaseTool
					var params any
					if name == "write" {
						tool = NewWriteTool(nil, permissions, nil)
						params = WriteParams{FilePath: path, Content: "denied"}
					} else {
						tool = NewEditTool(nil, permissions, nil)
						params = EditParams{FilePath: path, NewString: "denied"}
					}
					input, err := json.Marshal(params)
					require.NoError(t, err)
					_, err = tool.Run(toolContext(), ToolCall{Input: string(input)})
					require.ErrorIs(t, err, permission.ErrorPermissionDenied)
					require.Len(t, permissions.requests, 1)
					require.Equal(t, dir, permissions.requests[0].Path)
				})
			}
		})
	}
	t.Cleanup(func() { os.RemoveAll(root + "-sibling") })
}

func TestBashWhitespaceCommand(t *testing.T) {
	permissions := &denyingPermissions{}
	for _, command := range []string{"", " ", "\t\n"} {
		t.Run(fmt.Sprintf("%q", command), func(t *testing.T) {
			input, err := json.Marshal(BashParams{Command: command})
			require.NoError(t, err)
			response, err := NewBashTool(permissions).Run(toolContext(), ToolCall{Input: string(input)})
			require.NoError(t, err)
			require.True(t, response.IsError)
		})
	}
}

type fileHistory struct {
	history.Service
	lastVersion string
}

func (h *fileHistory) Create(_ context.Context, _, path, content string) (history.File, error) {
	return history.File{Path: path, Content: content}, nil
}

func (h *fileHistory) GetByPathAndSession(_ context.Context, path, _ string) (history.File, error) {
	return history.File{Path: path}, nil
}

func (h *fileHistory) CreateVersion(_ context.Context, _, path, content string) (history.File, error) {
	h.lastVersion = content
	return history.File{Path: path, Content: content}, nil
}

func TestApprovedFileChanges(t *testing.T) {
	for _, name := range []string{"write", "edit-create", "edit-replace", "edit-delete"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nested", "file.txt")
			permissions := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
			files := &fileHistory{}
			var tool BaseTool = NewEditTool(nil, permissions, files)
			var params any = EditParams{FilePath: path, NewString: "after"}
			if name == "write" {
				tool = NewWriteTool(nil, permissions, files)
				params = WriteParams{FilePath: path, Content: "after"}
			} else if name == "edit-replace" || name == "edit-delete" {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte("before after"), 0o600))
				recordFileRead(path)
				if name == "edit-delete" {
					params = EditParams{FilePath: path, OldString: "before "}
				} else {
					params = EditParams{FilePath: path, OldString: "before after", NewString: "after"}
				}
			}
			input, err := json.Marshal(params)
			require.NoError(t, err)
			response, err := tool.Run(toolContext(), ToolCall{Input: string(input)})
			require.NoError(t, err)
			require.False(t, response.IsError, response.Content)
			require.Len(t, permissions.requests, 1, "an edit must be applied only once")
			content, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "after", string(content))
			require.Equal(t, "after", files.lastVersion)
		})
	}
}

func TestPatchMoveRequiresDestinationApproval(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.txt")
	target := filepath.Join(dir, "destination", "target.txt")
	require.NoError(t, os.WriteFile(source, []byte("before\n"), 0o600))
	recordFileRead(source)
	permissions := &denyingPermissions{allow: func(opts permission.CreatePermissionRequest) bool {
		return opts.Action == "update"
	}}
	tool := NewPatchTool(nil, permissions, &fileHistory{})
	patch := fmt.Sprintf("*** Begin Patch\n*** Update File: %s\n*** Move to: %s\n@@\n-before\n+after\n*** End Patch", source, target)
	input, err := json.Marshal(PatchParams{PatchText: patch})
	require.NoError(t, err)
	_, err = tool.Run(toolContext(), ToolCall{Input: string(input)})
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	content, err := os.ReadFile(source)
	require.NoError(t, err)
	require.Equal(t, "before\n", string(content))
	_, err = os.Stat(filepath.Dir(target))
	require.True(t, os.IsNotExist(err))
}

func TestPatchMoveToSameFileDoesNotDeleteIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("before\n"), 0o600))
	recordFileRead(path)
	permissions := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	patch := fmt.Sprintf("*** Begin Patch\n*** Update File: %s\n*** Move to: %s\n@@\n-before\n+after\n*** End Patch", path, filepath.Join(filepath.Dir(path), ".", "file.txt"))
	input, err := json.Marshal(PatchParams{PatchText: patch})
	require.NoError(t, err)
	response, err := NewPatchTool(nil, permissions, &fileHistory{}).Run(toolContext(), ToolCall{Input: string(input)})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "after\n", string(content))
}
