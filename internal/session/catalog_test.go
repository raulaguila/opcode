package session

import (
	"bufio"
	"context"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/localstate"
)

func TestCatalogPersistsSessionsWithoutRestoringGrants(t *testing.T) {
	root := t.TempDir()
	store := NewCatalog(root, ".opcode")
	require.NoError(t, store.Activate("first"))
	require.NoError(t, store.SaveDraft("first", "unfinished prompt"))
	require.NoError(t, store.Archive("first", true))
	require.NoError(t, store.Activate("second"))
	entries, last, err := NewCatalog(root, ".opcode").Snapshot()
	require.NoError(t, err)
	require.Equal(t, "second", last)
	require.True(t, entries["first"].Archived)
	require.Equal(t, "unfinished prompt", entries["first"].Draft)
	info, err := os.Stat(filepath.Join(root, ".opcode", "session-preferences.json"))
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0077)
	require.Error(t, store.SaveDraft("first", strings.Repeat("x", 64001)))
	entries, _, err = store.Snapshot()
	require.NoError(t, err)
	require.Equal(t, "unfinished prompt", entries["first"].Draft)
}

func TestCatalogProcessWriter(t *testing.T) {
	if os.Getenv("OPCODE_CATALOG_CHILD") != "1" {
		return
	}
	root := os.Getenv("OPCODE_CATALOG_ROOT")
	// A pipe rendezvous lets the parent hold the actual kernel lock before we
	// attempt the complete read/change/write, without replacing storage code.
	os.Stdout.WriteString("ready\n")
	require.NoError(t, NewCatalog(root, ".opcode").SaveDraft("child", "second draft"))
}

func TestCatalogSerializesIndependentProcesses(t *testing.T) {
	root := t.TempDir()
	catalog := NewCatalog(root, ".opcode")
	unlock, err := localstate.LockFile(catalog.path)
	require.NoError(t, err)
	defer unlock()
	binary, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^TestCatalogProcessWriter$")
	child.Env = append(os.Environ(), "OPCODE_CATALOG_CHILD=1", "OPCODE_CATALOG_ROOT="+root)
	output, err := child.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, child.Start())
	defer child.Process.Kill()
	line, err := bufio.NewReader(output).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "ready\n", line)
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case err := <-done:
		t.Fatalf("child bypassed the held state lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	// Save the first process's document while retaining the transaction lock.
	require.NoError(t, localstate.Write(catalog.path, catalogDocument{Root: root, Sessions: map[string]LocalSession{"parent": {Archived: true, Draft: "first draft"}}}, 1<<20))
	unlock()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("child did not acquire released state lock")
	}
	entries, _, err := catalog.Snapshot()
	require.NoError(t, err)
	require.Equal(t, "first draft", entries["parent"].Draft)
	require.True(t, entries["parent"].Archived)
	require.Equal(t, "second draft", entries["child"].Draft)
}
func TestCatalogRefusesCorruptionAndWorkspaceMismatch(t *testing.T) {
	root := t.TempDir()
	data := t.TempDir()
	store := NewCatalog(root, data)
	require.NoError(t, store.Activate("s"))
	_, _, err := NewCatalog(t.TempDir(), data).Snapshot()
	require.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(data, "session-preferences.json"), []byte("{bad secret"), 0600))
	require.Error(t, store.Activate("new"))
	body, err := os.ReadFile(filepath.Join(data, "session-preferences.json"))
	require.NoError(t, err)
	require.Equal(t, "{bad secret", string(body))
}
