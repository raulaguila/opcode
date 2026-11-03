package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/tools/shell"
)

type LSParams struct {
	Path      string   `json:"path"`
	Snapshot  string   `json:"snapshot,omitempty"`
	Ignore    []string `json:"ignore"`
	Offset    int      `json:"offset"`
	Limit     int      `json:"limit"`
	Glob      string   `json:"glob"`
	Language  string   `json:"language"`
	Extension string   `json:"extension"`
	Recursive bool     `json:"recursive,omitempty"`
}

type LSResponseMetadata struct {
	NumberOfFiles int  `json:"number_of_files"`
	Truncated     bool `json:"truncated"`
}

type lsTool struct{}

const (
	LSToolName    = "ls"
	lsDescription = `List allowed project file paths in sorted pages. When every file fits in one page they are all listed; otherwise the page shows the folder's subfolders first, each with its file count, then its own files: call ls on a subfolder to go deeper, or set recursive=true for every file. Use offset/limit (default and maximum 60), glob, language, extension and ignore filters. Read next_offset when more=true to continue with the same filters. Each page is bounded to 6000 bytes of paths; directory scan is bounded to 20000 files. Respects ripgrep ignores when available and omits secrets, storage, dependencies and symlinks. Listing is navigation, not proof of file contents.`
)

func NewLsTool() BaseTool {
	return &lsTool{}
}

func (l *lsTool) Info() ToolInfo {
	params := scopeParameters()
	params["snapshot"] = map[string]any{"type": "string", "description": "Snapshot from previous page; changed listings require restarting at offset 0."}
	params["ignore"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	params["offset"] = map[string]any{"type": "integer", "minimum": 0, "maximum": 20000}
	params["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 60}
	params["recursive"] = map[string]any{"type": "boolean", "description": "List every file below path instead of folders first."}
	return ToolInfo{Name: LSToolName, Description: lsDescription, Parameters: params, Required: []string{"path"}}
}

func (l *lsTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params LSParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}
	scope, err := listingScope(params)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	root, err := authorizeFile(ctx, params.Path, "read")
	if err != nil {
		return ToolResponse{}, err
	}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return NewTextErrorResponse(fmt.Sprintf("path does not exist: %s", root)), nil
	}
	files, truncated, err := scopedFilePaths(ctx, root, scope, params.Ignore)
	if err != nil {
		return ToolResponse{}, err
	}
	snapshot, err := listingSnapshot(ctx, root, files)
	if err != nil {
		return ToolResponse{}, err
	}
	if params.Snapshot != "" && params.Snapshot != snapshot {
		return NewTextErrorResponse("listing changed; restart at offset 0 with the new snapshot"), nil
	}
	limit := params.Limit
	if limit == 0 {
		limit = 60
	}
	if !params.Recursive && params.Glob == "" && params.Language == "" && params.Extension == "" && len(files) > limit {
		return folderPage(root, snapshot, files, params.Offset, limit, truncated)
	}
	start := min(params.Offset, len(files))
	end, err := listingPageEnd(files, start, limit)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	var next *int
	if end < len(files) {
		next = &end
	}
	response := jsonToolResult(map[string]any{"snapshot": snapshot, "root": root, "files": files[start:end], "total": len(files), "offset": start, "next_offset": next, "more": next != nil, "truncated": truncated, "navigation_only": true})
	return WithResponseMetadata(response, LSResponseMetadata{NumberOfFiles: end - start, Truncated: next != nil || truncated}), nil
}

func shouldSkip(path string, ignorePatterns []string) bool {
	if shell.SensitivePath(path) {
		return true
	}
	base := filepath.Base(path)

	if base != "." && strings.HasPrefix(base, ".") {
		return true
	}

	commonIgnored := []string{
		"__pycache__",
		"node_modules",
		"dist",
		"build",
		"target",
		"vendor",
		"bin",
		"obj",
		".git",
		".idea",
		".vscode",
		".DS_Store",
		"*.pyc",
		"*.pyo",
		"*.pyd",
		"*.so",
		"*.dll",
		"*.exe",
	}

	if strings.Contains(path, filepath.Join("__pycache__", "")) {
		return true
	}

	for _, ignored := range commonIgnored {
		if ignoredPathPattern(path, base, ignored) {
			return true
		}
	}

	for _, pattern := range ignorePatterns {
		matched, err := filepath.Match(pattern, base)
		if err == nil && matched {
			return true
		}
	}

	return false
}

func listingPageEnd(files []string, start, limit int) (int, error) {
	size := 0
	end := start
	for end < len(files) && end-start < limit {
		raw, _ := json.Marshal(files[end])
		if size+len(raw)+1 > 6000 {
			if end == start {
				return 0, fmt.Errorf("one path exceeds the page byte limit; use a narrower directory scope")
			}
			break
		}
		size += len(raw) + 1
		end++
	}
	return end, nil
}

func ignoredPathPattern(path, base, pattern string) bool {
	if strings.HasSuffix(pattern, "/") {
		return strings.Contains(path, filepath.Join(pattern[:len(pattern)-1], ""))
	}
	if strings.HasPrefix(pattern, "*.") {
		return strings.HasSuffix(base, pattern[1:])
	}
	return base == pattern
}

func listingScope(params LSParams) (retrievalScope, error) {
	if params.Offset < 0 || params.Offset > 20000 || params.Limit < 0 || params.Limit > 60 || len(params.Ignore) > 32 {
		return retrievalScope{}, fmt.Errorf("offset must be 0–20000, limit 1–60, and at most 32 ignore patterns")
	}
	scope := retrievalScope{Path: params.Path, Glob: params.Glob, Language: params.Language, Extension: params.Extension}
	if err := scope.validate(); err != nil {
		return retrievalScope{}, err
	}
	for _, pattern := range params.Ignore {
		if len(pattern) > 500 {
			return retrievalScope{}, fmt.Errorf("ignore patterns must be at most 500 bytes")
		}
	}
	return scope, nil
}

// folderPage is one level of a listing too long for a page: the subfolders,
// each with how many files it holds, then the files of the folder itself. A
// flat page of a large tree shows a few folders' first files and hides the
// rest of the structure.
func folderPage(root, snapshot string, files []string, offset, limit int, truncated bool) (ToolResponse, error) {
	counts := map[string]int{}
	var dirs, own []string
	for _, file := range files {
		file = filepath.ToSlash(file)
		if dir, _, nested := strings.Cut(file, "/"); nested {
			if counts[dir+"/"] == 0 {
				dirs = append(dirs, dir+"/")
			}
			counts[dir+"/"]++
		} else {
			own = append(own, file)
		}
	}
	slices.Sort(dirs)
	entries := append(dirs, own...)
	start := min(offset, len(entries))
	end, err := listingPageEnd(entries, start, limit)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	type folder struct {
		Path  string `json:"path"`
		Files int    `json:"files"`
	}
	pageDirs, pageFiles := []folder{}, []string{}
	for _, entry := range entries[start:end] {
		if strings.HasSuffix(entry, "/") {
			pageDirs = append(pageDirs, folder{entry, counts[entry]})
		} else {
			pageFiles = append(pageFiles, entry)
		}
	}
	var next *int
	if end < len(entries) {
		next = &end
	}
	response := jsonToolResult(map[string]any{"snapshot": snapshot, "root": root, "dirs": pageDirs, "files": pageFiles, "total": len(entries), "total_files": len(files), "offset": start, "next_offset": next, "more": next != nil, "truncated": truncated, "navigation_only": true})
	return WithResponseMetadata(response, LSResponseMetadata{NumberOfFiles: len(pageFiles), Truncated: next != nil || truncated}), nil
}
