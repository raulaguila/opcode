package tools

import (
	"context"
	"encoding/hex"
	"encoding/json"
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
		if result0, result1, handled := visibleSourceReadReceiptView(name, &content, &r, &ok, &first, &last); handled {
			return result0, result1
		}
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
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return false
	}
	root, err := canonicalPath(config.WorkingDirectory())
	if err != nil {
		return false
	}
	path, err := canonicalPath(receipt.Path)
	if err != nil || !inside(root, path) || path != absolutePath(receipt.Path) {
		return false
	}
	// A policy with no permission service fails closed for sensitive paths. Root
	// handles and the regular-file check below preserve the existing I/O boundary.
	ctx = context.WithValue(ctx, accessKey{}, &accessPolicy{tool: "citation_validation", approved: map[string]bool{}})
	checked, err := authorizeFile(ctx, path, "read")
	if err != nil || filepath.Clean(checked) != filepath.Clean(path) {
		return false
	}
	raw, err := readFile(ctx, path)
	return err == nil && ctx.Err() == nil && receipt.StartLine > 0 && receipt.EndLine >= receipt.StartLine && receipt.EndLine <= strings.Count(string(raw), "\n")+1 && indexDigest(string(raw)) == receipt.Hash
}

func visibleSourceReadReceiptView(name string, content *string, r *SourceReadReceipt, ok *bool, first *int, last *int) (extractedResult0 SourceReadReceipt, extractedResult1 bool, handled bool) {
	if name == "read_symbol" {
		var page struct {
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte((*content)), &page) != nil {
			return (*r), false, true
		}
		(*content) = page.Content
	} else {
		// Diagnostics/notes outside the source cannot enlarge the interval.
		_, (*content), (*ok) = strings.Cut((*content), "<file>\n")
		if !(*ok) {
			return (*r), false, true
		}
		(*content), _, _ = strings.Cut((*content), "\n</file>")
	}
	for _, match := range numberedSourceLine.FindAllStringSubmatch((*content), 2001) {
		line, _ := strconv.Atoi(match[1])
		if (*first) == 0 {
			(*first) = line
		}
		if line != (*first) && line != (*last)+1 {
			return (*r), false, true
		}
		(*last) = line
	}
	return extractedResult0, extractedResult1, false
}
