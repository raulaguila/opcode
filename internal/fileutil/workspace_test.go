package fileutil

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMatchFilesPutsFileNamesStartingWithTheQueryFirst(t *testing.T) {
	all := []string{"internal/ui/model.go", "cmd/root.go", "internal/models/list.go", "README.md"}
	got := MatchFiles(all, "mod", 3)
	want := []string{"internal/ui/model.go", "internal/models/list.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("MatchFiles = %v, want %v", got, want)
	}
	if got := MatchFiles(all, "", 2); len(got) != 2 {
		t.Fatalf("empty query should list the first files, got %v", got)
	}
}

func TestGitBranchReadsHeadAndWorktrees(t *testing.T) {
	repo := t.TempDir()
	require := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	require(os.MkdirAll(filepath.Join(repo, ".git"), 0o700))
	require(os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o600))
	if got := readBranch(repo); got != "main" {
		t.Fatalf("branch = %q", got)
	}
	tree := t.TempDir()
	require(os.WriteFile(filepath.Join(tree, ".git"), []byte("gitdir: "+filepath.Join(repo, ".git")+"\n"), 0o600))
	require(os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("0123456789abcdef\n"), 0o600))
	if got := readBranch(tree); got != "0123456" {
		t.Fatalf("detached worktree = %q", got)
	}
}

func TestSearchWorkspaceFindsByPathIgnoringCase(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"main.go", "cmd/Main_test.go", "README.md"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := SearchWorkspace(root, "MAIN", 10)
	slices.Sort(got)
	if want := []string{"cmd/Main_test.go", "main.go"}; !slices.Equal(got, want) {
		t.Fatalf("SearchWorkspace = %v, want %v", got, want)
	}
	if got := SearchWorkspace(root, "main", 1); len(got) != 1 {
		t.Fatalf("limit ignored: %v", got)
	}
}
