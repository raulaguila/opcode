package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetContextFromPaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	_, err := config.Load(tmpDir, false)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}
	cfg := config.Get()
	cfg.WorkingDir = tmpDir
	cfg.ContextPaths = []string{
		"file.txt",
		"directory/",
	}
	testFiles := []string{
		"file.txt",
		"directory/file_a.txt",
		"directory/file_b.txt",
		"directory/file_c.txt",
	}

	createTestFiles(t, tmpDir, testFiles)

	context := getContextFromPaths()
	expectedContext := fmt.Sprintf("# From:%s/file.txt\nfile.txt: test content\n# From:%s/directory/file_a.txt\ndirectory/file_a.txt: test content\n# From:%s/directory/file_b.txt\ndirectory/file_b.txt: test content\n# From:%s/directory/file_c.txt\ndirectory/file_c.txt: test content", tmpDir, tmpDir, tmpDir, tmpDir)
	// Files are loaded concurrently; their completion order is not guaranteed.
	expectedBlocks := strings.Split(expectedContext, "# From:")
	actualBlocks := strings.Split(context, "# From:")
	for i := range expectedBlocks {
		expectedBlocks[i] = strings.TrimSpace(expectedBlocks[i])
	}
	for i := range actualBlocks {
		actualBlocks[i] = strings.TrimSpace(actualBlocks[i])
	}
	assert.ElementsMatch(t, expectedBlocks, actualBlocks)
}

func createTestFiles(t *testing.T, tmpDir string, testFiles []string) {
	t.Helper()
	for _, path := range testFiles {
		fullPath := filepath.Join(tmpDir, path)
		if path[len(path)-1] == '/' {
			err := os.MkdirAll(fullPath, 0755)
			require.NoError(t, err)
		} else {
			dir := filepath.Dir(fullPath)
			err := os.MkdirAll(dir, 0755)
			require.NoError(t, err)
			err = os.WriteFile(fullPath, []byte(path+": test content"), 0644)
			require.NoError(t, err)
		}
	}
}

func TestAutomaticGuidanceIsBoundedAndRejectsSecretsAndExternalLinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	require.NoError(t, os.WriteFile(outside, []byte("OUTSIDE_MUST_NOT_LEAK"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET_MUST_NOT_LEAK"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "safe.md"), []byte("Project guidance"), 0600))
	if err := os.Symlink(outside, filepath.Join(root, "AGENTS.md")); err != nil {
		t.Skip("symlinks unavailable")
	}
	text := processContextPaths(root, []string{"safe.md", ".env", "AGENTS.md", outside, "../outside.md"})
	require.Contains(t, text, "Project guidance")
	require.NotContains(t, text, "MUST_NOT_LEAK")
	require.NoError(t, os.WriteFile(filepath.Join(root, "large.md"), []byte(strings.Repeat("x", 100000)), 0600))
	text = processContextPaths(root, []string{"large.md"})
	require.LessOrEqual(t, len(text), 16384)
	require.Contains(t, text, "truncated")
}

func TestLegacyGuidanceCaseVariantRemainsReadableAfterRename(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "OpenCode.md"), []byte("LegacyGuidanceMarker: preserve local decisions"), 0600))
	text := processContextPaths(root, []string{"opencode.md", "OpenCode.md", "OPENCODE.md"})
	require.Contains(t, text, "LegacyGuidanceMarker")
}

func TestInitialProjectOverviewDoesNotEmbedRecursiveTree(t *testing.T) {
	root := t.TempDir()
	_, err := config.Load(root, false)
	require.NoError(t, err)
	for i := range 600 {
		path := filepath.Join(root, "internal", fmt.Sprintf("package_%03d", i), "implementation.go")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte("package example\n"), 0600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("Project overview"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("PRIVATE_VALUE"), 0600))
	text := CoderPrompt(models.ProviderLocal)
	require.Contains(t, text, "README.md")
	require.NotContains(t, text, "internal/")
	require.NotContains(t, text, "implementation.go", "nested paths belong in on-demand tool results")
	require.NotContains(t, text, ".env\n")
	require.NotContains(t, text, "PRIVATE_VALUE")
	require.Less(t, len(text), 8000, "initial prompt must not grow with nested file count")
	before := getEnvironmentInfo()
	for i := range 100 {
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("root_%03d.txt", i)), nil, 0600))
	}
	text = getEnvironmentInfo()
	require.Equal(t, before, text, "root and nested file counts must not grow the system prompt")
	require.Contains(t, text, "List files on demand")
	require.Contains(t, text, "get_project_map")
	require.Contains(t, text, "search_code_index")
}
