package tools

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
)

type GrepParams struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path"`
	Include     string `json:"include"`
	LiteralText bool   `json:"literal_text"`
}

type grepMatch struct {
	path     string
	lineNum  int
	lineText string
}

type GrepResponseMetadata struct {
	NumberOfMatches int  `json:"number_of_matches"`
	Truncated       bool `json:"truncated"`
}

type grepTool struct{}

const (
	GrepToolName    = "grep"
	grepDescription = `Fast content search tool that finds files containing specific text or patterns, returning matching file paths in deterministic path order.

WHEN TO USE THIS TOOL:
- Use when you need to find files containing specific text or patterns
- Great for searching code bases for function names, variable declarations, or error messages
- Useful for finding all files that use a particular API or pattern

HOW TO USE:
- Provide a regex pattern to search for within file contents
- Set literal_text=true if you want to search for the exact text with special characters (recommended for non-regex users)
- Optionally specify a starting directory (defaults to current working directory)
- Optionally provide an include pattern to filter which files to search
- Results are returned in deterministic path order

REGEX PATTERN SYNTAX (when literal_text=false):
- Supports standard regular expression syntax
- 'function' searches for the literal text "function"
- 'log\..*Error' finds text starting with "log." and ending with "Error"
- 'import\s+.*\s+from' finds import statements in JavaScript/TypeScript

COMMON INCLUDE PATTERN EXAMPLES:
- '*.js' - Only search JavaScript files
- '*.{ts,tsx}' - Only search TypeScript files
- '*.go' - Only search Go files

LIMITATIONS:
- Results are limited to 100 matches
- Performance depends on the number of files being searched
- Very large binary files may be skipped
- Hidden files (starting with '.') are skipped

TIPS:
- For faster, more targeted searches, first use Glob to find relevant files, then use Grep
- When doing iterative exploration that may require multiple rounds of searching, consider using the Agent tool instead
- Always check if results are truncated and refine your search pattern if needed
- Use literal_text=true when searching for exact text containing special characters like dots, parentheses, etc.`
)

func NewGrepTool() BaseTool {
	return &grepTool{}
}

func (g *grepTool) Info() ToolInfo {
	return ToolInfo{
		Name:        GrepToolName,
		Description: grepDescription,
		Parameters: map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "The regex pattern to search for in file contents",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "The directory to search in. Defaults to the current working directory.",
			},
			"include": map[string]any{
				"type":        "string",
				"description": "File pattern to include in the search (e.g. \"*.js\", \"*.{ts,tsx}\")",
			},
			"literal_text": map[string]any{
				"type":        "boolean",
				"description": "If true, the pattern will be treated as literal text with special regex characters escaped. Default is false.",
			},
		},
		Required: []string{"pattern"},
	}
}

// escapeRegexPattern escapes special regex characters so they're treated as literal characters
func escapeRegexPattern(pattern string) string {
	specialChars := []string{"\\", ".", "+", "*", "?", "(", ")", "[", "]", "{", "}", "^", "$", "|"}
	escaped := pattern

	for _, char := range specialChars {
		escaped = strings.ReplaceAll(escaped, char, "\\"+char)
	}

	return escaped
}

func (g *grepTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params GrepParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.Pattern == "" {
		return NewTextErrorResponse("pattern is required"), nil
	}

	// If literal_text is true, escape the pattern
	searchPattern := params.Pattern
	if params.LiteralText {
		searchPattern = escapeRegexPattern(params.Pattern)
	}

	searchPath := params.Path
	if searchPath == "" {
		searchPath = config.WorkingDirectory()
	}

	matches, truncated, err := searchFiles(ctx, searchPattern, searchPath, params.Include, 100)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("error searching files: %w", err)
	}

	output := formatGrepMatches(matches, truncated)
	return WithResponseMetadata(
		NewTextResponse(output),
		GrepResponseMetadata{
			NumberOfMatches: len(matches),
			Truncated:       truncated,
		},
	), nil
}

func searchFiles(ctx context.Context, pattern, rootPath, include string, limit int) ([]grepMatch, bool, error) {
	rootPath = absolutePath(rootPath)
	if _, err := exec.LookPath("rg"); err != nil {
		return fallbackSearch(ctx, pattern, rootPath, include, limit)
	}
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := []string{"--json", "--no-config", "--sort", "path"}
	if include != "" {
		args = append(args, "--glob", include)
	}
	args = append(args, "--", pattern, rootPath)
	cmd, err := shell.Command(ctx, config.WorkingDirectory(), commandString(append([]string{"rg"}, args...)))
	if err != nil {
		return nil, false, err
	}
	cmd.Env = shell.Environment()
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	stderr := &shell.Buffer{Limit: 4096}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	matches, truncated, parseErr := scanRipgrepMatches(ctx, rootPath, scanner, limit)
	scanErr := scanner.Err()
	if truncated || parseErr != nil || scanErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if parseErr != nil {
		return nil, false, parseErr
	}
	if scanErr != nil {
		return nil, false, scanErr
	}
	if err := parent.Err(); err != nil {
		return nil, false, err
	}
	if err := ripgrepWaitError(waitErr, truncated, stderr, "ripgrep search failed"); err != nil {
		return nil, false, err
	}
	return matches, truncated, nil
}
func fallbackSearch(ctx context.Context, pattern, root, include string, limit int) ([]grepMatch, bool, error) {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, false, err
	}
	var matches []grepMatch
	truncated := false
	files, _, err := findFiles(ctx, root, include, 10000)
	if err != nil {
		return nil, false, err
	}
	for _, path := range files {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		f, err := openFile(ctx, path)
		if err != nil {
			continue
		}
		matches, truncated, err = scanFallbackFile(f, path, regex, matches, limit)
		if err != nil {
			return nil, false, err
		}
		if truncated {
			break
		}
	}
	return matches, truncated, nil
}
func findFiles(ctx context.Context, root, pattern string, limit int) ([]string, bool, error) {
	root = absolutePath(root)
	if _, err := exec.LookPath("rg"); err == nil {
		return ripgrepFiles(ctx, root, pattern, limit)
	}
	collector := discoveryFileCollector{ctx: ctx, root: root, pattern: pattern, limit: limit}
	err := filepath.WalkDir(root, collector.visit)
	files, truncated := collector.files, collector.truncated
	return files, truncated, err
}

func ripgrepFiles(ctx context.Context, root, pattern string, limit int) ([]string, bool, error) {
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := []string{"rg", "--files", "--no-config", "--no-require-git", "--null", "--sort", "path"}
	if pattern != "" {
		args = append(args, "--glob", pattern)
	}
	args = append(args, "--", root)
	cwd := root
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		cwd = filepath.Dir(root)
	}
	cmd, err := shell.Command(ctx, cwd, commandString(args))
	if err != nil {
		return nil, false, err
	}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	stderr := &shell.Buffer{Limit: 4096}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	scanner := bufio.NewScanner(pipe)
	scanner.Split(splitNullPaths)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	var paths []string
	truncated := false
	for scanner.Scan() {
		path := scanner.Text()
		if ExclusionReason(discoveryPolicyRoot(root), path) != "" {
			continue
		}
		if len(paths) == limit {
			truncated = true
			cancel()
			break
		}
		paths = append(paths, path)
	}
	scanErr := scanner.Err()
	if scanErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if parent.Err() != nil {
		return nil, false, parent.Err()
	}
	if scanErr != nil {
		return nil, false, scanErr
	}
	if err := ripgrepWaitError(waitErr, truncated, stderr, "ripgrep file listing failed"); err != nil {
		return nil, false, err
	}
	return paths, truncated, nil
}

func bufferHasOutput(b *shell.Buffer) bool { s, _ := b.Snapshot(); return len(s) > 0 }

func formatGrepMatches(matches []grepMatch, truncated bool) string {
	if len(matches) == 0 {
		return "No files found"
	}
	var output strings.Builder
	fmt.Fprintf(&output, "Found %d matches\n", len(matches))
	currentFile := ""
	for _, match := range matches {
		if currentFile != match.path {
			if currentFile != "" {
				output.WriteByte('\n')
			}
			currentFile = match.path
			fmt.Fprintf(&output, "%s:\n", match.path)
		}
		if match.lineNum > 0 {
			fmt.Fprintf(&output, "  Line %d: %s\n", match.lineNum, match.lineText)
		} else {
			fmt.Fprintf(&output, "  %s\n", match.path)
		}
	}
	if truncated {
		output.WriteString("\n(Results are truncated. Consider using a more specific path or pattern.)")
	}
	return output.String()
}

type ripgrepText struct {
	Text  string `json:"text"`
	Bytes string `json:"bytes"`
}

func (v ripgrepText) decode() string {
	if v.Bytes != "" {
		raw, _ := base64.StdEncoding.DecodeString(v.Bytes)
		return string(raw)
	}
	return v.Text
}

type ripgrepMatchEvent struct {
	Type string
	Data struct {
		Path, Lines ripgrepText
		LineNumber  int `json:"line_number"`
	}
}

func scanRipgrepMatches(ctx context.Context, rootPath string, scanner *bufio.Scanner, limit int) ([]grepMatch, bool, error) {
	var matches []grepMatch
	truncated := false

	var parseErr error
	for scanner.Scan() {
		var event ripgrepMatchEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			parseErr = err
			break
		}
		if event.Type != "match" {
			continue
		}
		path := event.Data.Path.decode()
		if ExclusionReason(discoveryPolicyRoot(rootPath), path) != "" {
			continue
		}
		if _, err := authorizeFile(ctx, path, "read"); err != nil {
			continue
		}
		if len(matches) == limit {
			truncated = true
			break
		}
		text := strings.TrimRight(event.Data.Lines.decode(), "\r\n")
		if len(text) > MaxLineLength {
			text = text[:MaxLineLength] + "..."
		}
		matches = append(matches, grepMatch{path: path, lineNum: event.Data.LineNumber, lineText: text})
	}
	return matches, truncated, parseErr
}

func ripgrepWaitError(waitErr error, truncated bool, stderr *shell.Buffer, label string) error {
	if waitErr != nil && !truncated {
		if exit, ok := waitErr.(*exec.ExitError); !ok || exit.ExitCode() != 1 || bufferHasOutput(stderr) {
			return fmt.Errorf("%s", label)
		}
	}
	return nil
}

func splitNullPaths(data []byte, eof bool) (int, []byte, error) {

	for i, c := range data {
		if c == 0 {
			return i + 1, data[:i], nil
		}
	}
	if eof && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil

}

func scanFallbackFile(f *os.File, path string, regex *regexp.Regexp, matches []grepMatch, limit int) ([]grepMatch, bool, error) {
	truncated := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		if strings.ContainsRune(text, 0) {
			break
		}
		if regex.MatchString(text) {
			if len(matches) == limit {
				truncated = true
				break
			}
			if len(text) > MaxLineLength {
				text = text[:MaxLineLength] + "..."
			}
			matches = append(matches, grepMatch{path: path, lineNum: line, lineText: text})
		}
	}
	scanErr := scanner.Err()
	f.Close()
	if scanErr != nil {
		return nil, false, scanErr
	}

	return matches, truncated, nil
}

type discoveryFileCollector struct {
	ctx           context.Context
	root, pattern string
	limit         int
	files         []string
	truncated     bool
}

func (c *discoveryFileCollector) visit(path string, d fs.DirEntry, err error) error {

	if err != nil {
		return err
	}
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	if path != c.root && ExclusionReason(discoveryPolicyRoot(c.root), path) != "" {
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}
	if d.Type()&os.ModeSymlink != 0 {
		return nil
	}
	if d.IsDir() {
		return nil
	}
	matched, err := discoveryPatternMatches(c.root, path, d.Name(), c.pattern)
	if err != nil {
		return err
	}
	if !matched {
		return nil
	}
	if len(c.files) == c.limit {
		c.truncated = true
		return filepath.SkipAll
	}
	if ExclusionReason(discoveryPolicyRoot(c.root), path) == "" {
		c.files = append(c.files, path)
	}
	return nil

}

func discoveryPatternMatches(root, path, name, pattern string) (bool, error) {
	if pattern == "" {
		return true, nil
	}

	relative, _ := filepath.Rel(root, path)
	match, err := doublestar.Match(pattern, filepath.ToSlash(relative))
	if err != nil {
		return false, err
	}
	if !match {
		match, _ = doublestar.Match(pattern, name)
	}
	if !match {
		return false, nil
	}

	return true, nil
}
