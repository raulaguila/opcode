package workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Contract is set by the user, never by a model tool. Targets and acceptance
// criteria describe the requested outcome; they do not confer permissions or
// prove semantic correctness. Checks require actual current-revision receipts.
type Contract struct {
	Kind       string   `json:"kind"`
	Targets    []string `json:"targets,omitempty"`
	Acceptance []string `json:"acceptance,omitempty"`
	Checks     []string `json:"checks,omitempty"`
}

func (c Contract) Validate() error {
	if c.Kind != "explain" && c.Kind != "plan" && c.Kind != "change" && c.Kind != "verify" {
		return fmt.Errorf("task kind must be explain, plan, change or verify")
	}
	if len(c.Targets) > 16 || len(c.Acceptance) > 16 || len(c.Checks) > 16 {
		return fmt.Errorf("task contract accepts at most 16 targets, criteria and checks")
	}
	for _, p := range c.Targets {
		if !filepath.IsLocal(p) || strings.ContainsAny(p, "\x00\r\n") || len(p) > 500 {
			return fmt.Errorf("targets must be relative project paths")
		}
	}
	for _, v := range append(slices.Clone(c.Acceptance), c.Checks...) {
		if strings.TrimSpace(v) == "" || len(v) > 500 || strings.ContainsAny(v, "\x00\r\n") {
			return fmt.Errorf("criteria/checks must contain 1–500 characters on one line")
		}
	}
	if c.Kind == "verify" && len(c.Checks) == 0 {
		return fmt.Errorf("verify requires at least one configured check")
	}
	return nil
}
func (s *Store) SetContract(ctx context.Context, id string, c *Contract) (Task, error) {
	if c != nil {
		if err := c.Validate(); err != nil {
			return Task{}, err
		}
	}
	return s.update(ctx, id, func(t *Task) error {
		if c == nil {
			t.Contract = nil
			return nil
		}
		next := *c
		next.Targets = slices.Clone(c.Targets)
		next.Acceptance = slices.Clone(c.Acceptance)
		next.Checks = slices.Clone(c.Checks)
		t.Contract = &next
		return nil
	})
}
func (t Task) EffectiveMode() Mode {
	if t.Contract != nil {
		if t.Contract.Kind == "explain" {
			return Ask
		}
		if t.Contract.Kind == "plan" && t.Mode != Ask {
			return Plan
		}
	}
	return t.Mode
}
