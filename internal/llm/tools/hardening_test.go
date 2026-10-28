package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func testUnisolated(t *testing.T) {
	t.Helper()
	original := config.Get().Security
	config.Get().Security = config.SecurityConfig{Sandbox: "none"}
	t.Cleanup(func() { config.Get().Security = original })
}
func toolInput(value any) ToolCall {
	raw, _ := json.Marshal(value)
	return ToolCall{Input: string(raw)}
}
func workspaceTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(config.WorkingDirectory(), "tool-regression-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
func TestSensitiveAndExternalReadsRequirePermission(t *testing.T) {
	inside := workspaceTestDir(t)
	outside := t.TempDir()
	secret := filepath.Join(inside, ".env")
	external := filepath.Join(outside, "file.txt")
	normal := filepath.Join(inside, "normal.txt")
	for _, path := range []string{secret, external, normal} {
		require.NoError(t, os.WriteFile(path, []byte("sensitive-test-data"), 0600))
	}
	link := filepath.Join(inside, "external-link")
	require.NoError(t, os.Symlink(external, link))
	for _, path := range []string{secret, external, link} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			p := &denyingPermissions{}
			response, err := SecureTool(NewViewTool(nil), p).Run(toolContext(), toolInput(ViewParams{FilePath: path}))
			require.ErrorIs(t, err, permission.ErrorPermissionDenied)
			require.NotContains(t, response.Content, "sensitive-test-data")
			require.Len(t, p.requests, 1)
		})
	}
	p := &denyingPermissions{}
	response, err := SecureTool(NewViewTool(nil), p).Run(toolContext(), toolInput(ViewParams{FilePath: normal}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "sensitive-test-data")
	require.Empty(t, p.requests)
	p = &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	response, err = SecureTool(NewViewTool(nil), p).Run(toolContext(), toolInput(ViewParams{FilePath: external}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "sensitive-test-data")
	require.Len(t, p.requests, 1)
}
func TestLargeFileSlicesAndLongLines(t *testing.T) {
	dir := workspaceTestDir(t)
	path := filepath.Join(dir, "large.txt")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", 300000)+"\nsecond\nthird\n"), 0600))
	response, err := NewViewTool(nil).Run(toolContext(), toolInput(ViewParams{FilePath: path, Offset: 1, Limit: 1}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "second")
	require.NotContains(t, response.Content, "third")
	require.Contains(t, response.Content, "more lines")
	content, count, err := readTextFile(context.Background(), path, 2, 1)
	require.NoError(t, err)
	require.Equal(t, "third", content)
	require.Equal(t, 3, count)
	_, _, err = readTextFile(context.Background(), path, -1, 1)
	require.Error(t, err)
}
func TestAtomicWriteRejectsStaleApprovalAndPreservesMode(t *testing.T) {
	path := filepath.Join(workspaceTestDir(t), "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("before"), 0750))
	old := "before"
	p := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool {
		require.NoError(t, os.WriteFile(path, []byte("user-change"), 0750))
		return true
	}}
	recordFileRead(path)
	_, err := NewEditTool(nil, p, &fileHistory{}).Run(toolContext(), toolInput(EditParams{FilePath: path, OldString: "before", NewString: "after"}))
	require.ErrorContains(t, err, "changed since approval")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "user-change", string(raw))
	old = "user-change"
	require.NoError(t, atomicWriteFile(toolContext(), path, "after", &old))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0750), info.Mode().Perm())
}
func TestMultiFileChangePrevalidationLeavesAllOriginals(t *testing.T) {
	dir := workspaceTestDir(t)
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	old, next := "before", "after"
	require.NoError(t, os.WriteFile(a, []byte(old), 0600))
	require.NoError(t, os.WriteFile(b, []byte("user-change"), 0600))
	err := applyFiles(toolContext(), map[string]fileChange{a: {old: &old, next: &next}, b: {old: &old, next: &next}})
	require.Error(t, err)
	raw, err := os.ReadFile(a)
	require.NoError(t, err)
	require.Equal(t, old, string(raw))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
}
func TestBashStructuredFailureAndEnvironmentIsolation(t *testing.T) {
	testUnisolated(t)
	t.Setenv("OPENAI_API_KEY", "never-forward-this-secret")
	p := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	response, err := NewBashTool(p).Run(toolContext(), toolInput(BashParams{Command: "printf '%s' \"${OPENAI_API_KEY-unset}\"; exit 7"}))
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "unset")
	require.NotContains(t, response.Content, "never-forward-this-secret")
	var metadata BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Metadata), &metadata))
	require.Equal(t, 7, metadata.ExitCode)
	response, err = NewBashTool(p).Run(toolContext(), toolInput(BashParams{Command: "sleep 60", Timeout: 50}))
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.NoError(t, json.Unmarshal([]byte(response.Metadata), &metadata))
	require.True(t, metadata.TimedOut)
	first := shell.GetPersistentShell(config.WorkingDirectory())
	_, _, _, _, err = first.Exec(toolContext(), "export OPCODE_STATE_PROBE=changed; cd /", 1000)
	require.NoError(t, err)
	stdout, _, _, _, err := first.Exec(toolContext(), "printf '%s|%s' \"${OPCODE_STATE_PROBE-unset}\" \"$PWD\"", 1000)
	require.NoError(t, err)
	require.Equal(t, "unset|"+config.WorkingDirectory(), stdout)
}
func TestSandboxFailureNeverRunsCommandOutsideSandbox(t *testing.T) {
	original := config.Get().Security
	config.Get().Security = config.SecurityConfig{Sandbox: "bubblewrap"}
	t.Cleanup(func() { config.Get().Security = original })
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bwrap"), []byte("#!/bin/sh\nexit 71\n"), 0700))
	t.Setenv("PATH", dir)
	p := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	response, err := NewBashTool(p).Run(toolContext(), toolInput(BashParams{Command: "printf SHOULD_NEVER_RUN"}))
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.NotContains(t, response.Content, "SHOULD_NEVER_RUN")
}
func TestSearchLiteralOptionsColonPathsAndLimits(t *testing.T) {
	testUnisolated(t)
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep contract")
	}
	dir := workspaceTestDir(t)
	path := filepath.Join(dir, "name:with-colon.txt")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("--literal[\n", 105)), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "integration.json"), []byte("--literal["), 0600))
	matches, truncated, err := searchFiles(toolContext(), regexpQuote("--literal["), dir, "*.txt", 100)
	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, matches, 100)
	require.Equal(t, path, matches[0].path)
	require.Equal(t, 1, matches[0].lineNum)
	ctx, cancel := context.WithCancel(toolContext())
	cancel()
	_, _, err = searchFiles(ctx, "literal", dir, "", 100)
	require.Error(t, err)
	files, _, err := findFiles(toolContext(), dir, "*", 100)
	require.NoError(t, err)
	require.Equal(t, []string{path}, files)
}
func regexpQuote(value string) string { return escapeRegexPattern(value) }
func TestNetworkPolicyAndBoundedBodies(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::1", "10.0.0.1", "169.254.169.254", "::", "224.0.0.1"} {
		require.False(t, networkAddressAllowed(net.ParseIP(address), false), address)
	}
	require.True(t, networkAddressAllowed(net.ParseIP("127.0.0.1"), true))
	require.False(t, networkAddressAllowed(net.ParseIP("169.254.169.254"), true))
	require.True(t, networkAddressAllowed(net.ParseIP("8.8.8.8"), false))
	_, err := boundedBody(strings.NewReader("1234"), 3)
	require.Error(t, err)
	p := &denyingPermissions{}
	request, err := http.NewRequest("GET", "https://example.test/path?token=not-in-permission-scope", nil)
	require.NoError(t, err)
	_, err = networkDo(toolContext(), p, "fetch", request, time.Second)
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.Equal(t, "network:https://example.test", p.requests[0].Path)
	require.NotContains(t, p.requests[0].Description, "token")
}
func TestRedirectNeedsDestinationPermission(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("loopback listeners unavailable in this sandbox")
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:1/never-contact", http.StatusFound)
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()
	original := config.Get().Security
	config.Get().Security.AllowPrivateNetwork = true
	t.Cleanup(func() { config.Get().Security = original })
	p := &denyingPermissions{allow: func(opts permission.CreatePermissionRequest) bool { return !strings.Contains(opts.Path, "localhost") }}
	request, err := http.NewRequest("GET", server.URL, nil)
	require.NoError(t, err)
	_, err = networkDo(toolContext(), p, "fetch", request, time.Second)
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.Len(t, p.requests, 2)
}
func TestProcessOwnershipCancellationAndChecks(t *testing.T) {
	testUnisolated(t)
	manager := NewProcessManager()
	defer manager.Close()
	parent, cancel := context.WithCancel(toolContext())
	record, err := manager.start(parent, "test-session", "printf started; sleep 60", time.Minute)
	require.NoError(t, err)
	cancel()
	p := &processTool{name: "process_status", manager: manager}
	other := context.WithValue(toolContext(), SessionIDContextKey, "other-session")
	response, err := p.Run(other, toolInput(map[string]string{"id": record.id}))
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Eventually(t, func() bool { out, _ := record.stdout.Snapshot(); return out == "started" }, time.Second, 10*time.Millisecond)
	manager.CancelSession("test-session")
	select {
	case <-record.done:
	case <-time.After(3 * time.Second):
		t.Fatal("process did not stop")
	}
	require.True(t, manager.snapshot(record).IsError)
	checks := config.Get().Checks
	config.Get().Checks = map[string]config.CheckConfig{"ok": {Command: "printf checked"}, "fails": {Command: "exit 3"}}
	t.Cleanup(func() { config.Get().Checks = checks })
	permission := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	response, err = NewChecksTool(permission).Run(toolContext(), toolInput(map[string]any{"names": []string{"ok", "fails"}}))
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, `"exit_code":3`)
	require.Len(t, permission.requests, 2)
}
func TestRedactionKeepsWrappedErrorsAndEscapedSecretsPrivate(t *testing.T) {
	secret := "secret-with-\"quote"
	t.Setenv("TEST_API_KEY", secret)
	raw, _ := json.Marshal(map[string]string{"key": secret})
	require.NotContains(t, redact(string(raw)), strings.Trim(string(raw), "{}"))
	err := &toolError{name: "test", cause: fmt.Errorf("%s: %w", secret, permission.ErrorPermissionDenied)}
	require.True(t, errors.Is(err, permission.ErrorPermissionDenied))
	require.NotContains(t, err.Error(), secret)
}

func TestBubblewrapIsolationOnSupportedKernel(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("Bubblewrap not installed; fail-closed behavior has a separate test")
	}
	original := config.Get().Security
	config.Get().Security = config.SecurityConfig{Sandbox: "bubblewrap"}
	t.Cleanup(func() { config.Get().Security = original })
	probe, err := shell.Command(toolContext(), config.WorkingDirectory(), "true")
	require.NoError(t, err)
	output, err := probe.CombinedOutput()
	if err != nil && (strings.Contains(string(output), "Creating new namespace failed") || strings.Contains(string(output), "No permissions to create a new namespace")) {
		t.Skip("kernel or execution environment does not permit unprivileged namespaces")
	}
	require.NoError(t, err, string(output))
	dir := workspaceTestDir(t)
	sensitive := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(sensitive, []byte("PRIVATE_WORKSPACE_SECRET"), 0600))
	outside := filepath.Join(t.TempDir(), "outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte("OUTSIDE_WORKSPACE_SECRET"), 0600))
	t.Setenv("TEST_API_KEY", "PRIVATE_ENV_SECRET")
	nested := filepath.Join(dir, "node_modules", "package", ".env")
	require.NoError(t, os.MkdirAll(filepath.Dir(nested), 0755))
	require.NoError(t, os.WriteFile(nested, []byte("NESTED_PRIVATE_SECRET"), 0600))
	command := "printf '%s' \"${TEST_API_KEY-unset}\"; cat " + quoteArg(sensitive) + "; if [ -e " + quoteArg(outside) + " ]; then cat " + quoteArg(outside) + "; fi"
	command += "; cat " + quoteArg(nested) + " 2>/dev/null || true"
	probe, err = shell.Command(toolContext(), config.WorkingDirectory(), command)
	require.NoError(t, err)
	output, err = probe.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "unset")
	require.NotContains(t, string(output), "PRIVATE_WORKSPACE_SECRET")
	require.NotContains(t, string(output), "OUTSIDE_WORKSPACE_SECRET")
	require.NotContains(t, string(output), "NESTED_PRIVATE_SECRET")
}

type cancelAfterInstall struct {
	context.Context
	path      string
	cancelled bool
}

func (c *cancelAfterInstall) Err() error {
	if c.cancelled {
		return context.Canceled
	}
	raw, _ := os.ReadFile(c.path)
	if string(raw) == "after" {
		c.cancelled = true
		return context.Canceled
	}
	return nil
}
func TestInterruptedCommitRestoresAlreadyInstalledFiles(t *testing.T) {
	dir := workspaceTestDir(t)
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	old, next := "before", "after"
	require.NoError(t, os.WriteFile(a, []byte(old), 0640))
	require.NoError(t, os.WriteFile(b, []byte(old), 0600))
	ctx := &cancelAfterInstall{Context: context.Background(), path: a}
	err := applyFiles(ctx, map[string]fileChange{a: {old: &old, next: &next}, b: {old: &old, next: &next}})
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, ctx.cancelled)
	for _, path := range []string{a, b} {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, old, string(raw))
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
}
func TestWriteSymlinkRequiresDestinationApproval(t *testing.T) {
	dir := workspaceTestDir(t)
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")
	require.NoError(t, os.WriteFile(target, []byte("before"), 0600))
	require.NoError(t, os.Symlink(target, link))
	recordFileRead(link)
	p := &denyingPermissions{allow: func(opts permission.CreatePermissionRequest) bool { return opts.Action == "write" }}
	_, err := SecureTool(NewWriteTool(nil, p, &fileHistory{}), p).Run(toolContext(), toolInput(WriteParams{FilePath: link, Content: "after"}))
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	raw, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "before", string(raw))
	require.Equal(t, "write_symlink", p.requests[len(p.requests)-1].Action)
}
func TestBackgroundStartPermissionAndExpiry(t *testing.T) {
	testUnisolated(t)
	manager := NewProcessManager()
	defer manager.Close()
	permissions := &denyingPermissions{}
	tool := &processTool{name: "process_start", manager: manager, permissions: permissions}
	_, err := tool.Run(toolContext(), toolInput(map[string]any{"command": "sleep 60", "timeout": 50}))
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.Empty(t, manager.processes)
	permissions.allow = func(permission.CreatePermissionRequest) bool { return true }
	response, err := tool.Run(toolContext(), toolInput(map[string]any{"command": "sleep 60", "timeout": 50}))
	require.NoError(t, err)
	var result struct{ ID string }
	require.NoError(t, json.Unmarshal([]byte(response.Content), &result))
	record := manager.processes[result.ID]
	select {
	case <-record.done:
	case <-time.After(3 * time.Second):
		t.Fatal("process lifetime not enforced")
	}
	response = manager.snapshot(record)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, `"timed_out":true`)
}
