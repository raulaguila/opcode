package diff

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aymanbagabas/go-udiff"
	"github.com/raulaguila/opcode/internal/config"
)

// -------------------------------------------------------------------------
// Core Types
// -------------------------------------------------------------------------

// LineType represents the kind of line in a diff.
type LineType int

const (
	LineContext LineType = iota // Line exists in both files
	LineAdded                   // Line added in the new file
	LineRemoved                 // Line removed from the old file
)

// Segment represents a portion of a line for intra-line highlighting
type Segment struct {
	Start int
	End   int
	Type  LineType
	Text  string
}

// DiffLine represents a single line in a diff
type DiffLine struct {
	OldLineNo int       // Line number in old file (0 for added lines)
	NewLineNo int       // Line number in new file (0 for removed lines)
	Kind      LineType  // Type of line (added, removed, context)
	Content   string    // Content of the line
	Segments  []Segment // Segments for intraline highlighting
}

// Hunk represents a section of changes in a diff
type Hunk struct {
	Header string
	Lines  []DiffLine
}

// DiffResult contains the parsed result of a diff
type DiffResult struct {
	OldFile string
	NewFile string
	Hunks   []Hunk
}

// linePair represents a pair of lines for side-by-side display
type linePair struct {
	left  *DiffLine
	right *DiffLine
}

// -------------------------------------------------------------------------
// Parse Configuration
// -------------------------------------------------------------------------

// ParseConfig configures the behavior of diff parsing
type ParseConfig struct {
	ContextSize int // Number of context lines to include
}

// ParseOption modifies a ParseConfig
type ParseOption func(*ParseConfig)

// -------------------------------------------------------------------------
// Side-by-Side Configuration
// -------------------------------------------------------------------------

// SideBySideConfig configures the rendering of side-by-side diffs
type SideBySideConfig struct {
	TotalWidth int
}

// SideBySideOption modifies a SideBySideConfig
type SideBySideOption func(*SideBySideConfig)

// -------------------------------------------------------------------------
// Diff Parsing
// -------------------------------------------------------------------------

// -------------------------------------------------------------------------
// Syntax Highlighting
// -------------------------------------------------------------------------

// -------------------------------------------------------------------------
// Rendering Functions
// -------------------------------------------------------------------------

// -------------------------------------------------------------------------
// Public API
// -------------------------------------------------------------------------

// GenerateDiff creates a unified diff from two file contents
// displayContent keeps binary/invalid UTF-8 out of the text diff library, whose
// Unified helper calls log.Fatal for invalid text. Snapshots and committed bytes
// remain untouched; only approval/UI presentation uses this representation.
func displayContent(content string) string {
	if utf8.ValidString(content) && !strings.ContainsRune(content, 0) {
		return content
	}
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("[conteúdo binário: %d bytes; SHA256 %x]\n", len(content), hash)
}

func GenerateDiff(beforeContent, afterContent, fileName string) (string, int, int) {
	beforeContent = displayContent(beforeContent)
	afterContent = displayContent(afterContent)
	// remove the cwd prefix and ensure consistent path format
	// this prevents issues with absolute paths in different environments
	cwd := config.WorkingDirectory()
	fileName = strings.TrimPrefix(fileName, cwd)
	fileName = strings.TrimPrefix(fileName, "/")

	var (
		unified   = udiff.Unified("a/"+fileName, "b/"+fileName, beforeContent, afterContent)
		additions = 0
		removals  = 0
	)

	lines := strings.SplitSeq(unified, "\n")
	for line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			additions++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removals++
		}
	}

	return unified, additions, removals
}
