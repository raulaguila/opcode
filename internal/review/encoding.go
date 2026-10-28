package review

import (
	"encoding/json"
	"github.com/raulaguila/opcode/internal/llm/tools"
)

// JSON base64 byte fields preserve exact file contents, including invalid UTF-8.
// These private snapshots are never injected into model context or exports.
type storedChange struct {
	Path          string `json:"path"`
	Before, After *[]byte
}
type storedTurn struct {
	ID, SessionID string
	CreatedAt     int64
	Undone        bool
	Changes       []storedChange
}

func (t Turn) MarshalJSON() ([]byte, error) {
	stored := storedTurn{ID: t.ID, SessionID: t.SessionID, CreatedAt: t.CreatedAt, Undone: t.Undone}
	for _, c := range t.Changes {
		v := storedChange{Path: c.Path}
		if c.Before != nil {
			b := []byte(*c.Before)
			v.Before = &b
		}
		if c.After != nil {
			b := []byte(*c.After)
			v.After = &b
		}
		stored.Changes = append(stored.Changes, v)
	}
	return json.Marshal(stored)
}
func (t *Turn) UnmarshalJSON(data []byte) error {
	var stored storedTurn
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}
	*t = Turn{ID: stored.ID, SessionID: stored.SessionID, CreatedAt: stored.CreatedAt, Undone: stored.Undone}
	for _, c := range stored.Changes {
		v := tools.FileChange{Path: c.Path}
		if c.Before != nil {
			b := string(*c.Before)
			v.Before = &b
		}
		if c.After != nil {
			b := string(*c.After)
			v.After = &b
		}
		t.Changes = append(t.Changes, v)
	}
	return nil
}
