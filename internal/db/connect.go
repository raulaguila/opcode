package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/logging"

	"github.com/pressly/goose/v3"
)

func Connect() (*sql.DB, error) {
	dataDir := config.Get().Data.Directory
	if dataDir == "" {
		return nil, fmt.Errorf("data.dir is not set")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}
	dbPath, err := filepath.Abs(config.DatabasePath(dataDir))
	if err != nil {
		return nil, fmt.Errorf("invalid database path: %w", err)
	}
	// Pragmas belong in the DSN: database/sql pools connections, and a pragma
	// run with Exec only reaches the one connection that executed it.
	uriPath := filepath.ToSlash(dbPath)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath // Windows volume paths: file:///C:/...
	}
	dsn := url.URL{Scheme: "file", Path: uriPath}
	query := url.Values{}
	for _, pragma := range []string{"busy_timeout(10000)", "foreign_keys(1)", "journal_mode(wal)", "page_size(4096)", "cache_size(-8000)", "synchronous(normal)"} {
		query.Add("_pragma", pragma)
	}
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite3", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Verify connection
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	goose.SetBaseFS(FS)

	if err := goose.SetDialect("sqlite3"); err != nil {
		logging.Error("Failed to set dialect", "error", err)
		db.Close()
		return nil, fmt.Errorf("failed to set dialect: %w", err)
	}

	if err := goose.Up(db, "migrations"); err != nil {
		logging.Error("Failed to apply migrations", "error", err)
		db.Close()
		return nil, fmt.Errorf("failed to apply migrations: %w", err)
	}
	return db, nil
}
