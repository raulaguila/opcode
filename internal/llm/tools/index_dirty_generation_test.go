package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIndexInventoryWaitsForTheNewerCapturedGeneration(t *testing.T) {
	root, _, watcher := continuationIndexFixture(t, 1)
	path := filepath.Join(root, "source0000.txt")
	watcher.state.inventory = map[string]bool{path: true}
	watcher.state.dirty[path] = 2
	plan := indexRefreshPlan{watcher: watcher, partial: true, sequence: 1, paths: []string{path}}
	require.NoError(t, os.Remove(path))
	plan.consumeDirty(&watcher.state)
	require.True(t, watcher.state.inventory[path], "a newer deletion retains the conservative counted entry until inspected")
	require.EqualValues(t, 2, watcher.state.dirty[path], "the newer event must remain queued")
	plan.sequence = 2
	plan.consumeDirty(&watcher.state)
	require.NotContains(t, watcher.state.inventory, path)
	require.NotContains(t, watcher.state.dirty, path)
}

func TestIndexInventoryCountsNewPathsDespiteNewerEvents(t *testing.T) {
	root, _, watcher := continuationIndexFixture(t, 1)
	path := filepath.Join(root, "source0000.txt")
	watcher.state.inventory = map[string]bool{}
	watcher.state.dirty[path] = 2
	plan := indexRefreshPlan{watcher: watcher, partial: true, sequence: 1, paths: []string{path}}
	plan.consumeDirty(&watcher.state)
	require.True(t, watcher.state.inventory[path], "a committed new file must count even while a newer write remains queued")
	require.EqualValues(t, 2, watcher.state.dirty[path])
	// A captured generation still checks live policy rather than trusting its old
	// inventory entry, including when the path has become ignored.
	retrievalFixture(t, root, ".opcodeignore", "source0000.txt\n")
	plan.sequence = 2
	plan.consumeDirty(&watcher.state)
	require.NotContains(t, watcher.state.inventory, path)
	require.NotContains(t, watcher.state.dirty, path)
}
