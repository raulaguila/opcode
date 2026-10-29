package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/lsp"
)

type ViewParams struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

type viewTool struct {
	lspClients map[string]*lsp.Client
}

type ViewResponseMetadata struct {
	FilePath   string             `json:"file_path"`
	Content    string             `json:"content"`
	SourceRead *SourceReadReceipt `json:"source_read,omitempty"`
}

const (
	ViewToolName     = "view"
	MaxReadSize      = 250 * 1024
	DefaultReadLimit = 2000
	MaxLineLength    = 2000
	viewDescription  = `File viewing tool that reads and displays the contents of files with line numbers, allowing you to examine code, logs, or text data.

WHEN TO USE THIS TOOL:
- Use when you need to read the contents of a specific file
- Helpful for examining source code, configuration files, or log files
- Perfect for looking at text-based file formats

HOW TO USE:
- Provide the path to the file you want to view
- Optionally specify an offset to start reading from a specific line
- Optionally specify a limit to control how many lines are read

FEATURES:
- Displays file contents with line numbers for easy reference
- Can read from any position in a file using the offset parameter
- Handles large files by limiting the number of lines read
- Automatically truncates very long lines for better display
- Suggests similar file names when the requested file isn't found

LIMITATIONS:
- Reads a bounded slice even for large files
- Default reading limit is 2000 lines
- Lines longer than 2000 characters are truncated
- Cannot display binary files or images
- Images can be identified but not displayed

TIPS:
- Use with glob tool to first find files you want to view
- For code exploration, first use Grep to find relevant files, then View to examine them
- When viewing large files, use the offset parameter to read specific sections`
)

func NewViewTool(lspClients map[string]*lsp.Client) BaseTool {
	return &viewTool{
		lspClients,
	}
}

func (v *viewTool) Info() ToolInfo {
	return ToolInfo{
		Name:        ViewToolName,
		Description: viewDescription,
		Parameters: map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The path to the file to read",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "The line number to start reading from (0-based)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "The number of lines to read (defaults to 2000)",
			},
		},
		Required: []string{"file_path"},
	}
}

// Run implements Tool.
func (v *viewTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params ViewParams

	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.FilePath == "" {
		return NewTextErrorResponse("file_path is required"), nil
	}

	// Handle relative paths
	filePath := params.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(config.WorkingDirectory(), filePath)
	}

	// Check if file exists
	fileInfo, err := statFile(ctx, filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Try to offer suggestions for similarly named files
			dir := filepath.Dir(filePath)
			base := filepath.Base(filePath)

			dirEntries, dirErr := os.ReadDir(dir)
			if dirErr == nil {
				var suggestions []string
				for _, entry := range dirEntries {
					if strings.Contains(strings.ToLower(entry.Name()), strings.ToLower(base)) ||
						strings.Contains(strings.ToLower(base), strings.ToLower(entry.Name())) {
						suggestions = append(suggestions, filepath.Join(dir, entry.Name()))
						if len(suggestions) >= 3 {
							break
						}
					}
				}

				if len(suggestions) > 0 {
					return NewTextErrorResponse(fmt.Sprintf("File not found: %s\n\nDid you mean one of these?\n%s",
						filePath, strings.Join(suggestions, "\n"))), nil
				}
			}

			return NewTextErrorResponse(fmt.Sprintf("File not found: %s", filePath)), nil
		}
		return ToolResponse{}, fmt.Errorf("error accessing file: %w", err)
	}

	// Check if it's a directory
	if fileInfo.IsDir() {
		return NewTextErrorResponse(fmt.Sprintf("Path is a directory, not a file: %s", filePath)), nil
	}

	// Set default limit if not provided
	if params.Limit <= 0 {
		params.Limit = DefaultReadLimit
	}

	// Check if it's an image file
	isImage, imageType := isImageFile(filePath)
	// TODO: handle images
	if isImage {
		return NewTextErrorResponse(fmt.Sprintf("This is an image file of type: %s\nUse a different tool to process images", imageType)), nil
	}

	// Read the file content
	var raw []byte
	var content string
	var lineCount int
	// Read and hash the same snapshot. Very large files retain bounded viewing
	// without claiming a whole-file receipt that cannot be checked within limits.
	if fileInfo.Size() <= 16<<20 {
		raw, err = readFile(ctx, filePath)
		if err == nil {
			content, lineCount, err = readTextReader(ctx, bytes.NewReader(raw), params.Offset, params.Limit)
		}
	} else {
		content, lineCount, err = readTextFile(ctx, filePath, params.Offset, params.Limit)
	}
	if err != nil {
		return ToolResponse{}, fmt.Errorf("error reading file: %w", err)
	}

	notifyLspOpenFile(ctx, filePath, v.lspClients)
	// Providers do not all retain tool-call arguments in their templates. Keep
	// the inspected path in the result itself, alongside its numbered source.
	sourcePath := filePath
	if relative, err := filepath.Rel(config.WorkingDirectory(), filePath); err == nil && filepath.IsLocal(relative) {
		sourcePath = relative
	}
	quotedPath, _ := json.Marshal(filepath.ToSlash(sourcePath))
	output := "File: " + string(quotedPath) + "\n<file>\n"
	// Format the output with line numbers
	output += addLineNumbers(content, params.Offset+1)

	// Add a note if the content was truncated
	if lineCount > params.Offset+len(strings.Split(content, "\n")) {
		output += fmt.Sprintf("\n\n(File has more lines. Use 'offset' parameter to read beyond line %d)",
			params.Offset+len(strings.Split(content, "\n")))
	}
	output += "\n</file>\n"
	output += getDiagnostics(filePath, v.lspClients)
	recordFileRead(filePath)
	preview := content
	if len(preview) > 1024 {
		preview = string([]rune(preview)[:min(256, len([]rune(preview)))]) + "\n[Preview abbreviated; full source is in the tool result.]"
	}
	metadata := ViewResponseMetadata{FilePath: filePath, Content: preview}
	if raw != nil && content != "" {
		metadata.SourceRead = &SourceReadReceipt{Path: filePath, Hash: indexDigest(string(raw)), StartLine: params.Offset + 1, EndLine: params.Offset + len(strings.Split(content, "\n"))}
	}
	return WithResponseMetadata(
		NewTextResponse(output),
		metadata,
	), nil
}

func addLineNumbers(content string, startLine int) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")

	var result []string
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")

		lineNum := i + startLine
		numStr := fmt.Sprintf("%d", lineNum)

		if len(numStr) >= 6 {
			result = append(result, fmt.Sprintf("%s|%s", numStr, line))
		} else {
			paddedNum := fmt.Sprintf("%6s", numStr)
			result = append(result, fmt.Sprintf("%s|%s", paddedNum, line))
		}
	}

	return strings.Join(result, "\n")
}

func readTextFile(ctx context.Context, filePath string, offset, limit int) (string, int, error) {
	file, err := openFile(ctx, filePath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	return readTextReader(ctx, file, offset, limit)
}

func readTextReader(ctx context.Context, source io.Reader, offset, limit int) (string, int, error) {

	if offset < 0 {
		return "", 0, fmt.Errorf("offset must be nonnegative")
	}
	if limit <= 0 {
		limit = DefaultReadLimit
	}
	limit = min(limit, DefaultReadLimit)
	reader := bufio.NewReader(source)
	var lines []string
	lineCount := 0
	for {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		var prefix []byte
		hasData := false
		for {
			fragment, err := reader.ReadSlice('\n')
			if len(fragment) > 0 {
				hasData = true
			}
			if len(prefix) < MaxLineLength {
				prefix = append(prefix, fragment[:min(len(fragment), MaxLineLength-len(prefix))]...)
			}
			if err == bufio.ErrBufferFull {
				continue
			}
			if err != nil && err != io.EOF {
				return "", 0, err
			}
			if !hasData {
				return strings.Join(lines, "\n"), lineCount, nil
			}
			break
		}
		if lineCount >= offset {
			lines = append(lines, strings.TrimRight(string(prefix), "\r\n"))
		}
		lineCount++
		if len(lines) >= limit {
			if _, err := reader.Peek(1); err == io.EOF {
				return strings.Join(lines, "\n"), lineCount, nil
			}
			return strings.Join(lines, "\n"), lineCount + 1, nil
		}
	}
}

func isImageFile(filePath string) (bool, string) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		return true, "JPEG"
	case ".png":
		return true, "PNG"
	case ".gif":
		return true, "GIF"
	case ".bmp":
		return true, "BMP"
	case ".svg":
		return true, "SVG"
	case ".webp":
		return true, "WebP"
	default:
		return false, ""
	}
}

type LineScanner struct {
	scanner *bufio.Scanner
}

func NewLineScanner(r io.Reader) *LineScanner {
	return &LineScanner{
		scanner: bufio.NewScanner(r),
	}
}

func (s *LineScanner) Scan() bool {
	return s.scanner.Scan()
}

func (s *LineScanner) Text() string {
	return s.scanner.Text()
}

func (s *LineScanner) Err() error {
	return s.scanner.Err()
}
