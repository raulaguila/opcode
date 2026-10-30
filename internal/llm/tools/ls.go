package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
}

type TreeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Type     string      `json:"type"` // "file" or "directory"
	Children []*TreeNode `json:"children,omitempty"`
}

type LSResponseMetadata struct {
	NumberOfFiles int  `json:"number_of_files"`
	Truncated     bool `json:"truncated"`
}

type lsTool struct{}

const (
	LSToolName    = "ls"
	MaxLSFiles    = 1000
	lsDescription = `List allowed project file paths in sorted pages. Use offset/limit (default and maximum 60), glob, language, extension and ignore filters. Read next_offset when more=true to continue with the same filters. Each page is bounded to 6000 bytes of paths; directory scan is bounded to 20000 files. Respects ripgrep ignores when available and omits secrets, storage, dependencies and symlinks. Listing is navigation, not proof of file contents.`
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

func listDirectory(initialPath string, ignorePatterns []string, limit int, contexts ...context.Context) ([]string, bool, error) {
	ctx := context.Background()
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	collector := directoryListing{ctx: ctx, initialPath: initialPath, ignorePatterns: ignorePatterns, limit: limit}
	err := filepath.Walk(initialPath, collector.visit)
	results, truncated := collector.results, collector.truncated
	if err != nil {
		return nil, truncated, err
	}

	return results, truncated, nil
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

func createFileTree(sortedPaths []string) []*TreeNode {
	builder := fileTreeBuilder{roots: []*TreeNode{}, nodes: make(map[string]*TreeNode)}
	for _, path := range sortedPaths {
		builder.add(path)
	}
	return builder.roots
}

func printTree(tree []*TreeNode, rootPath string) string {
	var result strings.Builder

	result.WriteString(fmt.Sprintf("- %s%s\n", rootPath, string(filepath.Separator)))

	for _, node := range tree {
		printNode(&result, node, 1)
	}

	return result.String()
}

func printNode(builder *strings.Builder, node *TreeNode, level int) {
	indent := strings.Repeat("  ", level)

	nodeName := node.Name
	if node.Type == "directory" {
		nodeName += string(filepath.Separator)
	}

	fmt.Fprintf(builder, "%s- %s\n", indent, nodeName)

	if node.Type == "directory" && len(node.Children) > 0 {
		for _, child := range node.Children {
			printNode(builder, child, level+1)
		}
	}
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

type directoryListing struct {
	ctx            context.Context
	initialPath    string
	ignorePatterns []string
	limit          int
	results        []string
	truncated      bool
}

func (c *directoryListing) visit(path string, info os.FileInfo, err error) error {

	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	if err != nil {
		return nil // Skip files we don't have permission to access
	}

	if shouldSkip(path, c.ignorePatterns) {
		if info.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}

	if path != c.initialPath {
		if info.IsDir() {
			path = path + string(filepath.Separator)
		}
		c.results = append(c.results, path)
	}

	if len(c.results) >= c.limit {
		c.truncated = true
		return filepath.SkipAll
	}

	return nil

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

type fileTreeBuilder struct {
	roots []*TreeNode
	nodes map[string]*TreeNode
}

func (t *fileTreeBuilder) add(path string) {
	parts := strings.Split(path, string(filepath.Separator))
	currentPath := ""
	var parentPath string

	var cleanParts []string
	for _, part := range parts {
		if part != "" {
			cleanParts = append(cleanParts, part)
		}
	}
	parts = cleanParts

	if len(parts) == 0 {
		return
	}

	for i, part := range parts {
		if currentPath == "" {
			currentPath = part
		} else {
			currentPath = filepath.Join(currentPath, part)
		}

		if _, exists := t.nodes[currentPath]; exists {
			parentPath = currentPath
			continue
		}

		t.appendNode(part, currentPath, parentPath, i > 0, i < len(parts)-1 || strings.HasSuffix(path, string(filepath.Separator)))
		parentPath = currentPath
	}
}

func (t *fileTreeBuilder) appendNode(part, currentPath, parentPath string, hasParent, isDir bool) {

	nodeType := "file"
	if isDir {
		nodeType = "directory"
	}
	newNode := &TreeNode{
		Name:     part,
		Path:     currentPath,
		Type:     nodeType,
		Children: []*TreeNode{},
	}

	t.nodes[currentPath] = newNode

	if hasParent && parentPath != "" {
		if parent, ok := t.nodes[parentPath]; ok {
			parent.Children = append(parent.Children, newNode)
		}
	} else {
		t.roots = append(t.roots, newNode)
	}

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
