package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func gitReadOnlyFixture(t *testing.T, sandbox string) string {
	t.Helper()
	dir := t.TempDir()
	previousDirectory, previousSecurity := config.Get().WorkingDir, config.Get().Security
	config.Get().WorkingDir, config.Get().Security.Sandbox = dir, sandbox
	t.Cleanup(func() { config.Get().WorkingDir, config.Get().Security = previousDirectory, previousSecurity })
	gitFixtureCommand(t, dir, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("tracked.txt filter=audit.driver\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("original\n"), 0600))
	gitFixtureCommand(t, dir, "add", "--all")
	gitFixtureCommand(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("changed source\n"), 0600))
	return dir
}

func gitFixtureCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = shell.Environment()
	out, err := command.CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestGitInspectionDisablesCleanAndProcessFiltersBeforeReadingWorktree(t *testing.T) {
	for _, sandbox := range []string{"bubblewrap", "none"} {
		t.Run(sandbox, func(t *testing.T) {
			if sandbox == "bubblewrap" && runtime.GOOS != "linux" {
				t.Skip("Bubblewrap is Linux-only")
			}
			for _, driver := range []string{"clean", "process"} {
				t.Run(driver, func(t *testing.T) { assertGitFilterNeverExecutes(t, sandbox, driver) })
			}
		})
	}
}

func assertGitFilterNeverExecutes(t *testing.T, sandbox, driver string) {
	t.Helper()
	dir := gitReadOnlyFixture(t, sandbox)
	// Benign disposable markers expose execution. A process filter exits rather
	// than waiting for a Git protocol handshake, so a regression fails promptly.
	command := "sh -c 'printf marker > filter-marker; cat'"
	if driver == "process" {
		command = "sh -c 'printf marker > filter-marker; exit 1'"
	}
	gitFixtureCommand(t, dir, "config", "filter.audit.driver."+driver, command)
	gitFixtureCommand(t, dir, "config", "filter.audit.driver.required", "true")
	before, err := os.ReadFile(filepath.Join(dir, "tracked.txt"))
	require.NoError(t, err)
	permissions := &denyingPermissions{}
	for _, name := range []string{"git_status", "git_diff", "git_log"} {
		response, err := SecureTool(&gitTool{name: name}, permissions).Run(toolContext(), toolInput(map[string]any{}))
		require.NoError(t, err, name)
		require.False(t, response.IsError, response.Content)
		require.NoFileExists(t, filepath.Join(dir, "filter-marker"), name+" executed a repository filter")
	}
	after, err := os.ReadFile(filepath.Join(dir, "tracked.txt"))
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Empty(t, permissions.requests)
	require.Equal(t, sandbox, config.Get().Security.Sandbox, "an explicit sandbox policy must not be rewritten")
}

func TestGitInspectionMountCannotWriteWorkspace(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Bubblewrap is Linux-only")
	}
	dir := gitReadOnlyFixture(t, "bubblewrap")
	command, err := shell.GitCommand(context.Background(), dir, "printf marker > inspection-marker")
	require.NoError(t, err)
	output, err := command.CombinedOutput()
	require.Error(t, err, string(output))
	require.NoFileExists(t, filepath.Join(dir, "inspection-marker"))
}

func TestGitFilterOverridesDeduplicateDriverAndRejectInvalidKeys(t *testing.T) {
	overrides, err := disabledGitFilters("filter.a.b.process\x00filter.a.b.clean\x00filter.a.b.required\x00")
	require.NoError(t, err)
	require.Equal(t, []string{"-c", "filter.a.b.clean=", "-c", "filter.a.b.process=", "-c", "filter.a.b.required=false"}, overrides)
	_, err = disabledGitFilters("not-a-filter\x00")
	require.Error(t, err)
}

func TestApprovedManagedWorktreeStillCreatesUnderSandbox(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Bubblewrap is Linux-only")
	}
	dir := gitReadOnlyFixture(t, "bubblewrap")
	permissions := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	tool := SecureTool(&worktreeTool{permissions: permissions}, permissions)
	response, err := tool.Run(toolContext(), toolInput(map[string]any{"action": "create", "name": "approved"}))
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	path := filepath.Join(dir, ".opcode-worktrees", "approved")
	require.FileExists(t, filepath.Join(path, "tracked.txt"))
	require.Len(t, permissions.requests, 1, "the worktree mutation retains its own approval")
	rootContents, err := os.ReadFile(filepath.Join(dir, "tracked.txt"))
	require.NoError(t, err)
	require.Equal(t, "changed source\n", string(rootContents), "creation must preserve the original working tree")
}

func TestGitLogDoesNotRunConfiguredSignatureProgram(t *testing.T) {
	dir := gitReadOnlyFixture(t, "none")
	command := exec.Command("git", "-C", dir, "rev-parse", "HEAD^{tree}")
	command.Env = shell.Environment()
	tree, err := command.Output()
	require.NoError(t, err)
	// A synthetic signature is enough to make Git invoke a configured verifier;
	// no private keys, signing software or inherited user configuration are used.
	commit := "tree " + strings.TrimSpace(string(tree)) + "\nauthor Test <test@example.invalid> 1700000000 +0000\ncommitter Test <test@example.invalid> 1700000000 +0000\ngpgsig -----BEGIN PGP SIGNATURE-----\n fake\n -----END PGP SIGNATURE-----\n\nfixture\n"
	command = exec.Command("git", "-C", dir, "hash-object", "-w", "-t", "commit", "--stdin")
	command.Env, command.Stdin = shell.Environment(), strings.NewReader(commit)
	oid, err := command.Output()
	require.NoError(t, err)
	gitFixtureCommand(t, dir, "update-ref", "HEAD", strings.TrimSpace(string(oid)))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "verification-helper.sh"), []byte("#!/bin/sh\nprintf marker > verification-marker\nexit 1\n"), 0700))
	gitFixtureCommand(t, dir, "config", "gpg.program", "./verification-helper.sh")
	gitFixtureCommand(t, dir, "config", "log.showSignature", "true")
	permissions := &denyingPermissions{}
	response, err := SecureTool(&gitTool{name: "git_log"}, permissions).Run(toolContext(), toolInput(map[string]any{}))
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.Contains(t, response.Content, "fixture")
	require.NoFileExists(t, filepath.Join(dir, "verification-marker"))
	require.Empty(t, permissions.requests)
}

func TestGitInspectionFailsClosedForAmbiguousFilterKey(t *testing.T) {
	dir := gitReadOnlyFixture(t, "none")
	// '=' is legal in a Git subsection but ambiguous in a -c key=value override.
	gitFixtureCommand(t, dir, "config", "filter.a=b.clean", "sh -c 'printf marker > ambiguous-marker; cat'")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("tracked.txt filter=a=b\n"), 0600))
	response, err := SecureTool(&gitTool{name: "git_diff"}, &denyingPermissions{}).Run(toolContext(), toolInput(map[string]any{}))
	require.Error(t, err, response.Content)
	require.NoFileExists(t, filepath.Join(dir, "ambiguous-marker"))
}
