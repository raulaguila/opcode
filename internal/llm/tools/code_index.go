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
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/localstate"
)

var codeIndexLocks sync.Map

type codeIndexTool struct{}

func NewCodeIndexTool() BaseTool { return &codeIndexTool{} }
func (*codeIndexTool) Info() ToolInfo {
	params := scopeParameters()
	params["query"] = map[string]any{"type": "string", "maxLength": 512}
	params["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 12}
	params["kind"] = map[string]any{"type": "string", "description": "Declaration kind (Go parser), or text windows for other languages."}
	return ToolInfo{Name: "search_code_index", Description: "Search a private local FTS5/BM25 index with scope filters. Returns 6 short previews by default, maximum 12; refine the query when more=true. Go symbols are parsed; other languages use text windows. Refreshes ignore rules each call and reuses unchanged metadata for at most 60 seconds. Verifies current file hashes before returning hits. Previews are navigation, not edit-read evidence; use view/read_symbol for source. Requires rg; maximum 20000 files, 256 KiB/file and 20 MiB text.", Parameters: params, Required: []string{"query"}}
}
func (*codeIndexTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var input struct {
		Query string
		Limit int
		Kind  string
		retrievalScope
	}
	if json.Unmarshal([]byte(call.Input), &input) != nil || strings.TrimSpace(input.Query) == "" || len(input.Query) > 512 || input.Limit < 0 || input.Limit > 12 || len(input.Kind) > 40 {
		return NewTextErrorResponse("query must contain 1–512 bytes and limit must be 1–12"), nil
	}
	if err := input.retrievalScope.validate(); err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	if input.Limit == 0 {
		input.Limit = 6
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
	scopeRoot, err := authorizeFile(ctx, input.Path, "read")
	if err != nil {
		return ToolResponse{}, err
	}
	if !inside(root, scopeRoot) {
		return NewTextErrorResponse("index scope must be inside the current workspace"), nil
	}
	relativeScope, _ := filepath.Rel(root, scopeRoot)
	relativeScope = filepath.ToSlash(relativeScope)
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
	db, err := openCodeIndex(ctx, root)
	if err != nil {
		return ToolResponse{}, err
	}
	defer db.Close()
	stats, err := refreshCodeIndex(ctx, db, root)
	if err != nil {
		return ToolResponse{}, err
	}
	scoped := relativeScope != "." || input.Glob != "" || input.Language != "" || input.Extension != ""
	if scoped {
		if _, err := db.ExecContext(ctx, `CREATE TEMP TABLE scope_paths(path TEXT PRIMARY KEY)`); err != nil {
			return ToolResponse{}, err
		}
		paths, err := db.QueryContext(ctx, `SELECT path FROM files`)
		if err != nil {
			return ToolResponse{}, err
		}
		var allowed []string
		for paths.Next() {
			var path string
			if err := paths.Scan(&path); err != nil {
				paths.Close()
				return ToolResponse{}, err
			}
			if (relativeScope == "." || path == relativeScope || strings.HasPrefix(path, relativeScope+"/")) && input.matches(path) {
				allowed = append(allowed, path)
			}
		}
		err = paths.Err()
		paths.Close()
		if err != nil {
			return ToolResponse{}, err
		}
		for _, path := range allowed {
			if _, err := db.ExecContext(ctx, `INSERT INTO scope_paths VALUES(?)`, path); err != nil {
				return ToolResponse{}, err
			}
		}
	}
	terms := codeQueryTerms(input.Query)
	if len(terms) == 0 || len(terms) > 60 {
		return NewTextErrorResponse("use 1–60 code search terms"), nil
	}
	for i := range terms {
		terms[i] = `"` + terms[i] + `"`
	}
	query := `SELECT path,symbol,signature,kind,language,start_line,end_line,content,digest,bm25(chunks,3,8,4,1,1,0,0,0,0,0) FROM chunks WHERE chunks MATCH ?`
	args := []any{strings.Join(terms, " OR ")}
	if scoped {
		query += ` AND path IN (SELECT path FROM scope_paths)`
	}
	if input.Kind != "" {
		query += ` AND kind=?`
		args = append(args, input.Kind)
	}
	query += ` ORDER BY (lower(symbol)=? OR lower(path)=?) DESC, (kind='text'), bm25(chunks,3,8,4,1,1,0,0,0,0,0),path,start_line LIMIT 120`
	args = append(args, strings.ToLower(strings.TrimSpace(input.Query)), strings.ToLower(strings.TrimSpace(input.Query)))
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return ToolResponse{}, err
	}
	type hit struct {
		Path      string  `json:"path"`
		Symbol    string  `json:"symbol"`
		Signature string  `json:"signature"`
		Kind      string  `json:"kind"`
		Language  string  `json:"language"`
		Start     int     `json:"start_line"`
		End       int     `json:"end_line"`
		Preview   string  `json:"preview"`
		Hash      string  `json:"file_hash"`
		Rank      float64 `json:"rank"`
	}
	var candidates []hit
	for rows.Next() {
		var c hit
		if err := rows.Scan(&c.Path, &c.Symbol, &c.Signature, &c.Kind, &c.Language, &c.Start, &c.End, &c.Preview, &c.Hash, &c.Rank); err != nil {
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
	items := []hit{}
	more := false
	resultBytes := 0
	verified := map[string]string{}
	for _, c := range candidates {
		path := filepath.Join(root, c.Path)
		if !retrievalPathAllowed(root, path, nil) {
			continue
		}
		duplicate := false
		for _, old := range items {
			if old.Path == c.Path && max(old.Start, c.Start) <= min(old.End, c.End) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		digest, ok := verified[path]
		if !ok {
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
			digest = indexDigest(redact(string(raw)))
			verified[path] = digest
		}
		if digest != c.Hash {
			continue
		}
		if len(items) == input.Limit {
			more = true
			break
		}
		c.Preview = compactPreview(redact(c.Preview), 240)
		c.Signature = compactPreview(redact(c.Signature), 160)
		encoded, _ := json.Marshal(c)
		if resultBytes+len(encoded) > 7600 {
			more = true
			break
		}
		resultBytes += len(encoded)
		items = append(items, c)
	}
	return jsonToolResult(map[string]any{"results": items, "more": more || len(candidates) == 120, "updated_files": stats.Updated, "reused_files": stats.Reused, "scan_reads": stats.Read, "navigation_only": true}), nil
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
	path := filepath.Join(dir, indexDigest(root)+"-v2.sqlite")
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
	for _, query := range []string{`PRAGMA journal_mode=MEMORY`, `PRAGMA secure_delete=ON`, `PRAGMA max_page_count=32768`, `CREATE TABLE IF NOT EXISTS files(path TEXT PRIMARY KEY, digest TEXT NOT NULL, stamp TEXT NOT NULL, verified INTEGER NOT NULL, policy TEXT NOT NULL)`, `CREATE VIRTUAL TABLE IF NOT EXISTS chunks USING fts5(path,symbol,signature,content,terms,kind UNINDEXED,language UNINDEXED,start_line UNINDEXED,end_line UNINDEXED,digest UNINDEXED)`} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

type indexRefresh struct{ Updated, Reused, Read int }

// Metadata reuse only skips background reads; returned hits always verify content.
func refreshCodeIndex(ctx context.Context, db *sql.DB, root string) (indexRefresh, error) {
	var stats indexRefresh
	paths, truncated, err := ripgrepFiles(ctx, root, "", 20000)
	if err != nil {
		return stats, fmt.Errorf("index file discovery: %w", err)
	}
	if truncated {
		return stats, fmt.Errorf("workspace exceeds 20000-file index limit")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return stats, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TEMP TABLE seen(path TEXT PRIMARY KEY)`); err != nil {
		return stats, err
	}
	secrets := credentialValues()
	sort.Strings(secrets)
	policy := indexDigest(strings.Join(secrets, "\x00"))
	now, total := time.Now().Unix(), int64(0)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if !retrievalPathAllowed(root, path, nil) {
			continue
		}
		resolved, err := authorizeFile(ctx, path, "read")
		if err != nil {
			return stats, err
		}
		if resolved != path {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			return stats, err
		}
		if !info.Mode().IsRegular() || info.Size() > 256<<10 {
			continue
		}
		total += info.Size()
		if total > 20<<20 {
			return stats, fmt.Errorf("workspace text exceeds 20 MiB index scan limit")
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		stamp := indexFileStamp(info)
		var previous, oldStamp, oldPolicy string
		var verified int64
		err = tx.QueryRowContext(ctx, `SELECT digest,stamp,verified,policy FROM files WHERE path=?`, rel).Scan(&previous, &oldStamp, &verified, &oldPolicy)
		if err != nil && err != sql.ErrNoRows {
			return stats, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO seen(path) VALUES(?)`, rel); err != nil {
			return stats, err
		}
		if err == nil && stamp == oldStamp && policy == oldPolicy && now-verified >= 0 && now-verified < 60 {
			stats.Reused++
			continue
		}
		raw, err := readFile(ctx, path)
		if err != nil {
			return stats, err
		}
		stats.Read++
		after, err := os.Lstat(path)
		if err != nil {
			return stats, err
		}
		if indexFileStamp(after) != stamp {
			return stats, fmt.Errorf("file changed while indexing; retry search")
		}
		if len(raw) > 256<<10 || bytes.IndexByte(raw, 0) >= 0 || !utf8.Valid(raw) {
			if _, err := tx.ExecContext(ctx, `DELETE FROM seen WHERE path=?`, rel); err != nil {
				return stats, err
			}
			continue
		}
		text := redact(string(raw))
		digest := indexDigest(text)
		if previous != digest {
			if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE path=?`, rel); err != nil {
				return stats, err
			}
			for _, chunk := range codeChunks(rel, text) {
				searchTerms := strings.Join(codeTerms(rel+" "+chunk.Symbol+" "+chunk.Signature+" "+chunk.Content), " ")
				if _, err := tx.ExecContext(ctx, `INSERT INTO chunks(path,symbol,signature,content,terms,kind,language,start_line,end_line,digest) VALUES(?,?,?,?,?,?,?,?,?,?)`, rel, chunk.Symbol, chunk.Signature, chunk.Content, searchTerms, chunk.Kind, sourceLanguage(rel), chunk.Start, chunk.End, digest); err != nil {
					return stats, err
				}
			}
			stats.Updated++
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO files(path,digest,stamp,verified,policy) VALUES(?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET digest=excluded.digest,stamp=excluded.stamp,verified=excluded.verified,policy=excluded.policy`, rel, digest, stamp, now, policy); err != nil {
			return stats, err
		}
	}
	for _, query := range []string{`DELETE FROM chunks WHERE path NOT IN (SELECT path FROM seen)`, `DELETE FROM files WHERE path NOT IN (SELECT path FROM seen)`, `DROP TABLE seen`} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return stats, err
		}
	}
	return stats, tx.Commit()
}

func indexFileStamp(info os.FileInfo) string {
	stamp := fmt.Sprintf("%d:%d:%o", info.Size(), info.ModTime().UnixNano(), info.Mode())
	// Include identity/change-time fields where supplied by the OS, excluding access time.
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		for _, name := range []string{"Dev", "Ino", "Ctim", "Ctimespec", "CreationTime", "LastWriteTime"} {
			field := value.FieldByName(name)
			if field.IsValid() && field.CanInterface() {
				stamp += fmt.Sprintf(":%s=%v", name, field.Interface())
			}
		}
	}
	return stamp
}
