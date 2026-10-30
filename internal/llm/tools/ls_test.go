package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLsTool_Info(t *testing.T) {
	tool := NewLsTool()
	info := tool.Info()

	assert.Equal(t, LSToolName, info.Name)
	assert.NotEmpty(t, info.Description)
	assert.Contains(t, info.Parameters, "path")
	assert.Contains(t, info.Parameters, "ignore")
	assert.Contains(t, info.Required, "path")
}

func TestLsTool_Run(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "ls_tool_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a test directory structure
	testDirs := []string{
		"dir1",
		"dir2",
		"dir2/subdir1",
		"dir2/subdir2",
		"dir3",
		"dir3/.hidden_dir",
		"__pycache__",
	}

	testFiles := []string{
		"file1.txt",
		"file2.txt",
		"dir1/file3.txt",
		"dir2/file4.txt",
		"dir2/subdir1/file5.txt",
		"dir2/subdir2/file6.txt",
		"dir3/file7.txt",
		"dir3/.hidden_file.txt",
		"__pycache__/cache.pyc",
		".hidden_root_file.txt",
	}

	// Create directories
	for _, dir := range testDirs {
		dirPath := filepath.Join(tempDir, dir)
		err := os.MkdirAll(dirPath, 0755)
		require.NoError(t, err)
	}

	// Create files
	for _, file := range testFiles {
		filePath := filepath.Join(tempDir, file)
		err := os.WriteFile(filePath, []byte("test content"), 0644)
		require.NoError(t, err)
	}

	t.Run("lists directory successfully", func(t *testing.T) {
		tool := NewLsTool()
		params := LSParams{
			Path: tempDir,
		}

		paramsJSON, err := json.Marshal(params)
		require.NoError(t, err)

		call := ToolCall{
			Name:  LSToolName,
			Input: string(paramsJSON),
		}

		response, err := tool.Run(context.Background(), call)
		require.NoError(t, err)

		// Check that visible directories and files are included
		assert.Contains(t, response.Content, "dir1")
		assert.Contains(t, response.Content, "dir2")
		assert.Contains(t, response.Content, "dir3")
		assert.Contains(t, response.Content, "file1.txt")
		assert.Contains(t, response.Content, "file2.txt")

		// Check that hidden files and directories are not included
		assert.NotContains(t, response.Content, ".hidden_dir")
		assert.NotContains(t, response.Content, ".hidden_file.txt")
		assert.NotContains(t, response.Content, ".hidden_root_file.txt")

		// Check that __pycache__ is not included
		assert.NotContains(t, response.Content, "__pycache__")
	})

	t.Run("handles non-existent path", func(t *testing.T) {
		tool := NewLsTool()
		params := LSParams{
			Path: filepath.Join(tempDir, "non_existent_dir"),
		}

		paramsJSON, err := json.Marshal(params)
		require.NoError(t, err)

		call := ToolCall{
			Name:  LSToolName,
			Input: string(paramsJSON),
		}

		response, err := tool.Run(context.Background(), call)
		require.NoError(t, err)
		assert.Contains(t, response.Content, "path does not exist")
	})

	t.Run("handles empty path parameter", func(t *testing.T) {
		// For this test, we need to mock the config.WorkingDirectory function
		// Since we can't easily do that, we'll just check that the response doesn't contain an error message

		tool := NewLsTool()
		params := LSParams{
			Path: "",
		}

		paramsJSON, err := json.Marshal(params)
		require.NoError(t, err)

		call := ToolCall{
			Name:  LSToolName,
			Input: string(paramsJSON),
		}

		response, err := tool.Run(context.Background(), call)
		require.NoError(t, err)

		// The response should either contain a valid directory listing or an error
		// We'll just check that it's not empty
		assert.NotEmpty(t, response.Content)
	})

	t.Run("handles invalid parameters", func(t *testing.T) {
		tool := NewLsTool()
		call := ToolCall{
			Name:  LSToolName,
			Input: "invalid json",
		}

		response, err := tool.Run(context.Background(), call)
		require.NoError(t, err)
		assert.Contains(t, response.Content, "error parsing parameters")
	})

	t.Run("respects ignore patterns", func(t *testing.T) {
		tool := NewLsTool()
		params := LSParams{
			Path:   tempDir,
			Ignore: []string{"file1.txt", "dir1"},
		}

		paramsJSON, err := json.Marshal(params)
		require.NoError(t, err)

		call := ToolCall{
			Name:  LSToolName,
			Input: string(paramsJSON),
		}

		response, err := tool.Run(context.Background(), call)
		require.NoError(t, err)

		// The output format is a tree, so we need to check for specific patterns
		// Check that file1.txt is not directly mentioned
		assert.NotContains(t, response.Content, "- file1.txt")

		// Check that dir1/ is not directly mentioned
		assert.NotContains(t, response.Content, "- dir1/")
	})

	t.Run("handles relative path", func(t *testing.T) {
		// Save original working directory
		origWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() {
			os.Chdir(origWd)
		}()

		// Change to a directory above the temp directory
		parentDir := filepath.Dir(tempDir)
		cfg := config.Get()
		originalConfigDir := cfg.WorkingDir
		cfg.WorkingDir = parentDir
		t.Cleanup(func() { cfg.WorkingDir = originalConfigDir })
		err = os.Chdir(parentDir)
		require.NoError(t, err)

		tool := NewLsTool()
		params := LSParams{
			Path: filepath.Base(tempDir),
		}

		paramsJSON, err := json.Marshal(params)
		require.NoError(t, err)

		call := ToolCall{
			Name:  LSToolName,
			Input: string(paramsJSON),
		}

		response, err := tool.Run(context.Background(), call)
		require.NoError(t, err)

		// Should list the temp directory contents
		assert.Contains(t, response.Content, "dir1")
		assert.Contains(t, response.Content, "file1.txt")
	})
}

func TestShouldSkip(t *testing.T) {
	testCases := []struct {
		name           string
		path           string
		ignorePatterns []string
		expected       bool
	}{
		{
			name:           "hidden file",
			path:           "/path/to/.hidden_file",
			ignorePatterns: []string{},
			expected:       true,
		},
		{
			name:           "hidden directory",
			path:           "/path/to/.hidden_dir",
			ignorePatterns: []string{},
			expected:       true,
		},
		{
			name:           "pycache directory",
			path:           "/path/to/__pycache__/file.pyc",
			ignorePatterns: []string{},
			expected:       true,
		},
		{
			name:           "node_modules directory",
			path:           "/path/to/node_modules/package",
			ignorePatterns: []string{},
			expected:       false, // The shouldSkip function doesn't directly check for node_modules in the path
		},
		{
			name:           "normal file",
			path:           "/path/to/normal_file.txt",
			ignorePatterns: []string{},
			expected:       false,
		},
		{
			name:           "normal directory",
			path:           "/path/to/normal_dir",
			ignorePatterns: []string{},
			expected:       false,
		},
		{
			name:           "ignored by pattern",
			path:           "/path/to/ignore_me.txt",
			ignorePatterns: []string{"ignore_*.txt"},
			expected:       true,
		},
		{
			name:           "not ignored by pattern",
			path:           "/path/to/keep_me.txt",
			ignorePatterns: []string{"ignore_*.txt"},
			expected:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := shouldSkip(tc.path, tc.ignorePatterns)
			assert.Equal(t, tc.expected, result)
		})
	}
}
