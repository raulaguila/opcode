package memoryindex

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/raulaguila/opcode/internal/localstate"
)

type projectionIdentity struct {
	directory, file os.FileInfo
}

func (id projectionIdentity) same(other projectionIdentity) bool {
	return os.SameFile(id.directory, other.directory) && os.SameFile(id.file, other.file)
}

func projectionPath(directory, project string) (string, string) {
	hash := sha256.Sum256([]byte(filepath.Clean(project)))
	return filepath.Join(directory, "memory-index"), fmt.Sprintf("%x.sqlite", hash[:])
}

func checkedIdentity(directory, project string) (projectionIdentity, error) {
	dir, name := projectionPath(directory, project)
	root, err := localstate.OpenPrivateRoot(dir, false)
	if err != nil {
		return projectionIdentity{}, err
	}
	defer root.Close()
	return identityAt(root, name)
}

func identityAt(root *os.Root, name string) (projectionIdentity, error) {
	directory, err := root.Stat(".")
	if err != nil {
		return projectionIdentity{}, err
	}
	file, err := root.Lstat(name)
	if err != nil {
		return projectionIdentity{}, err
	}
	if !file.Mode().IsRegular() {
		return projectionIdentity{}, fmt.Errorf("memory index must be a regular file")
	}
	if err := root.Chmod(name, 0600); err != nil {
		return projectionIdentity{}, err
	}
	return projectionIdentity{directory, file}, nil
}

func open(ctx context.Context, directory, project string) (*sql.DB, error) {
	db, _, err := openChecked(ctx, directory, project)
	return db, err
}

func openChecked(ctx context.Context, directory, project string) (*sql.DB, projectionIdentity, error) {
	dir, name := projectionPath(directory, project)
	root, err := localstate.OpenPrivateRoot(dir, true)
	if err != nil {
		return nil, projectionIdentity{}, err
	}
	defer root.Close()
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		err = f.Close()
	} else if os.IsExist(err) {
		err = nil
	}
	if err != nil {
		return nil, projectionIdentity{}, err
	}
	before, err := identityAt(root, name)
	if err != nil {
		return nil, projectionIdentity{}, err
	}
	db, err := sql.Open("sqlite3", filepath.Join(dir, name))
	if err != nil {
		return nil, projectionIdentity{}, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{`PRAGMA journal_mode=MEMORY`, `PRAGMA secure_delete=ON`, `PRAGMA max_page_count=32768`, `PRAGMA busy_timeout=3000`, `CREATE TABLE IF NOT EXISTS documents(id TEXT PRIMARY KEY,kind TEXT NOT NULL,session TEXT NOT NULL,sources TEXT NOT NULL,stamp INTEGER NOT NULL)`, `CREATE INDEX IF NOT EXISTS documents_stamp ON documents(stamp DESC,id)`, `CREATE VIRTUAL TABLE IF NOT EXISTS memory USING fts5(id UNINDEXED,text)`} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			db.Close()
			return nil, projectionIdentity{}, err
		}
	}
	after, err := checkedIdentity(directory, project)
	if err == nil && !before.same(after) {
		err = fmt.Errorf("memory index changed during open")
	}
	if err != nil {
		db.Close()
		return nil, projectionIdentity{}, err
	}
	return db, after, nil
}

// Store owns one lazy projection connection. Callers close it with their service.
type Store struct {
	directory, project string
	ctx                context.Context
	cancel             context.CancelFunc
	gate               chan struct{}
	db                 *sql.DB
	identity           projectionIdentity
}

func NewStore(directory, project string) *Store {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Store{directory: directory, project: project, ctx: ctx, cancel: cancel, gate: make(chan struct{}, 1)}
	s.gate <- struct{}{}
	return s
}

func (s *Store) connection(ctx context.Context) (*sql.DB, error) {
	if s.db != nil {
		current, err := checkedIdentity(s.directory, s.project)
		if err == nil && s.identity.same(current) {
			return s.db, nil
		}
		s.db.Close()
		s.db = nil
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	db, identity, err := openChecked(ctx, s.directory, s.project)
	if err != nil {
		return nil, err
	}
	s.db, s.identity = db, identity
	return db, nil
}

func (s *Store) withDB(ctx context.Context, apply func(context.Context, *sql.DB) error) error {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.gate:
	}
	defer func() { s.gate <- struct{}{} }()
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	db, err := s.connection(ctx)
	if err != nil {
		return err
	}
	return apply(ctx, db)
}

func (s *Store) Upsert(ctx context.Context, d Document) error {
	if err := validateDocument(d); err != nil {
		return err
	}
	return s.withDB(ctx, func(ctx context.Context, db *sql.DB) error { return upsert(ctx, db, d) })
}

func (s *Store) Remove(ctx context.Context, id string) error {
	return s.withDB(ctx, func(ctx context.Context, db *sql.DB) error { return remove(ctx, db, id) })
}

// Cancel active and queued operations before waiting for exclusive ownership.
func (s *Store) Close() error {
	s.cancel()
	<-s.gate
	defer func() { s.gate <- struct{}{} }()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}
