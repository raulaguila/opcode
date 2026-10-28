package workflow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Mode string

const (
	Ask     Mode = "ask"
	Plan    Mode = "plan"
	Execute Mode = "execute"
)

func (m Mode) Valid() bool { return m == Ask || m == Plan || m == Execute }

type Step struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}
type Receipt struct {
	Name     string `json:"name"`
	Command  string `json:"command_hash"`
	Revision int64  `json:"revision"`
	Digest   string `json:"digest"`
	Passed   bool   `json:"passed"`
	At       int64  `json:"at"`
}
type Task struct {
	SessionID  string    `json:"session_id"`
	Mode       Mode      `json:"mode"`
	Objective  string    `json:"objective,omitempty"`
	Steps      []Step    `json:"steps,omitempty"`
	Criteria   []string  `json:"criteria,omitempty"`
	Checks     []string  `json:"checks,omitempty"`
	Status     string    `json:"status"`
	Revision   int64     `json:"revision"`
	Digest     string    `json:"digest,omitempty"`
	Changed    bool      `json:"changed"`
	Validation []Receipt `json:"validation,omitempty"`
	Summary    string    `json:"summary,omitempty"`
}

// Store owns task state only. It never stores or restores permission grants.
type Store struct {
	mu      sync.Mutex
	db      *sql.DB
	root    string
	initial Mode
	memory  map[string]Task
}

func New(db *sql.DB, root string, initial Mode) *Store {
	if !initial.Valid() {
		initial = Execute
	}
	root, _ = filepath.Abs(root)
	return &Store{db: db, root: filepath.Clean(root), initial: initial, memory: map[string]Task{}}
}
func clone(t Task) Task {
	t.Steps = slices.Clone(t.Steps)
	t.Criteria = slices.Clone(t.Criteria)
	t.Checks = slices.Clone(t.Checks)
	t.Validation = slices.Clone(t.Validation)
	return t
}
func (s *Store) get(ctx context.Context, id string) (Task, error) {
	if id == "" {
		return Task{}, fmt.Errorf("session is required")
	}
	if s.db == nil {
		if t, ok := s.memory[id]; ok {
			return clone(t), nil
		}
	} else {
		var root, raw string
		err := s.db.QueryRowContext(ctx, "SELECT root, state FROM agent_workflows WHERE session_id = ?", id).Scan(&root, &raw)
		if err == nil {
			if root != s.root {
				return Task{}, fmt.Errorf("task belongs to another project")
			}
			var t Task
			if len(raw) > 262144 || json.Unmarshal([]byte(raw), &t) != nil || t.SessionID != id || !t.Mode.Valid() {
				return Task{}, fmt.Errorf("invalid saved task")
			}
			return t, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Task{}, err
		}
	}
	return Task{SessionID: id, Mode: s.initial, Status: "idle"}, nil
}
func (s *Store) Get(ctx context.Context, id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(ctx, id)
}
func (s *Store) update(ctx context.Context, id string, change func(*Task) error) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.get(ctx, id)
	if err != nil {
		return Task{}, err
	}
	if err = change(&t); err != nil {
		return Task{}, err
	}
	raw, err := json.Marshal(t)
	if err != nil {
		return Task{}, err
	}
	if len(raw) > 262144 {
		return Task{}, fmt.Errorf("task exceeds storage limit")
	}
	if s.db != nil {
		_, err = s.db.ExecContext(ctx, "INSERT INTO agent_workflows(session_id, root, state) VALUES(?, ?, ?) ON CONFLICT(session_id) DO UPDATE SET state=excluded.state WHERE agent_workflows.root=excluded.root", id, s.root, string(raw))
		if err != nil {
			return Task{}, err
		}
	} else {
		s.memory[id] = clone(t)
	}
	return clone(t), nil
}
func (s *Store) SetMode(ctx context.Context, id string, mode Mode) (Task, error) {
	if !mode.Valid() {
		return Task{}, fmt.Errorf("mode must be ask, plan or execute")
	}
	return s.update(ctx, id, func(t *Task) error { t.Mode = mode; return nil })
}
func (s *Store) Resume(ctx context.Context, id string) (Task, error) {
	return s.update(ctx, id, func(t *Task) error {
		if t.Mode == Execute && t.Objective != "" && t.Status != "completed" {
			t.Status = "in_progress"
		}
		return nil
	})
}
func (s *Store) UpdatePlan(ctx context.Context, id, objective string, steps []Step, criteria, checks []string) (Task, error) {
	if strings.TrimSpace(objective) == "" || len(objective) > 2000 || len(steps) < 1 || len(steps) > 24 || len(criteria) < 1 || len(criteria) > 16 || len(checks) > 16 {
		return Task{}, fmt.Errorf("plan requires objective, 1–24 steps, 1–16 criteria and at most 16 checks")
	}
	doing := 0
	for _, step := range steps {
		if strings.TrimSpace(step.Title) == "" || len(step.Title) > 500 || (step.Status != "pending" && step.Status != "in_progress" && step.Status != "completed") {
			return Task{}, fmt.Errorf("invalid plan step")
		}
		if step.Status == "in_progress" {
			doing++
		}
	}
	if doing > 1 {
		return Task{}, fmt.Errorf("only one step may be in progress")
	}
	for _, text := range append(slices.Clone(criteria), checks...) {
		if strings.TrimSpace(text) == "" || len(text) > 500 {
			return Task{}, fmt.Errorf("invalid criterion or check")
		}
	}
	return s.update(ctx, id, func(t *Task) error {
		if t.Mode == Ask {
			return fmt.Errorf("planning is unavailable in ask mode")
		}
		t.Objective = objective
		t.Steps = slices.Clone(steps)
		t.Criteria = slices.Clone(criteria)
		t.Checks = slices.Clone(checks)
		t.Status = "in_progress"
		t.Summary = ""
		return nil
	})
}
func (s *Store) Observe(ctx context.Context, id, digest string) (Task, error) {
	return s.update(ctx, id, func(t *Task) error {
		if digest == "" {
			t.Validation = nil
			t.Digest = ""
			return nil
		}
		if t.Digest != "" && digest != t.Digest {
			t.Revision++
			t.Changed = true
			if t.Status == "completed" {
				t.Status = "blocked"
				t.Summary = "Files changed after validation"
			}
		}
		t.Digest = digest
		return nil
	})
}
func (s *Store) InvalidateChecks(ctx context.Context, id string, names []string) error {
	_, err := s.update(ctx, id, func(t *Task) error {
		t.Validation = slices.DeleteFunc(t.Validation, func(r Receipt) bool { return slices.Contains(names, r.Name) })
		return nil
	})
	return err
}

func (s *Store) Mutated(ctx context.Context, id string) error {
	_, err := s.update(ctx, id, func(t *Task) error {
		t.Changed = true
		t.Revision++
		t.Digest = ""
		t.Validation = nil
		if t.Status == "completed" {
			t.Status = "in_progress"
		}
		return nil
	})
	return err
}
func (s *Store) RecordCheck(ctx context.Context, id string, r Receipt) error {
	_, err := s.update(ctx, id, func(t *Task) error {
		r.Command = fmt.Sprintf("%x", sha256.Sum256([]byte(r.Command)))
		r.Revision = t.Revision
		r.At = time.Now().Unix()
		if r.Digest == "" || r.Digest != t.Digest {
			r.Passed = false
		}
		t.Validation = slices.DeleteFunc(t.Validation, func(old Receipt) bool { return old.Name == r.Name })
		t.Validation = append(t.Validation, r)
		if len(t.Validation) > 32 {
			t.Validation = t.Validation[len(t.Validation)-32:]
		}
		return nil
	})
	return err
}
func (t Task) ValidationProblem(commands map[string]string) string {
	required := slices.Clone(t.Checks)
	if len(required) == 0 && t.Changed {
		for _, r := range t.Validation {
			if !slices.Contains(required, r.Name) {
				required = append(required, r.Name)
			}
		}
		if len(required) == 0 {
			return "Changes require checks for the current revision; configure checks and run run_checks"
		}
	}
	for _, name := range required {
		valid := false
		for _, r := range t.Validation {
			if r.Name == name && r.Passed && r.Digest != "" && r.Digest == t.Digest && r.Revision == t.Revision && commands[name] != "" && fmt.Sprintf("%x", sha256.Sum256([]byte(commands[name]))) == r.Command {
				valid = true
			}
		}
		if !valid {
			return "Missing or outdated validation: " + name
		}
	}
	return ""
}
func (s *Store) Finish(ctx context.Context, id, status, summary string, commands map[string]string) (Task, error) {
	if len(summary) > 2000 || (status != "planned" && status != "completed" && status != "blocked") {
		return Task{}, fmt.Errorf("invalid task status or summary")
	}
	return s.update(ctx, id, func(t *Task) error {
		if status == "completed" {
			if t.Mode != Execute {
				return fmt.Errorf("completion requires execute mode")
			}
			for _, step := range t.Steps {
				if step.Status != "completed" {
					return fmt.Errorf("plan has incomplete steps")
				}
			}
			if problem := t.ValidationProblem(commands); problem != "" {
				return errors.New(problem)
			}
		}
		if status == "planned" && len(t.Steps) == 0 {
			return fmt.Errorf("save the plan before marking it planned")
		}
		t.Status = status
		t.Summary = summary
		return nil
	})
}
func (s *Store) Reset(ctx context.Context, id string) (Task, error) {
	return s.update(ctx, id, func(t *Task) error { *t = Task{SessionID: id, Mode: t.Mode, Status: "idle"}; return nil })
}
