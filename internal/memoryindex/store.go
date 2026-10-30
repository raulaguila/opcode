// Package memoryindex owns a disposable, project-scoped search projection.
// Conversation history and findings remain authoritative in their existing stores.
package memoryindex

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/raulaguila/opcode/internal/localstate"
)

type Document struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Session string          `json:"session_id"`
	Text    string          `json:"text"`
	Sources json.RawMessage `json:"sources,omitempty"`
	Stamp   int64           `json:"stamp"`
}

func open(ctx context.Context, directory, project string) (*sql.DB, error) {
	hash := sha256.Sum256([]byte(filepath.Clean(project)))
	dir := filepath.Join(directory, "memory-index")
	root, err := localstate.OpenPrivateRoot(dir, true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := fmt.Sprintf("%x.sqlite", hash[:])
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		err = f.Close()
	} else if os.IsExist(err) {
		info, e := root.Lstat(name)
		if e != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("memory index must be a regular file")
		}
		err = root.Chmod(name, 0600)
	}
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{`PRAGMA journal_mode=MEMORY`, `PRAGMA secure_delete=ON`, `PRAGMA max_page_count=32768`, `PRAGMA busy_timeout=3000`, `CREATE TABLE IF NOT EXISTS documents(id TEXT PRIMARY KEY,kind TEXT NOT NULL,session TEXT NOT NULL,sources TEXT NOT NULL,stamp INTEGER NOT NULL)`, `CREATE INDEX IF NOT EXISTS documents_stamp ON documents(stamp DESC,id)`, `CREATE VIRTUAL TABLE IF NOT EXISTS memory USING fts5(id UNINDEXED,text)`} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}
func Upsert(ctx context.Context, directory, project string, d Document) error {
	if len(d.ID) > 160 || len(d.Session) > 128 || len(d.Text) > 8192 || len(d.Sources) > 8192 || d.Kind != "conversation" && d.Kind != "finding" {
		return fmt.Errorf("invalid bounded memory projection")
	}
	db, err := open(ctx, directory, project)
	if err != nil {
		return err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory WHERE id=?`, d.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO documents(id,kind,session,sources,stamp) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET kind=excluded.kind,session=excluded.session,sources=excluded.sources,stamp=excluded.stamp`, d.ID, d.Kind, d.Session, string(d.Sources), d.Stamp); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memory(id,text) VALUES(?,?)`, d.ID, d.Text); err != nil {
		return err
	}
	if err := prune(ctx, conn, tx); err != nil {
		return err
	}
	return tx.Commit()
}

const maxDocuments = 100000

// Check only for the presence of a row: database/sql's column metadata readers
// initialize additional WASM functions on every freshly opened connection. Raw
// holds the same connection's lock; the probe and deletes share the transaction.
func retentionExceeded(ctx context.Context, conn *sql.Conn) (bool, error) {
	var exceeded bool
	err := conn.Raw(func(raw any) error {
		c := raw.(driver.Conn).Raw()
		old := c.SetInterrupt(ctx)
		defer c.SetInterrupt(old)
		statement, _, err := c.Prepare(`SELECT 1 WHERE (SELECT count(*) FROM documents)>?1`)
		if err != nil {
			return err
		}
		defer statement.Close()
		if err := statement.BindInt(1, maxDocuments); err != nil {
			return err
		}
		exceeded = statement.Step()
		return statement.Err()
	})
	return exceeded, err
}

func prune(ctx context.Context, conn *sql.Conn, tx *sql.Tx) error {
	exceeded, err := retentionExceeded(ctx, conn)
	if err != nil || !exceeded {
		return err
	}
	// Keep count(*) separate from arithmetic so SQLite uses its fast count.
	oldest := `SELECT id FROM documents ORDER BY stamp ASC,id DESC
LIMIT max(0,(SELECT count(*) FROM documents)-?1)`
	for _, table := range []string{"memory", "documents"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE id IN (`+oldest+`)`, maxDocuments); err != nil {
			return err
		}
	}
	return nil
}
func Remove(ctx context.Context, directory, project, id string) error {
	db, err := open(ctx, directory, project)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM memory WHERE id=?`, `DELETE FROM documents WHERE id=?`} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func Search(ctx context.Context, directory, project, kind, session, query string, offset, limit int) ([]Document, bool, error) {
	if len(query) < 2 || len(query) > 200 || offset < 0 || offset > 10000 || limit < 1 || limit > 20 {
		return nil, false, fmt.Errorf("invalid memory search bounds")
	}
	terms, err := memorySearchTerms(query)
	if err != nil {
		return nil, false, err
	}
	db, err := open(ctx, directory, project)
	if err != nil {
		return nil, false, err
	}
	defer db.Close()
	q := `SELECT d.id,d.kind,d.session,snippet(memory,1,'','', ' … ',40),d.sources,d.stamp FROM memory JOIN documents d ON d.id=memory.id WHERE memory MATCH ? AND d.kind=?`
	args := []any{strings.Join(terms, " OR "), kind}
	if session != "" {
		q += ` AND d.session=?`
		args = append(args, session)
	}
	q += ` ORDER BY bm25(memory),d.stamp DESC,d.id LIMIT ? OFFSET ?`
	args = append(args, limit+1, offset)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := []Document{}
	for rows.Next() {
		var d Document
		var sources string
		if err := rows.Scan(&d.ID, &d.Kind, &d.Session, &d.Text, &sources, &d.Stamp); err != nil {
			return nil, false, err
		}
		if sources != "" {
			d.Sources = json.RawMessage(sources)
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	return items, more, nil
}

func memorySearchTerms(query string) ([]string, error) {
	fields := strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(fields) > 12 {
		fields = fields[:12]
	}
	terms := []string{}
	for _, s := range fields {
		terms = append(terms, `"`+s+`"`)
	}
	if len(terms) == 0 {
		return nil, fmt.Errorf("query requires words")
	}
	return terms, nil
}
