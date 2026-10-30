package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Only live watchers retain freshness state; database handles remain per operation.
var indexWatchers sync.Map

const indexReconcileInterval = 60 * time.Second
const maxIndexDirtyPaths = 20000

type indexWatchState struct {
	covered, known    bool
	unavailable       bool
	epoch, sequence   uint64
	dirty             map[string]uint64
	inventory         map[string]bool
	policy            string
	reconciled        time.Time
	pending           []indexPending
	structuralPending int
}

type indexRefreshPlan struct {
	watcher                     *IndexWatcher
	epoch, sequence             uint64
	paths                       []string
	partial                     bool
	policy                      string
	queueCount, queueStructural int
}

func indexRefreshPolicy() string {
	secrets := credentialValues()
	sort.Strings(secrets)
	limits, _ := json.Marshal(indexLimits())
	return indexDigest("parser-v5\x00" + strings.Join(secrets, "\x00") + "\x00" + string(limits))
}

func planIndexRefresh(root string, incremental bool) indexRefreshPlan {
	return planIndexRefreshLimit(root, incremental, 0)
}

func planIndexRefreshLimit(root string, incremental bool, batchLimit int) indexRefreshPlan {
	plan := indexRefreshPlan{policy: indexRefreshPolicy()}
	value, ok := indexWatchers.Load(root)
	if !ok {
		return plan
	}
	w := value.(*IndexWatcher)
	w.mu.Lock()
	defer w.mu.Unlock()
	plan.watcher, plan.epoch, plan.sequence = w, w.state.epoch, w.state.sequence
	state := &w.state
	continuing := len(state.pending) > 0
	plan.partial = incremental && state.covered && state.policy == plan.policy && (state.known || continuing) && (continuing || time.Since(state.reconciled) < indexReconcileInterval)
	if plan.partial {
		plan.selectPaths(state, batchLimit)
	}
	return plan
}

func (p *indexRefreshPlan) selectPaths(state *indexWatchState, limit int) {
	for path := range state.dirty {
		p.paths = append(p.paths, path)
	}
	sort.Strings(p.paths)
	if limit > 0 && len(p.paths) > limit {
		p.paths = p.paths[:limit]
	}
	selected := make(map[string]bool, len(p.paths))
	for _, path := range p.paths {
		selected[path] = true
	}
	for _, pending := range state.pending {
		if limit > 0 && len(p.paths) >= limit {
			break
		}
		p.queueCount++
		if !pending.revalidation {
			p.queueStructural++
		}
		if selected[pending.path] {
			continue
		}
		p.paths = append(p.paths, pending.path)
		selected[pending.path] = true
	}
}

func (p indexRefreshPlan) finish(paths []string, stats *indexRefresh, err error) {
	if p.watcher == nil {
		return
	}
	w := p.watcher
	w.mu.Lock()
	defer w.mu.Unlock()
	state := &w.state
	if err != nil {
		state.known = false
		state.pending = nil
		return
	}
	if p.epoch != state.epoch {
		return
	}
	if p.partial {
		p.finishContinuation(state, stats)
	} else {
		p.finishSnapshot(state, paths, stats)
	}
	p.consumeDirty(state)
	if p.partial {
		stats.Pending = len(state.pending) + len(state.dirty)
	}
}

func (p indexRefreshPlan) finishSnapshot(state *indexWatchState, paths []string, stats *indexRefresh) {
	state.inventory = make(map[string]bool, len(paths))
	for _, path := range paths {
		state.inventory[path] = true
	}
	state.pending, state.structuralPending = stats.deferred, stats.structuralPending
	state.known, state.policy, state.reconciled = stats.structuralPending == 0, p.policy, time.Now()
}

func (p indexRefreshPlan) finishContinuation(state *indexWatchState, stats *indexRefresh) {
	state.pending = append(state.pending[p.queueCount:], stats.deferred...)
	if len(state.pending) == 0 {
		state.pending = nil
	}
	state.structuralPending += stats.structuralPending - p.queueStructural
	state.known = state.structuralPending == 0
}

func (p indexRefreshPlan) consumeDirty(state *indexWatchState) {
	if !p.partial {
		for path, sequence := range state.dirty {
			if sequence <= p.sequence {
				delete(state.dirty, path)
			}
		}
		return
	}
	for _, path := range p.paths {
		p.consumePath(state, path)
	}
}

func (p indexRefreshPlan) consumePath(state *indexWatchState, path string) {
	sequence := state.dirty[path]
	// A newer generation remains queued. Retain an already counted entry until
	// that generation is inspected; new paths still need accounting now.
	if sequence > p.sequence && state.inventory[path] {
		return
	}
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && retrievalPathAllowed(p.watcher.root, path, nil) {
		state.inventory[path] = true
	} else {
		delete(state.inventory, path)
	}
	if sequence <= p.sequence {
		delete(state.dirty, path)
	}

}

func (p indexRefreshPlan) checkFileLimit(root string) error {
	if !p.partial {
		return nil
	}
	w := p.watcher
	w.mu.Lock()
	count := len(w.state.inventory)
	for _, path := range p.paths {
		present := false
		if info, err := os.Lstat(path); err == nil {
			present = info.Mode().IsRegular() && retrievalPathAllowed(root, path, nil)
		}
		if present && !w.state.inventory[path] {
			count++
		}
		if !present && w.state.inventory[path] {
			count--
		}
	}
	w.mu.Unlock()
	if count > indexLimits().MaxFiles {
		return fmt.Errorf("workspace exceeds 20000-file index limit")
	}
	return nil
}

func (w *IndexWatcher) invalidate() {
	w.mu.Lock()
	w.state.epoch++
	w.state.known, w.state.covered = false, false
	w.state.pending = nil
	w.state.structuralPending = 0
	clear(w.state.dirty)
	w.mu.Unlock()
}

func (w *IndexWatcher) recordEvent(event fsnotify.Event) {
	if !inside(w.root, event.Name) {
		return
	}
	base := filepath.Base(event.Name)
	if base == ".gitignore" || base == ".opcodeignore" {
		w.invalidate()
		return
	}
	if event.Op&(fsnotify.Rename|fsnotify.Remove) != 0 {
		w.mu.Lock()
		knownFile := w.state.inventory[event.Name]
		w.mu.Unlock()
		// A removed directory can hide arbitrarily many descendants.
		if !knownFile {
			w.invalidate()
			return
		}
	}
	if info, err := os.Lstat(event.Name); err == nil && info.IsDir() {
		w.invalidate()
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state.sequence++
	if len(w.state.dirty) >= maxIndexDirtyPaths {
		w.state.epoch++
		w.state.known = false
		w.state.pending = nil
		w.state.structuralPending = 0
		clear(w.state.dirty)
		return
	}
	w.state.dirty[event.Name] = w.state.sequence
}

// Watch eligible directories, including empty ones. Unknown or excessive coverage
// keeps searches on full discovery rather than trusting a blind time cache.
func (w *IndexWatcher) syncDirectories(ctx context.Context, watch *fsnotify.Watcher) {
	if watch == nil {
		return
	}
	w.mu.Lock()
	epoch := w.state.epoch
	w.mu.Unlock()
	desired := map[string]bool{}
	policy := newDiscoveryPolicy()
	err := filepath.WalkDir(w.root, indexWatchDirectoryVisitor(ctx, w.root, policy, desired))
	if err == nil {
		err = policy.Validate()
	}
	if err == nil {
		err = reconcileIndexWatches(watch, desired)
	}
	w.mu.Lock()
	w.state.covered = err == nil && !w.state.unavailable && epoch == w.state.epoch
	if err != nil {
		w.state.known = false
	}
	w.mu.Unlock()
}

func reconcileIndexWatches(watch *fsnotify.Watcher, desired map[string]bool) error {
	existing := map[string]bool{}
	for _, path := range watch.WatchList() {
		existing[path] = true
		if !desired[path] {
			_ = watch.Remove(path)
		}
	}
	for path := range desired {
		if !existing[path] {
			if err := watch.Add(path); err != nil {
				return err
			}
		}
	}
	return nil
}

// Observe independently of bounded indexing, so a slow refresh cannot conceal
// queued filesystem changes from the next search's freshness decision.
func (w *IndexWatcher) observe(ctx context.Context, watch *fsnotify.Watcher, changes chan<- struct{}) {
	events, failures := watch.Events, watch.Errors
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				events = nil
			}
			if !w.receiveWatchEvent(event, ok) {
				continue
			}
		case _, ok := <-failures:
			if !ok {
				failures = nil
				w.stopCoverage()
			} else {
				w.invalidate()
			}
		}
		select {
		case changes <- struct{}{}:
		default:
		}
	}
}

func (w *IndexWatcher) stopCoverage() {
	w.invalidate()
	w.mu.Lock()
	w.state.unavailable = true
	w.mu.Unlock()
}

func indexWatchDirectoryVisitor(ctx context.Context, root string, policy *discoveryPolicy, desired map[string]bool) fs.WalkDirFunc {
	return func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root && policy.exclusionReason(root, path) != "" {
			return filepath.SkipDir
		}
		if len(desired) >= 2048 {
			return fmt.Errorf("index watch directory limit")
		}
		desired[path] = true
		return nil
	}
}

func (w *IndexWatcher) receiveWatchEvent(event fsnotify.Event, ok bool) bool {
	if !ok {
		w.stopCoverage()
		return true
	}
	if !shouldRefreshIndexEvent(w.root, event) {
		return false
	}
	w.recordEvent(event)
	return true
}

// Finish the captured bounded pass before scheduling the next TTL pass. Expiry
// is revalidation work, not a structural freshness failure or a reason to restart.
func (w *IndexWatcher) reconcileWhenIdle() {
	w.mu.Lock()
	continuing := len(w.state.pending) > 0
	w.mu.Unlock()
	if !continuing {
		w.invalidate()
	}
}
