package workflow

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// FileReceipt records verified tool effects, separately from executed checks.
// An empty hash means the path was absent; contents and permissions are not stored.
type FileReceipt struct {
	Path   string `json:"path"`
	Before string `json:"before_hash,omitempty"`
	After  string `json:"after_hash,omitempty"`
}

func (s *Store) RecordFileChanges(ctx context.Context, id string, files []FileReceipt) error {
	if len(files) == 0 || len(files) > 128 {
		return fmt.Errorf("file verification requires 1–128 affected files")
	}
	for _, file := range files {
		if !filepath.IsAbs(file.Path) || filepath.Clean(file.Path) != file.Path || strings.ContainsAny(file.Path, "\x00\r\n") || !validFileHash(file.Before) || !validFileHash(file.After) || file.Before == "" && file.After == "" {
			return fmt.Errorf("invalid affected-file receipt")
		}
	}
	_, err := s.update(ctx, id, func(t *Task) error {
		for _, file := range files {
			i := slices.IndexFunc(t.FileChanges, func(old FileReceipt) bool { return old.Path == file.Path })
			if i >= 0 {
				t.FileChanges[i].After = file.After
			} else {
				t.FileChanges = append(t.FileChanges, file)
			}
		}
		if len(t.FileChanges) > 128 {
			return fmt.Errorf("file verification exceeds 128 affected files")
		}
		invalidateTaskChecks(t)
		return nil
	})
	return err
}

func validFileHash(value string) bool {
	if value == "" {
		return true
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func invalidateTaskChecks(t *Task) {
	t.Changed = true
	t.Revision++
	t.Digest = ""
	t.Validation = nil
	if t.Status == "completed" || t.Status == "idle" {
		t.Status = "in_progress"
	}
}

func (t Task) HasFileChanges() bool {
	return slices.ContainsFunc(t.FileChanges, func(file FileReceipt) bool { return file.Before != file.After })
}

// Commands and check receipts still need the complete workspace observation.
func (t Task) NeedsWorkspaceVerification(commands map[string]string) bool {
	return len(t.FileChanges) == 0 || t.WorkspaceRequired || len(t.Checks) > 0 || len(t.Validation) > 0 || t.Contract != nil && len(t.Contract.Checks) > 0 || t.Changed && len(commands) > 0
}

func (s *Store) RequireWorkspace(ctx context.Context, id string) error {
	_, err := s.update(ctx, id, func(t *Task) error { t.WorkspaceRequired = true; return nil })
	return err
}
