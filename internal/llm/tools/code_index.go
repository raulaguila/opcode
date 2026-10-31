package tools

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/localstate"
)

var codeIndexLocks sync.Map

type codeIndexHit struct {
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

type codeIndexTool struct{}

func NewCodeIndexTool() BaseTool { return &codeIndexTool{} }
func (*codeIndexTool) Info() ToolInfo {
	params := scopeParameters()
	params["query"] = map[string]any{"type": "string", "maxLength": 512}
	params["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 12}
	params["intent"] = map[string]any{"type": "string", "enum": []string{"implementation", "overview", "tests", "documentation"}}
	params["strategy"] = map[string]any{"type": "string", "enum": []string{"lexical", "hybrid"}, "description": "Hybrid requires explicitly configured local embeddings; lexical fallback remains available."}
	params["kind"] = map[string]any{"type": "string", "description": "Structural declaration kind, or text."}
	return ToolInfo{Name: "search_code_index", Description: "Search a private local FTS5/BM25 index with scope filters. Returns 6 short previews by default, maximum 12; refine the query when more=true. Selected languages use local syntax parsers; others use text windows. Refreshes ignore rules each call and reuses unchanged metadata for at most 60 seconds. Verifies current file hashes before returning hits. Previews are navigation, not edit-read evidence; use view/read_symbol for source. Requires rg; maximum 20000 files, 256 KiB/file, 20 MiB text and 128 MiB scanned per refresh; binary assets do not consume the text allowance.", Parameters: params, Required: []string{"query"}}
}
func (*codeIndexTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	input, err := parseCodeIndexInput(call.Input)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
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
	return runCodeIndexSearch(ctx, root, relativeScope, input)
}

func runCodeIndexSearch(ctx context.Context, root, relativeScope string, input codeIndexInput) (ToolResponse, error) {
	unlock, err := acquireIndex(ctx, root)
	if err != nil {
		return ToolResponse{}, err
	}
	release := sync.OnceFunc(unlock)
	defer release()
	db, err := openCodeIndex(ctx, root)
	if err != nil {
		return ToolResponse{}, err
	}
	defer db.Close()
	snapshot, err := captureIndexSearch(ctx, db, root, relativeScope, input)
	if err != nil {
		return ToolResponse{}, err
	}
	release() // Network and result verification do not own the workspace lock.
	candidates, strategy := snapshot.candidates, snapshot.strategy
	if snapshot.hybrid {
		candidates, strategy = rankHybridCandidates(ctx, db, input.Query, candidates, snapshot.pool, root)
	}
	if input.Intent == "overview" {
		candidates = overviewIndexCandidates(candidates, input.Query)
	}
	items, more, err := verifiedCodeIndexHits(ctx, root, candidates, input.Limit)
	if err != nil {
		return ToolResponse{}, err
	}
	return jsonToolResult(map[string]any{"results": items, "more": more || len(candidates) == 120, "updated_files": snapshot.stats.Updated, "reused_files": snapshot.stats.Reused, "scan_reads": snapshot.stats.Read, "strategy": strategy, "index_paused": snapshot.paused, "navigation_only": true}), nil
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
	db, err := driver.Open(path, fts5.Register)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, query := range []string{`PRAGMA journal_mode=MEMORY`, `PRAGMA secure_delete=ON`, `PRAGMA max_page_count=32768`, fmt.Sprintf("PRAGMA cache_size=-%d", indexLimits().MemoryMiB*1024), `CREATE TABLE IF NOT EXISTS embeddings(key TEXT PRIMARY KEY,vector TEXT NOT NULL,at INTEGER NOT NULL)`, `CREATE INDEX IF NOT EXISTS embeddings_recent ON embeddings(at DESC,key)`, `CREATE TABLE IF NOT EXISTS files(path TEXT PRIMARY KEY, digest TEXT NOT NULL, stamp TEXT NOT NULL, verified INTEGER NOT NULL, policy TEXT NOT NULL)`, `CREATE VIRTUAL TABLE IF NOT EXISTS chunks USING fts5(path,symbol,signature,content,terms,kind UNINDEXED,language UNINDEXED,start_line UNINDEXED,end_line UNINDEXED,digest UNINDEXED)`} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func indexFileStamp(info os.FileInfo) string {
	var stamp strings.Builder
	stamp.WriteString(fmt.Sprintf("%d:%d:%o", info.Size(), info.ModTime().UnixNano(), info.Mode()))
	// Include identity/change-time fields where supplied by the OS, excluding access time.
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		for _, name := range []string{"Dev", "Ino", "Ctim", "Ctimespec", "CreationTime", "LastWriteTime"} {
			field := value.FieldByName(name)
			if field.IsValid() && field.CanInterface() {
				stamp.WriteString(fmt.Sprintf(":%s=%v", name, field.Interface()))
			}
		}
	}
	return stamp.String()
}

func verifiedCodeIndexHits(ctx context.Context, root string, candidates []codeIndexHit, limit int) ([]codeIndexHit, bool, error) {
	items := []codeIndexHit{}
	more := false
	resultBytes := 0
	verified := map[string]string{}
	redactor := newCredentialRedactor(credentialValues())
	for _, c := range candidates {
		path := filepath.Join(root, c.Path)
		if !retrievalPathAllowed(root, path, nil) {
			continue
		}
		if overlappingIndexHit(items, c) {
			continue
		}
		digest, allowed, err := verifiedIndexDigest(ctx, path, verified)
		if err != nil {
			return nil, false, err
		}
		if !allowed {
			continue
		}
		if digest != c.Hash {
			continue
		}
		if len(items) == limit {
			more = true
			break
		}
		c.Preview = compactPreview(redactor.Redact(c.Preview), 240)
		c.Signature = compactPreview(redactor.Redact(c.Signature), 160)
		encoded, _ := json.Marshal(c)
		if resultBytes+len(encoded) > 7600 {
			more = true
			break
		}
		resultBytes += len(encoded)
		items = append(items, c)
	}
	return items, more, nil
}

func createIndexScope(ctx context.Context, input codeIndexInput, relativeScope string, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TEMP TABLE scope_paths(path TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	paths, err := db.QueryContext(ctx, `SELECT path FROM files`)
	if err != nil {
		return err
	}
	var allowed []string
	for paths.Next() {
		var path string
		if err := paths.Scan(&path); err != nil {
			paths.Close()
			return err
		}
		if (relativeScope == "." || path == relativeScope || strings.HasPrefix(path, relativeScope+"/")) && input.matches(path) {
			allowed = append(allowed, path)
		}
	}
	err = paths.Err()
	paths.Close()
	if err != nil {
		return err
	}
	for _, path := range allowed {
		if _, err := db.ExecContext(ctx, `INSERT INTO scope_paths VALUES(?)`, path); err != nil {
			return err
		}
	}
	return nil
}

type codeIndexInput struct {
	Query            string
	Intent, Strategy string
	Limit            int
	Kind             string
	retrievalScope
}

func parseCodeIndexInput(raw string) (codeIndexInput, error) {
	var input codeIndexInput
	if json.Unmarshal([]byte(raw), &input) != nil || strings.TrimSpace(input.Query) == "" || len(input.Query) > 512 || input.Limit < 0 || input.Limit > 12 || len(input.Kind) > 40 {
		return input, fmt.Errorf("query must contain 1–512 bytes and limit must be 1–12")
	}
	if err := input.retrievalScope.validate(); err != nil {
		return input, err
	}
	if !validIndexIntent(input.Intent) || !validIndexStrategy(input.Strategy) {
		return input, fmt.Errorf("invalid intent/strategy")
	}
	if input.Limit == 0 {
		input.Limit = 6
	}
	return input, nil
}
func validIndexIntent(intent string) bool {
	return intent == "" || intent == "implementation" || intent == "overview" || intent == "tests" || intent == "documentation"
}
func validIndexStrategy(strategy string) bool {
	return strategy == "" || strategy == "lexical" || strategy == "hybrid"
}

func buildCodeIndexQuery(input codeIndexInput, scoped bool) (string, []any, error) {
	terms := codeQueryTerms(input.Query)
	if len(terms) == 0 || len(terms) > 60 {
		return "", nil, fmt.Errorf("use 1–60 code search terms")
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
	preference := ""
	switch input.Intent {
	case "tests":
		preference = " (path NOT LIKE '%test%' AND path NOT LIKE '%spec%'),"
	case "documentation":
		preference = " (path NOT LIKE '%.md' AND path NOT LIKE 'docs/%'),"
	case "implementation", "overview":
		preference = " (path LIKE '%.md' OR path LIKE '%test%'),"
	}
	query += ` ORDER BY (lower(symbol)=? OR lower(path)=?) DESC, ` + preference + ` (kind='text'), bm25(chunks,3,8,4,1,1,0,0,0,0,0),path,start_line LIMIT 120`
	args = append(args, strings.ToLower(strings.TrimSpace(input.Query)), strings.ToLower(strings.TrimSpace(input.Query)))
	return query, args, nil
}

func readCodeIndexCandidates(ctx context.Context, db *sql.DB, query string, args []any) ([]codeIndexHit, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var candidates []codeIndexHit
	for rows.Next() {
		var c codeIndexHit
		if err := rows.Scan(&c.Path, &c.Symbol, &c.Signature, &c.Kind, &c.Language, &c.Start, &c.End, &c.Preview, &c.Hash, &c.Rank); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return candidates, nil
}

func overlappingIndexHit(items []codeIndexHit, candidate codeIndexHit) bool {
	for _, old := range items {
		if old.Path == candidate.Path && max(old.Start, candidate.Start) <= min(old.End, candidate.End) {
			return true
		}
	}
	return false
}
func verifiedIndexDigest(ctx context.Context, path string, verified map[string]string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if digest, ok := verified[path]; ok {
		return digest, digest != "", nil
	}
	resolved, err := authorizeFile(ctx, path, "read")
	if err != nil {
		return "", false, err
	}
	if resolved != path {
		return "", false, nil
	}
	digest, allowed, err := boundedIndexDigest(ctx, resolved)
	if err == nil {
		verified[path] = digest
	}
	return digest, allowed, err
}

func boundedIndexDigest(ctx context.Context, path string) (string, bool, error) {
	file, err := openFile(ctx, path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", false, err
	}
	limit := indexLimits().MaxFileBytes
	if !info.Mode().IsRegular() || info.Size() > limit {
		return "", false, nil
	}
	return streamIndexDigest(ctx, io.LimitReader(file, limit+1), limit, min(info.Size()+1, 32<<10))
}

func streamIndexDigest(ctx context.Context, reader io.Reader, limit, bufferSize int64) (string, bool, error) {
	buffer := make([]byte, bufferSize)
	digest := sha256.New()
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		n, err := reader.Read(buffer)
		total += int64(n)
		if total > limit {
			return "", false, nil
		}
		digest.Write(buffer[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false, err
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), true, ctx.Err()
}
