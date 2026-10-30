package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
)

// FileChange is an exact, private snapshot used by transactional review/undo.
type FileChange struct {
	Path   string  `json:"path"`
	Before *string `json:"before"`
	After  *string `json:"after"`
}
type changeObserverKey struct{}

func WithChangeObserver(ctx context.Context, observer func([]FileChange) error) context.Context {
	return context.WithValue(ctx, changeObserverKey{}, observer)
}

// ApplyFileChanges revalidates all expected contents and commits atomically. The
// caller owns approval; paths still pass through the context access policy.
func ApplyFileChanges(ctx context.Context, changes []FileChange) error {
	if len(changes) > 128 {
		return fmt.Errorf("at most 128 files can be changed in one transaction")
	}
	pending := make(map[string]fileChange, len(changes))
	for _, change := range changes {
		path := absolutePath(change.Path)
		if _, exists := pending[path]; exists {
			return fmt.Errorf("duplicate file change")
		}
		pending[path] = fileChange{old: change.Before, next: change.After}
	}
	return applyFiles(ctx, pending)
}

type fileChange struct {
	old  *string
	next *string
}
type stagedFile struct {
	root                   *os.Root
	name, temp, backup     string
	hadOriginal, installed bool
}

var fileCommitMu sync.Mutex

func randomFileName(prefix string) string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(data[:])
}
func atomicWriteFile(ctx context.Context, path, content string, old *string) error {
	return applyFiles(ctx, map[string]fileChange{path: {old: old, next: &content}})
}

// All contents are staged and revalidated before installing anything. Runtime
// failures restore backups; a machine crash is not a multi-file transaction.
func applyFiles(ctx context.Context, changes map[string]fileChange) (failure error) {
	if len(changes) == 0 {
		return nil
	}
	fileCommitMu.Lock()
	defer fileCommitMu.Unlock()
	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var stages []*stagedFile
	defer func() { failure = finishStagedFiles(stages, failure) }()
	var err error
	stages, err = prepareFileChanges(ctx, paths, changes)
	if err != nil {
		return err
	}
	if err := validateFileStages(stages, paths, changes); err != nil {
		return err
	}
	for i, path := range paths {
		if err := stages[i].install(ctx, changes[path]); err != nil {
			return err
		}
	}
	return observeFileChanges(ctx, paths, changes)
}

func validateStagedSource(root *os.Root, name string, expected *string) (os.FileMode, error) {
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		if expected != nil {
			return 0, fmt.Errorf("file changed since approval; read again")
		}
		return 0644, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("only regular files can be edited")
	}
	if expected == nil {
		return 0, fmt.Errorf("file appeared since approval; read again")
	}
	raw, err := root.ReadFile(name)
	if err != nil {
		return 0, err
	}
	if string(raw) != *expected {
		return 0, fmt.Errorf("file changed since approval; read again")
	}
	return info.Mode().Perm(), nil
}

func prepareFileChanges(ctx context.Context, paths []string, changes map[string]fileChange) ([]*stagedFile, error) {
	stages := make([]*stagedFile, 0, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return stages, err
		}
		root, name, err := fileRoot(ctx, path, "write")
		if err != nil {
			return stages, err
		}
		s := &stagedFile{root: root, name: name}
		stages = append(stages, s) // Cleanup owns the handle even if staging fails.
		change := changes[path]
		mode, err := validateStagedSource(root, name, change.old)
		if err != nil {
			return stages, err
		}
		s.hadOriginal = change.old != nil
		if change.next != nil {
			if err := s.write(*change.next, mode); err != nil {
				return stages, err
			}
		}
	}
	return stages, nil
}

func (s *stagedFile) write(content string, mode os.FileMode) error {
	if err := s.root.MkdirAll(filepath.Dir(s.name), 0755); err != nil {
		return err
	}
	s.temp = filepath.Join(filepath.Dir(s.name), randomFileName(".opcode-stage-"))
	file, err := s.root.OpenFile(s.temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(content)
	syncErr := file.Sync()
	modeErr := file.Chmod(mode)
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, modeErr, closeErr)
}

func validateFileStages(stages []*stagedFile, paths []string, changes map[string]fileChange) error {
	for i, path := range paths {
		if _, err := validateStagedSource(stages[i].root, stages[i].name, changes[path].old); err != nil {
			return err
		}
	}
	return nil
}

func (s *stagedFile) install(ctx context.Context, change fileChange) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := validateStagedSource(s.root, s.name, change.old); err != nil {
		return err
	}
	if s.hadOriginal {
		s.backup = filepath.Join(filepath.Dir(s.name), randomFileName(".opcode-backup-"))
		if err := s.root.Rename(s.name, s.backup); err != nil {
			s.backup = ""
			return err
		}
	}
	if change.next != nil {
		if err := s.root.Rename(s.temp, s.name); err != nil {
			return err
		}
		s.temp = ""
		s.installed = true
	}
	return nil
}

func finishStagedFiles(stages []*stagedFile, failure error) error {
	if failure != nil {
		failure = rollbackFileStages(stages, failure)
	}
	for _, s := range stages {
		if s.temp != "" {
			s.root.Remove(s.temp)
		}
		if s.backup != "" {
			s.root.Remove(s.backup)
		}
		s.root.Close()
	}
	return failure
}

func rollbackFileStages(stages []*stagedFile, failure error) error {
	for _, s := range slices.Backward(stages) {

		if s.installed {
			if err := s.root.Remove(s.name); err != nil && !os.IsNotExist(err) {
				failure = errors.Join(failure, fmt.Errorf("rollback remove failed: %w", err))
			}
		}
		if s.backup != "" {
			if err := s.root.Rename(s.backup, s.name); err != nil {
				failure = errors.Join(failure, fmt.Errorf("rollback restore failed; backup retained as %s: %w", s.backup, err))
				s.backup = ""
			}
		}
	}
	return failure
}

func observeFileChanges(ctx context.Context, paths []string, changes map[string]fileChange) error {
	if err := notifyMutation(ctx); err != nil {
		return err
	}
	observer, ok := ctx.Value(changeObserverKey{}).(func([]FileChange) error)
	if !ok {
		return nil
	}
	snapshot := make([]FileChange, 0, len(paths))
	for _, path := range paths {
		change := changes[path]
		snapshot = append(snapshot, FileChange{Path: path, Before: change.old, After: change.next})
	}
	return observer(snapshot)
}
