package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/lsp/protocol"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func productivityWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	old := config.Get().WorkingDir
	config.Get().WorkingDir = root
	t.Cleanup(func() { config.Get().WorkingDir = old })
	return root
}
func TestProjectMapOutlineAndSymbolPage(t *testing.T) {
	root := productivityWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\ntype Thing struct { Value string }\nfunc (t *Thing) Run() string {\n return t.Value\n}\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("PRIVATE=hide"), 0600))
	require.NoError(t, os.Symlink(".env", filepath.Join(root, "link")))
	mapTool := SecureTool(&projectTool{name: "get_project_map"}, &denyingPermissions{})
	response, err := mapTool.Run(toolContext(), toolInput(map[string]any{"limit": 1}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "main.go")
	require.NotContains(t, response.Content, ".env")
	require.NotContains(t, response.Content, "\"path\":\"link\"")
	response, err = SecureTool(&projectTool{name: "get_file_outline"}, nil).Run(toolContext(), toolInput(map[string]any{"file_path": "main.go"}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "Thing.Run")
	response, err = SecureTool(&projectTool{name: "read_symbol"}, nil).Run(toolContext(), toolInput(map[string]any{"file_path": "main.go", "symbol": "Thing.Run", "limit": 1}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "func (t *Thing) Run")
	require.Contains(t, response.Content, "\"more\":true")
	permissions := &denyingPermissions{}
	_, err = SecureTool(&projectTool{name: "get_file_outline"}, permissions).Run(toolContext(), toolInput(map[string]any{"file_path": ".env"}))
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.Len(t, permissions.requests, 1)
}
func TestTransactionObserverFailureRollsBackAndConflicts(t *testing.T) {
	root := productivityWorkspace(t)
	path := filepath.Join(root, "file.txt")
	before, after := "before", "after"
	require.NoError(t, os.WriteFile(path, []byte(before), 0600))
	called := false
	ctx := WithChangeObserver(toolContext(), func(changes []FileChange) error {
		called = true
		require.Equal(t, before, *changes[0].Before)
		require.Equal(t, after, *changes[0].After)
		return fmt.Errorf("storage unavailable")
	})
	err := ApplyFileChanges(ctx, []FileChange{{path, &before, &after}})
	require.ErrorContains(t, err, "storage unavailable")
	require.True(t, called)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, string(raw))
	wrong := "outdated"
	require.Error(t, ApplyFileChanges(toolContext(), []FileChange{{path, &wrong, &after}}))
	require.Error(t, ApplyFileChanges(toolContext(), []FileChange{{path, &before, &after}, {path, &before, &after}}))
}
func TestDiscoverChecksNeverRunsAndValidationFreshness(t *testing.T) {
	root := productivityWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"test":"touch should-not-exist","lint":"eslint ."}}`), 0600))
	response, err := (&validationTool{"discover_checks"}).Run(toolContext(), toolInput(map[string]any{}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "npm run test")
	require.Contains(t, response.Content, "\"executed\":false")
	_, err = os.Stat(filepath.Join(root, "should-not-exist"))
	require.True(t, os.IsNotExist(err))
	store := workflow.New(nil, root, workflow.Execute)
	ctx := WithWorkflowStore(toolContext(), store)
	response, err = (&validationTool{"get_validation_report"}).Run(ctx, toolInput(map[string]any{}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "task_revision")
	require.Contains(t, response.Content, "receipts")
}
func TestUTF16EditsAndUntrustedWorkspaceOperations(t *testing.T) {
	text := "hello 😀 name\r\nsecond\n"
	edit := protocol.TextEdit{Range: protocol.Range{Start: protocol.Position{Line: 0, Character: 9}, End: protocol.Position{Line: 0, Character: 13}}, NewText: "world"}
	next, err := applyUTF16Edits(text, []protocol.TextEdit{edit})
	require.NoError(t, err)
	require.Equal(t, "hello 😀 world\r\nsecond\n", next)
	edit.Range.Start.Character = 7
	_, err = applyUTF16Edits(text, []protocol.TextEdit{edit})
	require.ErrorContains(t, err, "surrogate")
	edit.Range.Start.Character = 9
	_, err = applyUTF16Edits(text, []protocol.TextEdit{edit, edit})
	require.ErrorContains(t, err, "overlapping")
	_, err = workspaceTextChanges(context.Background(), []byte(`{"documentChanges":[{"kind":"delete","uri":"file:///tmp/file"}]}`))
	require.ErrorContains(t, err, "resource operations")
	_, err = localURI("file://other-host/tmp/file")
	require.Error(t, err)
}

type advancedLSPStub struct{ raw map[string]json.RawMessage }

func (s *advancedLSPStub) OpenFile(context.Context, string) error { return nil }
func (s *advancedLSPStub) Call(_ context.Context, method string, _ any, out any) error {
	raw, ok := s.raw[method]
	if !ok {
		return fmt.Errorf("unsupported")
	}
	*out.(*json.RawMessage) = raw
	return nil
}
func TestLSPRenameApprovalAndAtomicCommit(t *testing.T) {
	root := productivityWorkspace(t)
	path := filepath.Join(root, "main.go")
	before := "package main\nvar old = 1\n"
	require.NoError(t, os.WriteFile(path, []byte(before), 0600))
	edit := fmt.Sprintf(`{"changes":{%q:[{"range":{"start":{"line":1,"character":4},"end":{"line":1,"character":7}},"newText":"new"}]}}`, string(protocol.URIFromPath(path)))
	client := &advancedLSPStub{map[string]json.RawMessage{"textDocument/rename": json.RawMessage(edit)}}
	params := advancedNavigationParams{NewName: "new"}
	denied := &denyingPermissions{}
	_, err := advancedNavigate(toolContext(), client, "lsp_rename", path, params, denied)
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	raw, _ := os.ReadFile(path)
	require.Equal(t, before, string(raw))
	approved := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	response, err := advancedNavigate(toolContext(), client, "lsp_rename", path, params, approved)
	require.NoError(t, err)
	require.Contains(t, response.Content, "changed_files")
	raw, _ = os.ReadFile(path)
	require.Equal(t, strings.Replace(before, "old", "new", 1), string(raw))
	require.NotEmpty(t, approved.requests[0].Params)
}

type outputSnapshotStub struct {
	calls   int
	content string
}

func (s *outputSnapshotStub) Info() ToolInfo { return ToolInfo{Name: "output_stub"} }
func (s *outputSnapshotStub) Run(context.Context, ToolCall) (ToolResponse, error) {
	s.calls++
	return NewTextResponse(s.content), nil
}
func TestLargeOutputArtifactRedactedAndDoesNotRepeatTool(t *testing.T) {
	root := productivityWorkspace(t)
	previous := config.Get().Data.Directory
	config.Get().Data.Directory = filepath.Join(root, "private")
	t.Cleanup(func() { config.Get().Data.Directory = previous })
	secret := "integration-secret-0123456789"
	t.Setenv("PRODUCTIVITY_API_KEY", secret)
	stub := &outputSnapshotStub{content: strings.Repeat("output line\n", 5000) + secret + " tail-marker"}
	response, err := SecureTool(stub, nil).Run(toolContext(), ToolCall{Input: "{}"})
	require.NoError(t, err)
	require.NotContains(t, response.Content, secret)
	require.Contains(t, response.Content, "Output saved as artifact")
	require.Equal(t, 1, stub.calls)
	lines := strings.SplitN(response.Content, "\n", 3)
	require.Len(t, lines, 3)
	var info struct{ ID string }
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &info))
	require.NotEmpty(t, info.ID)
	response, err = (&artifactTool{"search_artifact"}).Run(toolContext(), toolInput(map[string]any{"artifact_id": info.ID, "query": "tail-marker"}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "tail-marker")
	require.NotContains(t, response.Content, secret)
	require.Equal(t, 1, stub.calls)
}
func TestUserPreviewIsProtectedAndDoesNotMarkFileRead(t *testing.T) {
	root := productivityWorkspace(t)
	path := filepath.Join(root, "preview.go")
	require.NoError(t, os.WriteFile(path, []byte("package main\n"), 0600))
	require.True(t, getLastReadTime(path).IsZero())
	response, err := SecureTool(NewPreviewTool(), nil).Run(toolContext(), toolInput(map[string]any{"file_path": path}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "package main")
	require.True(t, getLastReadTime(path).IsZero())
}

func TestLSPHierarchyLimitsAndSymbolReplacement(t *testing.T) {
	root := productivityWorkspace(t)
	path := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(path, []byte("package main\nvar value = 1\n"), 0600))
	calls := json.RawMessage("[" + strings.TrimSuffix(strings.Repeat(`{"from":{"name":"caller"},"fromRanges":[]},`, 125), ",") + "]")
	client := &advancedLSPStub{raw: map[string]json.RawMessage{"textDocument/prepareCallHierarchy": json.RawMessage(`[{"name":"value"}]`), "callHierarchy/incomingCalls": calls, "textDocument/documentSymbol": json.RawMessage(`[{"name":"value","range":{"start":{"line":1,"character":0},"end":{"line":1,"character":13}}}]`)}}
	response, err := advancedNavigate(toolContext(), client, "lsp_call_hierarchy", path, advancedNavigationParams{Direction: "incoming"}, nil)
	require.NoError(t, err)
	var hierarchy struct {
		Items     []any
		Truncated bool
	}
	require.NoError(t, json.Unmarshal([]byte(response.Content), &hierarchy))
	require.Len(t, hierarchy.Items, 100)
	require.True(t, hierarchy.Truncated)
	replacement := "var value = 2"
	denied := &denyingPermissions{}
	_, err = advancedNavigate(toolContext(), client, "lsp_replace_symbol", path, advancedNavigationParams{Line: 1, Character: 5, Content: &replacement}, denied)
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	approved := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	_, err = advancedNavigate(toolContext(), client, "lsp_replace_symbol", path, advancedNavigationParams{Line: 1, Character: 5, Content: &replacement}, approved)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "package main\nvar value = 2\n", string(raw))
}

func TestCustomPrivateStorageRequiresSensitiveReadApproval(t *testing.T) {
	root := productivityWorkspace(t)
	previous := config.Get().Data.Directory
	config.Get().Data.Directory = filepath.Join(root, "private-storage")
	t.Cleanup(func() { config.Get().Data.Directory = previous })
	require.NoError(t, os.MkdirAll(config.Get().Data.Directory, 0700))
	path := filepath.Join(config.Get().Data.Directory, "other-session.json")
	require.NoError(t, os.WriteFile(path, []byte("private history"), 0600))
	permissions := &denyingPermissions{}
	_, err := SecureTool(NewPreviewTool(), permissions).Run(toolContext(), toolInput(map[string]any{"file_path": path}))
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.Len(t, permissions.requests, 1)
	require.Equal(t, "read_sensitive", permissions.requests[0].Action)
}

func TestProfileCredentialsWithArbitraryEnvironmentNamesAreRedactedBeforeArtifacts(t *testing.T) {
	root := productivityWorkspace(t)
	cfg := config.Get()
	previousData, previousProfiles := cfg.Data.Directory, cfg.ProviderProfiles
	cfg.Data.Directory = filepath.Join(root, "private-storage")
	cfg.ProviderProfiles = map[string]config.ProviderProfile{"profile": {Provider: config.Provider{APIKey: "fixture-profile-credential-0123456789", APIKeyEnv: "CUSTOM_CREDENTIAL"}}}
	t.Cleanup(func() { cfg.Data.Directory = previousData; cfg.ProviderProfiles = previousProfiles })
	t.Setenv("CUSTOM_CREDENTIAL", "fixture-environment-credential-0123456789")
	stub := &outputSnapshotStub{content: strings.Repeat("recorded output\n", 2000) + cfg.ProviderProfiles["profile"].APIKey + " " + os.Getenv("CUSTOM_CREDENTIAL") + " tail"}
	response, err := SecureTool(stub, nil).Run(toolContext(), ToolCall{Input: "{}"})
	require.NoError(t, err)
	require.Contains(t, response.Content, "Output saved as artifact")
	require.NotContains(t, response.Content, cfg.ProviderProfiles["profile"].APIKey)
	require.NotContains(t, response.Content, os.Getenv("CUSTOM_CREDENTIAL"))
	files, err := os.ReadDir(filepath.Join(cfg.Data.Directory, "artifacts"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	raw, err := os.ReadFile(filepath.Join(cfg.Data.Directory, "artifacts", files[0].Name()))
	require.NoError(t, err)
	require.NotContains(t, string(raw), cfg.ProviderProfiles["profile"].APIKey)
	require.NotContains(t, string(raw), os.Getenv("CUSTOM_CREDENTIAL"))
	require.Contains(t, string(raw), "REDACTED")
}

func TestLegacyPrivatePathsRemainSensitiveAfterRename(t *testing.T) {
	root := productivityWorkspace(t)
	for _, name := range []string{".opencode.json", "opencode.json", ".opencode-integration.json", ".opcode.json", "opcode.json", ".opcode-integration.json", ".opencode/sessions/private.json", ".opcode/sessions/private.json"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte("private content"), 0600))
		permissions := &denyingPermissions{}
		_, err := SecureTool(NewPreviewTool(), permissions).Run(toolContext(), toolInput(map[string]any{"file_path": path}))
		require.ErrorIs(t, err, permission.ErrorPermissionDenied, name)
		require.Len(t, permissions.requests, 1)
		require.Equal(t, "read_sensitive", permissions.requests[0].Action)
	}
}
