package fileutil

import (
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
