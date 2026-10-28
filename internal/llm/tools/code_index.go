package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/raulaguila/opcode/internal/localstate"
)

var codeIndexLocks sync.Map

type codeIndexTool struct{}

func NewCodeIndexTool() BaseTool { return &codeIndexTool{} }
func (*codeIndexTool) Info() ToolInfo {
	return ToolInfo{Name: "search_code_index", Description: "Search a private incremental SQLite FTS5/BM25 workspace index. Refreshes on each call, respects rg ignore rules, omits secret/dependency/storage/link paths and redacts credentials before storage. Results require current file authorization and content verification; snippets are navigation, never edit-read evidence. Requires rg. Bounded to 10000 files, 256 KiB/file, 20 MiB scanned text and 50 results; refine the workspace if limits are exceeded.", Parameters: map[string]any{"query": map[string]any{"type": "string", "maxLength": 512}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50}}, Required: []string{"query"}}
}
func (*codeIndexTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var input struct {
		Query string
		Limit int
	}
	if json.Unmarshal([]byte(call.Input), &input) != nil || strings.TrimSpace(input.Query) == "" || len(input.Query) > 512 || input.Limit < 0 || input.Limit > 50 {
		return NewTextErrorResponse("query must contain 1–512 bytes and limit must be 1–50"), nil
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	if err := ctx.Err(); err != nil {
		return ToolResponse{}, err
	}
	if _, err := exec.LookPath("rg"); err != nil {
		return NewTextErrorResponse("ripgrep (rg) is required for code indexing; install it explicitly or use the regular file tools. No index was created."), nil
	}
	root, err := canonicalPath(config.WorkingDirectory())
	if err != nil {
		return ToolResponse{}, err
	}
	semaphore := make(chan struct{}, 1)
	semaphore <- struct{}{}
	lock, _ := codeIndexLocks.LoadOrStore(root, semaphore)
	semaphore = lock.(chan struct{})
	select {
	case <-ctx.Done():
		return ToolResponse{}, ctx.Err()
	case <-semaphore:
	}
	defer func() { semaphore <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return ToolResponse{}, err
	}
	db, err := openCodeIndex(ctx, root)
	if err != nil {
		return ToolResponse{}, err
	}
	defer db.Close()
	updated, err := refreshCodeIndex(ctx, db, root)
	if err != nil {
		return ToolResponse{}, err
	}
	terms := strings.FieldsFunc(input.Query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	if len(terms) == 0 || len(terms) > 32 {
		return NewTextErrorResponse("use 1–32 code search terms"), nil
	}
	for i := range terms {
		terms[i] = `"` + terms[i] + `"`
	}
	rows, err := db.QueryContext(ctx, `SELECT path, start_line, content, digest, bm25(chunks, 3.0, 0.0, 1.0, 0.0) FROM chunks WHERE chunks MATCH ? ORDER BY bm25(chunks, 3.0, 0.0, 1.0, 0.0) LIMIT ?`, strings.Join(terms, " AND "), input.Limit+1)
	if err != nil {
		return ToolResponse{}, err
	}
	type hit struct {
		Path    string  `json:"path"`
		Line    int     `json:"start_line"`
		Content string  `json:"content"`
		Rank    float64 `json:"rank"`
	}
	var candidates []struct {
		hit
		digest string
	}
	for rows.Next() {
		var c struct {
			hit
			digest string
		}
		if err := rows.Scan(&c.Path, &c.Line, &c.Content, &c.digest, &c.Rank); err != nil {
			rows.Close()
			return ToolResponse{}, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ToolResponse{}, err
	}
	more := len(candidates) > input.Limit
	if more {
		candidates = candidates[:input.Limit]
	}
	items := []hit{}
	for _, c := range candidates {
		path := filepath.Join(root, c.Path)
		if !inside(root, path) || shell.SensitivePath(path) {
			continue
		}
		// Retrieval never trusts persisted authorization or a stale snapshot.
		resolved, err := authorizeFile(ctx, path, "read")
		if err != nil {
			return ToolResponse{}, err
		}
		if resolved != path {
			continue
		}
		raw, err := readFile(ctx, resolved)
		if err != nil {
			return ToolResponse{}, err
		}
		if indexDigest(redact(string(raw))) != c.digest {
			continue
		}
		c.Content = redact(c.Content)
		items = append(items, c.hit)
	}
	return jsonToolResult(map[string]any{"results": items, "more": more, "updated_files": updated, "navigation_only": true}), nil
}
func indexDigest(text string) string {
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:])
}
func openCodeIndex(ctx context.Context, root string) (*sql.DB, error) {
	cfg := config.Get()
	if cfg == nil || cfg.Data.Directory == "" {
		return nil, fmt.Errorf("private data directory is not configured")
	}
	dir := filepath.Join(cfg.Data.Directory, "code-index")
	privateRoot, err := localstate.OpenPrivateRoot(dir, true)
	if err != nil {
		return nil, err
	}
	defer privateRoot.Close()
	if err := privateRoot.Chmod(".", 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, indexDigest(root)+".sqlite")
	f, err := privateRoot.OpenFile(filepath.Base(path), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		err = f.Close()
	} else if os.IsExist(err) {
		info, statErr := privateRoot.Lstat(filepath.Base(path))
		if statErr != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("index must be a regular private file")
		}
		err = privateRoot.Chmod(filepath.Base(path), 0600)
	}
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, query := range []string{`PRAGMA journal_mode=MEMORY`, `PRAGMA secure_delete=ON`, `PRAGMA max_page_count=32768`, `CREATE TABLE IF NOT EXISTS files(path TEXT PRIMARY KEY, digest TEXT NOT NULL)`, `CREATE VIRTUAL TABLE IF NOT EXISTS chunks USING fts5(path, start_line UNINDEXED, content, digest UNINDEXED)`} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}
func refreshCodeIndex(ctx context.Context, db *sql.DB, root string) (int, error) {
	paths, truncated, err := ripgrepFiles(ctx, root, "", 10000)
	if err != nil {
		return 0, fmt.Errorf("index file discovery: %w", err)
	}
	if truncated {
		return 0, fmt.Errorf("workspace exceeds 10000-file index limit")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TEMP TABLE seen(path TEXT PRIMARY KEY)`); err != nil {
		return 0, err
	}
	updated, total := 0, 0
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		relative, _ := filepath.Rel(root, path)
		if !inside(root, path) || strings.HasPrefix(filepath.ToSlash(relative), ".opcode-worktrees/") || shouldSkip(path, nil) || shell.SensitivePath(path) {
			continue
		}
		if cfg := config.Get(); cfg != nil && cfg.Data.Directory != "" && inside(absolutePath(cfg.Data.Directory), path) {
			continue
		}
		resolved, err := authorizeFile(ctx, path, "read")
		if err != nil {
			return 0, err
		}
		if resolved != path {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			return 0, err
		}
		if !info.Mode().IsRegular() || info.Size() > 256<<10 {
			continue
		}
		raw, err := readFile(ctx, path)
		if err != nil {
			return 0, err
		}
		if len(raw) > 256<<10 || bytes.IndexByte(raw, 0) >= 0 || !utf8.Valid(raw) {
			continue
		}
		total += len(raw)
		if total > 20<<20 {
			return 0, fmt.Errorf("workspace text exceeds 20 MiB index scan limit")
		}
		text := redact(string(raw))
		digest := indexDigest(text)
		rel, _ := filepath.Rel(root, path)
		if _, err := tx.ExecContext(ctx, `INSERT INTO seen(path) VALUES(?)`, rel); err != nil {
			return 0, err
		}
		var previous string
		err = tx.QueryRowContext(ctx, `SELECT digest FROM files WHERE path=?`, rel).Scan(&previous)
		if err != nil && err != sql.ErrNoRows {
			return 0, err
		}
		if previous == digest {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE path=?`, rel); err != nil {
			return 0, err
		}
		lines := strings.Split(text, "\n")
		for start := 0; start < len(lines); {
			end, size := start, 0
			for end < len(lines) && end-start < 80 && size+len(lines[end])+1 <= 8192 {
				size += len(lines[end]) + 1
				end++
			}
			if end == start {
				end++
			}
			chunk := strings.Join(lines[start:end], "\n")
			if len(chunk) > 8192 {
				chunk = string([]rune(chunk)[:min(2048, len([]rune(chunk)))])
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO chunks(path,start_line,content,digest) VALUES(?,?,?,?)`, rel, start+1, chunk, digest); err != nil {
				return 0, err
			}
			start = end
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO files(path,digest) VALUES(?,?) ON CONFLICT(path) DO UPDATE SET digest=excluded.digest`, rel, digest); err != nil {
			return 0, err
		}
		updated++
	}
	for _, query := range []string{`DELETE FROM chunks WHERE path NOT IN (SELECT path FROM seen)`, `DELETE FROM files WHERE path NOT IN (SELECT path FROM seen)`} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE seen`); err != nil {
		return 0, err
	}
	return updated, tx.Commit()
}
