package tools

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type indexRefresh struct{ Updated, Reused, Read, Pending int }

// Binary assets do not consume the text allowance. Bound actual scan I/O
// separately, so a workspace of non-text files cannot cause unbounded reads.
const maxIndexScanBytes = 128 << 20

// Metadata reuse only skips background reads; returned hits always verify content.
func refreshCodeIndex(ctx context.Context, db *sql.DB, root string) (indexRefresh, error) {
	var stats indexRefresh
	paths, truncated, err := ripgrepFiles(ctx, root, "", indexLimits().MaxFiles)
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
	prioritizeIndexPaths(paths)
	batchLimit, _ := ctx.Value(indexBatchLimitKey{}).(int)
	secrets := credentialValues()
	sort.Strings(secrets)
	statements, err := prepareIndexStatements(ctx, tx)
	if err != nil {
		return stats, err
	}
	defer statements.Close()
	refresh := indexRefresher{statements: statements, redactor: newCredentialRedactor(secrets), policySnapshot: newDiscoveryPolicy(), ctx: ctx, tx: tx, root: root, batchLimit: batchLimit, now: time.Now().Unix(), policy: indexDigest("parser-v5\x00" + strings.Join(secrets, "\x00"))}
	for _, path := range paths {
		if err := refresh.process(path); err != nil {
			return refresh.stats, err
		}
	}
	if err := refresh.removeUnseen(); err != nil {
		return refresh.stats, err
	}
	if err := refresh.policySnapshot.Validate(); err != nil {
		return refresh.stats, err
	}
	return refresh.stats, tx.Commit()
}

type indexRefresher struct {
	ctx                   context.Context
	tx                    *sql.Tx
	statements            *indexStatements
	redactor              *credentialRedactor
	policySnapshot        *discoveryPolicy
	root, policy          string
	batchLimit, processed int
	now, total, scanned   int64
	stats                 indexRefresh
}
type indexedSource struct {
	path, relative, stamp, previous, oldStamp, oldPolicy string
	verified                                             int64
	bytes                                                int64
	known                                                bool
}

func (r *indexRefresher) inspect(path string) (*indexedSource, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if !retrievalPathAllowedPolicy(r.root, path, nil, r.policySnapshot) {
		return nil, nil
	}
	resolved, err := authorizeFile(r.ctx, path, "read")
	if err != nil {
		return nil, err
	}
	if resolved != path {
		return nil, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > indexLimits().MaxFileBytes {
		return nil, nil
	}
	rel, _ := filepath.Rel(r.root, path)
	return &indexedSource{path: path, relative: filepath.ToSlash(rel), stamp: indexFileStamp(info), bytes: info.Size()}, nil
}

func (r *indexRefresher) process(path string) error {
	source, err := r.inspect(path)
	if err != nil || source == nil {
		return err
	}
	if err := r.loadPrevious(source); err != nil {
		return err
	}
	if _, err := r.statements.seen.ExecContext(r.ctx, source.relative); err != nil {
		return err
	}
	force, _ := r.ctx.Value(indexRebuildKey{}).(bool)
	if force {
		source.oldPolicy = ""
	}
	if source.reusable(r.policy, r.now) {
		r.stats.Reused++
		return r.accountText(source.bytes)
	}
	if r.batchLimit > 0 && r.processed >= r.batchLimit {
		r.stats.Pending++
		return nil
	}
	return r.refreshSource(source)
}

func (r *indexRefresher) loadPrevious(source *indexedSource) error {
	err := r.statements.previous.QueryRowContext(r.ctx, source.relative).Scan(&source.previous, &source.oldStamp, &source.verified, &source.oldPolicy)
	source.known = err == nil
	if err == sql.ErrNoRows {
		return nil
	}
	return err
}
func (s *indexedSource) reusable(policy string, now int64) bool {
	age := now - s.verified
	return s.known && s.stamp == s.oldStamp && policy == s.oldPolicy && age >= 0 && age < 60
}

func (r *indexRefresher) refreshSource(source *indexedSource) error {
	if r.scanned+source.bytes > maxIndexScanBytes {
		return fmt.Errorf("workspace exceeds 128 MiB index scan limit; narrow the workspace or exclude assets")
	}
	raw, err := readIndexSource(r.ctx, source.path)
	if err != nil {
		return err
	}
	r.scanned += int64(len(raw))
	r.stats.Read++
	after, err := os.Lstat(source.path)
	if err != nil {
		return err
	}
	if indexFileStamp(after) != source.stamp {
		return fmt.Errorf("file changed while indexing; retry search")
	}
	if int64(len(raw)) > indexLimits().MaxFileBytes || bytes.IndexByte(raw, 0) >= 0 || !utf8.Valid(raw) {
		_, err := r.statements.unsee.ExecContext(r.ctx, source.relative)
		return err
	}
	if err := r.accountText(int64(len(raw))); err != nil {
		return err
	}
	r.processed++ // Binary assets must not starve a bounded background text batch.
	text := r.redactor.Redact(string(raw))
	digest := indexDigest(string(raw))
	if source.previous != digest || source.oldPolicy != r.policy {
		if err := r.replaceChunks(source.relative, text, digest); err != nil {
			return err
		}
		r.stats.Updated++
	}
	_, err = r.statements.upsert.ExecContext(r.ctx, source.relative, digest, source.stamp, r.now, r.policy)
	return err
}

func (r *indexRefresher) accountText(size int64) error {
	r.total += size
	if r.total > indexLimits().MaxTextBytes {
		return fmt.Errorf("workspace text exceeds index limit (%d bytes)", indexLimits().MaxTextBytes)
	}
	return nil
}

func readIndexSource(ctx context.Context, path string) ([]byte, error) {
	file, err := openFile(ctx, path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, indexLimits().MaxFileBytes+1))
}

func (r *indexRefresher) replaceChunks(relative, text, digest string) error {
	if _, err := r.statements.deleteChunks.ExecContext(r.ctx, relative); err != nil {
		return err
	}
	for _, chunk := range codeChunksContext(r.ctx, relative, text) {
		searchTerms := strings.Join(codeTerms(relative+" "+chunk.Symbol+" "+chunk.Signature+" "+chunk.Content), " ")
		if _, err := r.statements.insertChunk.ExecContext(r.ctx, relative, chunk.Symbol, chunk.Signature, chunk.Content, searchTerms, chunk.Kind, sourceLanguage(relative), chunk.Start, chunk.End, digest); err != nil {
			return err
		}
	}
	return nil
}
func (r *indexRefresher) removeUnseen() error {
	for _, query := range []string{`DELETE FROM chunks WHERE path NOT IN (SELECT path FROM seen)`, `DELETE FROM files WHERE path NOT IN (SELECT path FROM seen)`, `DROP TABLE seen`} {
		if _, err := r.tx.ExecContext(r.ctx, query); err != nil {
			return err
		}
	}
	return nil
}
