package diff

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPatchAppliesOrRejects(t *testing.T) {
	const head = "*** Begin Patch\n"
	const tail = "\n*** End Patch"
	orig := map[string]string{"a.txt": "one\ntwo\nthree\nfour\n", "gone.txt": "bye\n"}
	for _, tc := range []struct {
		name, patch, path string
		kind              ActionType
		want              string
		fails             bool
	}{
		{name: "replace in the middle", patch: "*** Update File: a.txt\n@@\n one\n-two\n+TWO\n three", path: "a.txt", kind: ActionUpdate, want: "one\nTWO\nthree\nfour\n"},
		{name: "insert keeps neighbours", patch: "*** Update File: a.txt\n@@\n two\n+2.5\n three", path: "a.txt", kind: ActionUpdate, want: "one\ntwo\n2.5\nthree\nfour\n"},
		{name: "two hunks", patch: "*** Update File: a.txt\n@@\n one\n-two\n+2\n@@\n three\n-four\n+4", path: "a.txt", kind: ActionUpdate, want: "one\n2\nthree\n4\n"},
		{name: "add a new file", patch: "*** Add File: new.txt\n+hello\n+world", path: "new.txt", kind: ActionAdd, want: "hello\nworld"},
		{name: "delete a file", patch: "*** Delete File: gone.txt", path: "gone.txt", kind: ActionDelete},
		{name: "context not in the file", patch: "*** Update File: a.txt\n@@\n nothing here\n-two\n+2", fails: true},
		{name: "update of an unknown file", patch: "*** Update File: missing.txt\n@@\n one\n-two\n+2", fails: true},
		{name: "add over an existing file", patch: "*** Add File: a.txt\n+x", fails: true},
		{name: "delete of an unknown file", patch: "*** Delete File: missing.txt", fails: true},
		{name: "unknown section", patch: "*** Rename File: a.txt", fails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patch, _, err := TextToPatch(head+tc.patch+tail, orig)
			if tc.fails {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			commit, err := PatchToCommit(patch, orig)
			require.NoError(t, err)
			change := commit.Changes[tc.path]
			require.Equal(t, tc.kind, change.Type)
			if tc.kind == ActionDelete {
				require.Equal(t, orig[tc.path], *change.OldContent)
				return
			}
			require.Equal(t, tc.want, *change.NewContent)
		})
	}
}

func TestPatchTextNeedsItsMarkers(t *testing.T) {
	for _, text := range []string{"", "*** Begin Patch", "*** Begin Patch\n*** Delete File: a.txt", "*** Delete File: a.txt\n*** End Patch"} {
		_, _, err := TextToPatch(text, map[string]string{"a.txt": "x"})
		require.Error(t, err, text)
	}
}

func TestPatchToleratesWhitespaceAndCountsIt(t *testing.T) {
	orig := map[string]string{"a.txt": "one  \ntwo\nthree\n"}
	patch, fuzz, err := TextToPatch("*** Begin Patch\n*** Update File: a.txt\n@@\n one\n-two\n+2\n three\n*** End Patch", orig)
	require.NoError(t, err)
	require.Positive(t, fuzz)
	commit, err := PatchToCommit(patch, orig)
	require.NoError(t, err)
	require.Contains(t, *commit.Changes["a.txt"].NewContent, "\n2\n")
}

func TestIdentifyFiles(t *testing.T) {
	text := "*** Begin Patch\n*** Update File: a\n*** Delete File: b\n*** Add File: c\n+x\n*** End Patch"
	require.ElementsMatch(t, []string{"a", "b"}, IdentifyFilesNeeded(text))
	require.Equal(t, []string{"c"}, IdentifyFilesAdded(text))
}
