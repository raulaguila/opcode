package tools

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

type ReadPageParams struct {
	File         string `json:"file_path"`
	Offset       int    `json:"offset"`
	Column       int    `json:"column"`
	Limit        int    `json:"limit"`
	MaxBytes     int    `json:"max_bytes"`
	ExpectedHash string `json:"expected_hash"`
}
type readPageTool struct{}

func NewReadPageTool() BaseTool { return &readPageTool{} }
func (*readPageTool) Info() ToolInfo {
	return ToolInfo{Name: "read_lines", Description: "Read current source in byte-bounded pages. Zero-based offset/column (UTF-8 byte column). Return file_hash plus next_offset/next_column; send expected_hash on subsequent pages to reject external edits. Long lines continue without silently dropping content. Maximum 8 KiB/page, 200 lines; complete file hash limit 16 MiB.", Required: []string{"file_path"}, Parameters: map[string]any{
		"file_path": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer", "minimum": 0}, "column": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200}, "max_bytes": map[string]any{"type": "integer", "minimum": 64, "maximum": 8192}, "expected_hash": map[string]any{"type": "string"},
	}}
}
func (t *readPageTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var p ReadPageParams
	if json.Unmarshal([]byte(call.Input), &p) != nil || p.File == "" || p.Offset < 0 || p.Column < 0 || p.Limit < 0 || p.Limit > 200 || p.MaxBytes < 0 || p.MaxBytes > 8192 || p.MaxBytes > 0 && p.MaxBytes < 64 || p.ExpectedHash != "" && len(p.ExpectedHash) != 64 {
		return NewTextErrorResponse("invalid paged read parameters"), nil
	}
	path, err := authorizeFile(ctx, p.File, "read")
	if err != nil {
		return ToolResponse{}, err
	}
	raw, err := readFile(ctx, path)
	if err != nil {
		return ToolResponse{}, err
	}
	return readSourcePage(ctx, path, raw, p)
}
func readSourcePage(ctx context.Context, path string, raw []byte, p ReadPageParams) (ToolResponse, error) {
	if err := ctx.Err(); err != nil {
		return ToolResponse{}, err
	}
	if !utf8.Valid(raw) || strings.IndexByte(string(raw), 0) >= 0 {
		return NewTextErrorResponse("source must be UTF-8 text"), nil
	}
	digest := indexDigest(string(raw))
	if p.ExpectedHash != "" && p.ExpectedHash != digest {
		return NewTextErrorResponse("file changed since previous page; restart with a fresh read and hash"), nil
	}
	lines := strings.Split(string(raw), "\n")
	if p.Offset >= len(lines) || p.Column > len(lines[p.Offset]) {
		return NewTextErrorResponse("offset/column exceeds source range"), nil
	}
	if p.Column < len(lines[p.Offset]) && !utf8.RuneStart(lines[p.Offset][p.Column]) {
		return NewTextErrorResponse("column splits a UTF-8 character"), nil
	}
	limit := p.Limit
	if limit == 0 {
		limit = 100
	}
	budget := p.MaxBytes
	if budget == 0 {
		budget = 4096
	}
	line, column := p.Offset, p.Column
	var content strings.Builder
	for line < len(lines) && line-p.Offset < limit && content.Len() < budget {
		remaining := lines[line][column:]
		take := min(len(remaining), budget-content.Len())
		for take > 0 && take < len(remaining) && !utf8.RuneStart(remaining[take]) {
			take--
		}
		if take == 0 && len(remaining) > 0 {
			break
		}
		content.WriteString(remaining[:take])
		column += take
		if column < len(lines[line]) {
			break
		}
		if line == len(lines)-1 {
			line++
			column = 0
			break
		}
		if content.Len() == budget {
			break
		}
		content.WriteByte('\n')
		line++
		column = 0
	}
	recordFileRead(path)
	response := jsonToolResult(map[string]any{"file_path": path, "file_hash": digest, "hash_scope": "whole file (max 16 MiB)", "offset": p.Offset, "column": p.Column, "content": content.String(), "bytes": content.Len(), "next_offset": line, "next_column": column, "more": line < len(lines)})
	if content.Len() > 0 {
		end := p.Offset + strings.Count(strings.TrimSuffix(content.String(), "\n"), "\n") + 1
		response = sourceReadMetadata(response, path, raw, p.Offset+1, end)
	}
	return response, nil
}
