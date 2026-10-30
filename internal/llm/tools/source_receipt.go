package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/raulaguila/opcode/internal/config"
)

// SourceReadReceipt describes source actually returned by a native reading
// tool. It is not an authorization, an index hit or proof of an interpretation.
// Line numbers are one-based and inclusive; a partial line is still a read of
// that line, not evidence for the omitted part of its content.
type SourceReadReceipt struct {
	Path      string `json:"file_path"`
	Hash      string `json:"file_hash"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func sourceReadMetadata(response ToolResponse, path string, raw []byte, start, end int) ToolResponse {
	return WithResponseMetadata(response, map[string]any{"source_read": SourceReadReceipt{Path: path, Hash: indexDigest(string(raw)), StartLine: start, EndLine: end}})
}

// ParseSourceReadReceipt accepts only the bounded native metadata shape. Its
// caller must additionally require a successful native source-reading call.
func ParseSourceReadReceipt(metadata string) (SourceReadReceipt, bool) {
	var envelope struct {
		Receipt SourceReadReceipt `json:"source_read"`
	}
	if len(metadata) > 4096 || json.Unmarshal([]byte(metadata), &envelope) != nil {
		return SourceReadReceipt{}, false
	}
	r := envelope.Receipt
	_, err := hex.DecodeString(r.Hash)
	return r, r.Path != "" && len(r.Path) <= 4096 && len(r.Hash) == 64 && err == nil && r.StartLine > 0 && r.EndLine >= r.StartLine
}

var numberedSourceLine = regexp.MustCompile(`(?m)^\s*([0-9]{1,9})\|`)

// VisibleSourceReadReceipt restricts the receipt to source that survived the
// result budget/redaction path and was actually delivered to the model.
func VisibleSourceReadReceipt(name, content, metadata string) (SourceReadReceipt, bool) {
	r, ok := ParseSourceReadReceipt(metadata)
	if !ok {
		return r, false
	}
	first, last := 0, 0
	switch name {
	case "read_lines":
		var page struct {
			Path    string `json:"file_path"`
			Hash    string `json:"file_hash"`
			Offset  int    `json:"offset"`
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(content), &page) != nil || page.Content == "" || page.Hash != r.Hash || absolutePath(page.Path) != absolutePath(r.Path) {
			return r, false
		}
		first = page.Offset + 1
		last = first + strings.Count(strings.TrimSuffix(page.Content, "\n"), "\n")
	case "view", "read_symbol":
		interval, visible := visibleSourceReadReceiptView(name, content)
		if !visible {
			return r, false
		}
		first, last = interval.first, interval.last
	default:
		return r, false
	}
	if first < r.StartLine || first > r.EndLine || last < first {
		return r, false
	}
	r.StartLine, r.EndLine = first, min(last, r.EndLine)
	return r, true
}

// SourceReadCurrent checks a previous receipt without granting any new access.
// Only non-sensitive regular workspace files can be verified automatically;
// external/sensitive reads retain their original receipt but are not certified
// as current. No permission prompt, integration or model invocation occurs.
func SourceReadCurrent(ctx context.Context, receipt SourceReadReceipt) bool {
	return NewSourceReadVerifier(ctx).Current(receipt)
}

// SourceReadVerifier belongs to one validation phase. Its observations are never
// reused across turns, repairs or calls to SourceReadCurrent.
type SourceReadVerifier struct {
	ctx       context.Context
	buffer    []byte
	snapshots map[string]sourceReadSnapshot
}
type sourceReadSnapshot struct {
	lines int
	hash  string
	valid bool
}

func NewSourceReadVerifier(ctx context.Context) *SourceReadVerifier {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, accessKey{}, &accessPolicy{tool: "citation_validation", approved: map[string]bool{}})
	return &SourceReadVerifier{ctx: ctx}
}

func (v *SourceReadVerifier) Current(receipt SourceReadReceipt) bool {
	if v.ctx.Err() != nil || receipt.StartLine < 1 || receipt.EndLine < receipt.StartLine {
		return false
	}
	path, err := currentSourcePath(v.ctx, receipt.Path)
	if err != nil {
		return false
	}
	snapshot, ok := v.snapshots[path]
	if !ok {
		snapshot = v.read(path)
		if v.snapshots == nil {
			v.snapshots = map[string]sourceReadSnapshot{}
		}
		if len(v.snapshots) < 32 {
			v.snapshots[path] = snapshot
		}
	}
	return snapshot.valid && snapshot.hash == receipt.Hash && receipt.EndLine <= snapshot.lines && v.ctx.Err() == nil
}

func currentSourcePath(ctx context.Context, input string) (string, error) {
	root, err := canonicalPath(config.WorkingDirectory())
	if err != nil {
		return "", err
	}
	path, err := canonicalPath(input)
	if err != nil || !inside(root, path) || path != absolutePath(input) {
		return "", fmt.Errorf("source is not a regular workspace path")
	}
	checked, err := authorizeFile(ctx, path, "read")
	if err != nil {
		return "", err
	}
	if filepath.Clean(checked) != filepath.Clean(path) {
		return "", fmt.Errorf("source path changed")
	}
	return path, nil
}

func (v *SourceReadVerifier) read(path string) sourceReadSnapshot {
	file, err := openFile(v.ctx, path)
	if err != nil {
		return sourceReadSnapshot{}
	}
	defer file.Close()
	if v.buffer == nil {
		v.buffer = make([]byte, 32<<10)
	}
	digest := sha256.New()
	reader := io.LimitReader(file, 16<<20+1)
	var total int64
	lines := 1
	for {
		if v.ctx.Err() != nil {
			return sourceReadSnapshot{}
		}
		n, err := reader.Read(v.buffer)
		total += int64(n)
		if total > 16<<20 {
			return sourceReadSnapshot{}
		}
		digest.Write(v.buffer[:n])
		lines += bytes.Count(v.buffer[:n], []byte{'\n'})
		if err == io.EOF {
			break
		}
		if err != nil {
			return sourceReadSnapshot{}
		}
	}
	return sourceReadSnapshot{lines: lines, hash: hex.EncodeToString(digest.Sum(nil)), valid: true}
}

type sourceReadInterval struct{ first, last int }

func visibleSourceReadReceiptView(name, content string) (sourceReadInterval, bool) {
	source, ok := sourceReadViewContent(name, content)
	if !ok {
		return sourceReadInterval{}, false
	}
	var interval sourceReadInterval
	for _, match := range numberedSourceLine.FindAllStringSubmatch(source, 2001) {
		line, _ := strconv.Atoi(match[1])
		if interval.first == 0 {
			interval.first = line
		}
		if line != interval.first && line != interval.last+1 {
			return sourceReadInterval{}, false
		}
		interval.last = line
	}
	return interval, interval.first > 0
}

func sourceReadViewContent(name, content string) (string, bool) {
	if name == "read_symbol" {
		var page struct {
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(content), &page) != nil {
			return "", false
		}
		return page.Content, true
	}
	// Diagnostics and notes outside the native source cannot enlarge its interval.
	_, source, ok := strings.Cut(content, "<file>\n")
	if !ok {
		return "", false
	}
	source, _, _ = strings.Cut(source, "\n</file>")
	return source, true
}
