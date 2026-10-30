package tools

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/localstate"
)

type IndexStatus struct {
	Root         string             `json:"root"`
	Paused       bool               `json:"paused"`
	Files        int                `json:"files"`
	Chunks       int                `json:"chunks"`
	UpdatedFiles int                `json:"updated_files"`
	Pending      int                `json:"pending_files"`
	ReusedFiles  int                `json:"reused_files"`
	Snapshot     string             `json:"snapshot"`
	Limits       config.IndexConfig `json:"limits"`
	Exclusion    string             `json:"exclusion,omitempty"`
}
type indexControlState struct {
	Paused bool `json:"paused"`
}

func indexStatePath(root string) string {
	return filepath.Join(config.Get().Data.Directory, "code-index", indexDigest(root)+"-state.json")
}
func indexPaused(root string) (bool, error) {
	var state indexControlState
	err := localstate.Read(indexStatePath(root), 1024, &state)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return state.Paused, err
}
func acquireIndex(ctx context.Context, root string) (func(), error) {
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	v, _ := codeIndexLocks.LoadOrStore(root, sem)
	sem = v.(chan struct{})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-sem:
		return func() { sem <- struct{}{} }, nil
	}
}

// ControlCodeIndex operates without an LLM and never alters source/config or
// conversational history. Rebuild/clear touch only derived index tables.
func ControlCodeIndex(ctx context.Context, action, path string) (IndexStatus, error) {
	root, err := canonicalPath(config.WorkingDirectory())
	if err != nil {
		return IndexStatus{}, err
	}
	limits := indexLimits()
	status := IndexStatus{Root: root, Limits: limits}
	status.Limits.Embeddings = nil
	unlock, err := acquireIndex(ctx, root)
	if err != nil {
		return status, err
	}
	defer unlock()
	status.Paused, err = indexPaused(root)
	if err != nil {
		return status, err
	}
	switch action {
	case "pause", "resume":
		status.Paused = action == "pause"
		if err := localstate.Write(indexStatePath(root), indexControlState{status.Paused}, 1024); err != nil {
			return status, err
		}
	case "status", "update", "rebuild", "clear", "explain":
	default:
		return status, fmt.Errorf("index action: status, update, rebuild, pause, resume, clear or explain")
	}
	if action == "explain" {
		status.Exclusion = indexExclusion(root, path)
		return status, nil
	}
	db, err := openCodeIndex(ctx, root)
	if err != nil {
		return status, err
	}
	defer db.Close()
	if action == "clear" {
		if err := clearCodeIndex(ctx, db); err != nil {
			return status, err
		}
	}
	if action == "update" || action == "rebuild" {
		ctx, err = updateControlledIndex(ctx, action, root, db, &status)
		if err != nil {
			return status, err
		}
	}
	err = populateCodeIndexStatus(ctx, db, &status)
	return status, err
}

// IndexWatcher owns its goroutine and OS handles; Close cancels in-progress
// bounded updates and joins. Failed watches fall back to periodic reconciliation.
type IndexWatcher struct {
	cancel    context.CancelFunc
	done      chan struct{}
	mu        sync.Mutex
	lastError string
}

func StartIndexWatcher(parent context.Context) *IndexWatcher {
	ctx, cancel := context.WithCancel(parent)
	w := &IndexWatcher{cancel: cancel, done: make(chan struct{})}
	go func() { defer close(w.done); w.watch(ctx) }()
	return w
}
func (w *IndexWatcher) Close() {
	if w != nil {
		w.cancel()
		<-w.done
	}
}
func (w *IndexWatcher) Error() string { w.mu.Lock(); defer w.mu.Unlock(); return w.lastError }
func (w *IndexWatcher) watch(ctx context.Context) {
	root := config.WorkingDirectory()
	watch, err := fsnotify.NewWatcher()
	if err == nil {
		defer watch.Close()
		_ = watch.Add(root)
	}
	var events <-chan fsnotify.Event
	var failures <-chan error
	if watch != nil {
		events = watch.Events
		failures = watch.Errors
	}
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	timer := time.NewTimer(150 * time.Millisecond)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			w.refresh(ctx, root, watch, timer)
		case <-ticker.C:
			w.refresh(ctx, root, watch, timer)
		case _, ok := <-failures:
			if !ok {
				failures = nil
			}
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if shouldRefreshIndexEvent(root, event) {
				resetIndexTimer(timer, 150*time.Millisecond)
			}
		}
	}
}

func IndexStatusJSON(ctx context.Context, action, path string) (string, error) {
	status, err := ControlCodeIndex(ctx, action, path)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(status, "", "  ")
	return RedactCredentials(string(raw)), err
}

type indexRebuildKey struct{}

type indexBatchLimitKey struct{}

func prioritizeIndexPaths(paths []string) {
	changed := map[string]int64{}
	for _, path := range paths {
		if info, err := os.Lstat(path); err == nil {
			changed[path] = info.ModTime().UnixNano()
		}
	}
	sort.SliceStable(paths, func(i, j int) bool {
		a, b := getLastReadTime(paths[i]), getLastReadTime(paths[j])
		if !a.Equal(b) {
			return a.After(b)
		}
		if changed[paths[i]] != changed[paths[j]] {
			return changed[paths[i]] > changed[paths[j]]
		}
		return paths[i] < paths[j]
	})
}

func (w *IndexWatcher) refresh(ctx context.Context, root string, watch *fsnotify.Watcher, timer *time.Timer) {
	limits := indexLimits()
	if limits.Background != nil && !*limits.Background {
		return
	}
	updateCtx, cancel := context.WithTimeout(ctx, time.Duration(limits.UpdateSeconds)*time.Second)
	defer cancel()
	status, err := ControlCodeIndex(context.WithValue(updateCtx, indexBatchLimitKey{}, 8), "update", "")
	if err == nil && status.Pending > 0 {
		timer.Reset(time.Second)
	}
	w.mu.Lock()
	if err != nil {
		w.lastError = "Background index unavailable; run /index or opcode index update for details"
	} else {
		w.lastError = ""
	}
	w.mu.Unlock()
	if watch != nil {
		syncIndexWatchDirectories(updateCtx, root, watch)
	}
}

func shouldRefreshIndexEvent(root string, event fsnotify.Event) bool {
	base := filepath.Base(event.Name)
	return base == ".gitignore" || base == ".opcodeignore" || ExclusionReason(root, event.Name) == ""
}
func resetIndexTimer(timer *time.Timer, delay time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}
func syncIndexWatchDirectories(ctx context.Context, root string, watch *fsnotify.Watcher) {
	files, _, err := scopedFilePaths(ctx, root, retrievalScope{}, nil)
	if err != nil {
		return
	}
	existing := map[string]bool{}
	for _, dir := range watch.WatchList() {
		existing[dir] = true
	}
	for _, path := range files {
		dir := filepath.Dir(filepath.Join(root, path))
		if !existing[dir] && len(existing) < 2048 {
			if watch.Add(dir) == nil {
				existing[dir] = true
			}
		}
	}
}

func clearCodeIndex(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, q := range []string{"DELETE FROM chunks", "DELETE FROM files", "DELETE FROM embeddings"} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func populateCodeIndexStatus(ctx context.Context, db *sql.DB, status *IndexStatus) error {
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM files").Scan(&status.Files); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM chunks").Scan(&status.Chunks); err != nil {
		return err
	}
	digest := sha256.New()
	rows, err := db.QueryContext(ctx, "SELECT path,digest FROM files ORDER BY path")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p, h string
		if err := rows.Scan(&p, &h); err != nil {
			return err
		}
		fmt.Fprintf(digest, "%s\x00%s\n", p, h)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	status.Snapshot = fmt.Sprintf("%x", digest.Sum(nil))
	return nil
}

func updateControlledIndex(ctx context.Context, action, root string, db *sql.DB, status *IndexStatus) (context.Context, error) {
	if status.Paused {
		return ctx, fmt.Errorf("index paused; resume before updating")
	}
	if action == "rebuild" {
		ctx = context.WithValue(ctx, indexRebuildKey{}, true)
	}
	stats, err := refreshCodeIndex(ctx, db, root)
	if err != nil {
		return ctx, err
	}
	status.UpdatedFiles = stats.Updated
	status.ReusedFiles = stats.Reused
	status.Pending = stats.Pending
	return ctx, nil
}

func indexExclusion(root, path string) string {
	reason := ExclusionReason(root, absolutePath(path))
	if reason == "" {
		return "eligible; binary/size/scan limits are checked during update"
	}
	return reason
}
