package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/diff"
	"github.com/raulaguila/opcode/internal/history"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/permission"
)

type WriteParams struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type WritePermissionsParams struct {
	FilePath string `json:"file_path"`
	Diff     string `json:"diff"`
}

type writeTool struct {
	lspClients  map[string]*lsp.Client
	permissions permission.Service
	files       history.Service
}

type WriteResponseMetadata struct {
	Diff      string `json:"diff"`
	Additions int    `json:"additions"`
	Removals  int    `json:"removals"`
}

const (
	WriteToolName    = "write"
	writeDescription = `File writing tool that creates or updates files in the filesystem, allowing you to save or modify text content.

WHEN TO USE THIS TOOL:
- Use when you need to create a new file
- Helpful for updating existing files with modified content
- Perfect for saving generated code, configurations, or text data

HOW TO USE:
- Provide the path to the file you want to write
- Include the content to be written to the file
- The tool will create any necessary parent directories

FEATURES:
- Can create new files or overwrite existing ones
- Creates parent directories automatically if they don't exist
- Checks if the file has been modified since last read for safety
- Avoids unnecessary writes when content hasn't changed

LIMITATIONS:
- You should read a file before writing to it to avoid conflicts
- Cannot append to files (rewrites the entire file)


TIPS:
- Use the View tool first to examine existing files before modifying them
- Use the ls tool to verify the correct location when creating new files
- Combine with Glob and Grep tools to find and modify multiple files
- Always include descriptive comments when making changes to existing code`
)

func NewWriteTool(lspClients map[string]*lsp.Client, permissions permission.Service, files history.Service) BaseTool {
	return &writeTool{
		lspClients:  lspClients,
		permissions: permissions,
		files:       files,
	}
}

func (w *writeTool) Info() ToolInfo {
	return ToolInfo{
		Name:        WriteToolName,
		Description: writeDescription,
		Parameters: map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The path to the file to write",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file",
			},
		},
		Required: []string{"file_path", "content"},
	}
}

func (w *writeTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	params, invalid := parseWriteParams(call.Input)
	if invalid != nil {
		return *invalid, nil
	}

	filePath := writeTargetPath(params.FilePath)

	fileInfo, err := statFile(ctx, filePath)
	if err == nil {
		if response := checkExistingWrite(ctx, filePath, params.Content, fileInfo); response != nil {
			return *response, nil
		}
	} else if !os.IsNotExist(err) {
		return ToolResponse{}, fmt.Errorf("error checking file: %w", err)
	}
	oldContent := ""
	if fileInfo != nil && !fileInfo.IsDir() {
		oldBytes, readErr := readFile(ctx, filePath)
		if readErr == nil {
			oldContent = string(oldBytes)
		}
	}

	sessionID, messageID := GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return ToolResponse{}, fmt.Errorf("session_id and message_id are required")
	}

	diff, additions, removals := diff.GenerateDiff(
		oldContent,
		params.Content,
		filePath,
	)

	permissionPath := filepath.Dir(filePath)
	p := w.permissions.Request(ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        permissionPath,
			ToolName:    WriteToolName,
			Action:      "write",
			Description: fmt.Sprintf("Create file %s", filePath),
			Params: WritePermissionsParams{
				FilePath: filePath,
				Diff:     diff,
			},
		},
	)
	if !p {
		return ToolResponse{}, permission.ErrorPermissionDenied
	}

	var expected *string
	if fileInfo != nil {
		expected = &oldContent
	}
	if err = atomicWriteFile(ctx, filePath, params.Content, expected); err != nil {
		return ToolResponse{}, err
	}

	if err := saveEditedFileHistory(ctx, w.files, sessionID, filePath, oldContent, params.Content); err != nil {
		return ToolResponse{}, err
	}

	recordFileWrite(filePath)
	recordFileRead(filePath)
	waitForLspDiagnostics(ctx, filePath, w.lspClients)

	result := fmt.Sprintf("File successfully written: %s", filePath)
	result = fmt.Sprintf("<result>\n%s\n</result>", result)
	result += getDiagnostics(filePath, w.lspClients)
	return WithResponseMetadata(NewTextResponse(result),
		WriteResponseMetadata{
			Diff:      diff,
			Additions: additions,
			Removals:  removals,
		},
	), nil
}

func checkExistingWrite(ctx context.Context, filePath, newContent string, fileInfo os.FileInfo) *ToolResponse {
	if fileInfo.IsDir() {
		response := NewTextErrorResponse(fmt.Sprintf("Path is a directory, not a file: %s", filePath))
		return &response
	}

	modTime := fileInfo.ModTime()
	lastRead := getLastReadTime(filePath)
	if modTime.After(lastRead) {
		response := NewTextErrorResponse(fmt.Sprintf("File %s has been modified since it was last read.\nLast modification: %s\nLast read: %s\n\nPlease read the file again before modifying it.",
			filePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339)))
		return &response
	}

	oldContent, readErr := readFile(ctx, filePath)
	if readErr == nil && string(oldContent) == newContent {
		response := NewTextErrorResponse(fmt.Sprintf("File %s already contains the exact content. No changes made.", filePath))
		return &response
	}
	return nil
}

func parseWriteParams(raw string) (WriteParams, *ToolResponse) {
	var params WriteParams
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		response := NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err))
		return params, &response
	}

	if params.FilePath == "" {
		response := NewTextErrorResponse("file_path is required")
		return params, &response
	}

	if params.Content == "" {
		response := NewTextErrorResponse("content is required")
		return params, &response
	}

	return params, nil
}
func writeTargetPath(path string) string {
	filePath := path
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(config.WorkingDirectory(), filePath)
	}

	return filePath
}
