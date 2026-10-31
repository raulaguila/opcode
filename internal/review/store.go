// Package review records exact tool commits for conflict-checked turn reversal.
package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/diff"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/localstate"
	"github.com/raulaguila/opcode/internal/permission"
)

const maxState = 16 << 20
const maxContent = 12 << 20

type Turn struct {
	ID        string             `json:"id"`
	SessionID string             `json:"session_id"`
	CreatedAt int64              `json:"created_at"`
	Undone    bool               `json:"undone"`
	Changes   []tools.FileChange `json:"changes"`
}
type document struct {
	Root  string `json:"root"`
	Turns []Turn `json:"turns"`
}
type Store struct{ root, path string }
type Preview struct {
	Turn           Turn
	Changes        []tools.FileChange
	Diff, Revision string
	Redo           bool
}

func New(root, dataDir string) *Store {
	root, _ = filepath.Abs(root)
	if dataDir == "" {
		dataDir = filepath.Join(root, ".opcode")
	}
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(root, dataDir)
	}
	return &Store{filepath.Clean(root), filepath.Join(dataDir, "turn-changes.json")}
}
func (s *Store) load() (document, error) {
	d := document{Root: s.root}
	err := localstate.Read(s.path, maxState, &d)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	if d.Root != s.root || len(d.Turns) > 64 {
		return d, fmt.Errorf("review history belongs to another workspace or exceeds retention")
	}
	for _, turn := range d.Turns {
		if turn.ID == "" || turn.SessionID == "" || len(turn.Changes) > 128 {
			return d, fmt.Errorf("invalid review history")
		}
		for _, change := range turn.Changes {
			if change.Path == "" || !filepath.IsAbs(change.Path) {
				return d, fmt.Errorf("invalid review path")
			}
		}
	}
	return d, nil
}
func cloneTurn(t Turn) Turn {
	data, _ := json.Marshal(t)
	var c Turn
	_ = json.Unmarshal(data, &c)
	return c
}
func totalBytes(turns []Turn) int {
	n := 0
	for _, t := range turns {
		for _, c := range t.Changes {
			if c.Before != nil {
				n += len(*c.Before)
			}
			if c.After != nil {
				n += len(*c.After)
			}
		}
	}
	return n
}
func same(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func (s *Store) Record(session, turnID string, changes []tools.FileChange) error {
	if session == "" || turnID == "" {
		return fmt.Errorf("review requires a session and turn")
	}
	if len(changes) == 0 {
		return nil
	}
	unlock, err := localstate.LockFile(s.path)
	if err != nil {
		return err
	}
	defer unlock()
	d, err := s.load()
	if err != nil {
		return err
	}
	idx := slices.IndexFunc(d.Turns, func(t Turn) bool { return t.ID == turnID && t.SessionID == session })
	if idx < 0 {
		// A new edit after undo starts another branch; old redo candidates expire.
		d.Turns = slices.DeleteFunc(d.Turns, func(t Turn) bool { return t.SessionID == session && t.Undone })
		d.Turns = append(d.Turns, Turn{ID: turnID, SessionID: session, CreatedAt: time.Now().Unix()})
		idx = len(d.Turns) - 1
	}
	turn := cloneTurn(d.Turns[idx])
	if err := mergeTurnChanges(&turn, s.root, changes); err != nil {
		return err
	}
	if len(turn.Changes) > 128 || totalBytes([]Turn{turn}) > maxContent {
		return fmt.Errorf("turn snapshots exceed review retention limit")
	}
	d.Turns[idx] = turn
	for len(d.Turns) > 64 || totalBytes(d.Turns) > maxContent {
		d.Turns = d.Turns[1:]
	}
	return localstate.Write(s.path, d, maxState)
}
func (s *Store) List(session string) ([]Turn, error) {
	unlock, err := localstate.LockFile(s.path)
	if err != nil {
		return nil, err
	}
	defer unlock()
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	var found []Turn
	for _, t := range d.Turns {
		if t.SessionID == session {
			found = append(found, cloneTurn(t))
		}
	}
	return found, nil
}
func fingerprint(t Turn) string {
	data, _ := json.Marshal(t)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (s *Store) Preview(session, turnID string, redo bool) (Preview, error) {
	turns, err := s.List(session)
	if err != nil {
		return Preview{}, err
	}
	var selected *Turn
	for _, t := range turns {
		if t.Undone != redo || turnID != "" && t.ID != turnID {
			continue
		}
		copy := t
		selected = &copy
		if redo || turnID != "" {
			break
		}
	}
	if selected == nil {
		return Preview{}, fmt.Errorf("nenhuma interação disponível para esta revisão")
	}
	p := Preview{Turn: *selected, Redo: redo, Revision: fingerprint(*selected)}
	var body strings.Builder
	for _, c := range selected.Changes {
		change := c
		if !redo {
			change.Before, change.After = c.After, c.Before
		}
		p.Changes = append(p.Changes, change)
		text := reviewChangeDiff(change, c.Path)
		body.WriteString(text)
		body.WriteByte('\n')
	}
	p.Diff = tools.RedactCredentials(body.String())
	return p, nil
}

// Apply asks for exact file/diff approvals and revalidates every expected
// content inside the shared transactional writer. Commands are never undone.
// Never hold the journal lock across the writer or permission wait: commit
// observers acquire that lock only after the file transaction lock.
func (s *Store) Apply(ctx context.Context, session string, p Preview, permissions permission.Service, onMutation func() error) error {
	if permissions == nil {
		return permission.ErrorPermissionDenied
	}
	current, err := s.Preview(session, p.Turn.ID, p.Redo)
	if err != nil {
		return err
	}
	if current.Revision != p.Revision {
		return fmt.Errorf("review changed; open the preview again")
	}
	// Always execute the freshly loaded candidate, never caller-supplied bytes.
	p = current
	action := "undo_turn:"
	if p.Redo {
		action = "redo_turn:"
	}
	for _, change := range p.Changes {
		before, after := "", ""
		if change.Before != nil {
			before = *change.Before
		}
		if change.After != nil {
			after = *change.After
		}
		preview, _, _ := diff.GenerateDiff(before, after, change.Path)
		if !permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: session, ToolName: tools.PatchToolName, Action: action + p.Turn.ID, Path: change.Path, Description: "Reverter interação: " + filepath.Base(change.Path), Params: tools.EditPermissionsParams{FilePath: change.Path, Diff: tools.RedactCredentials(preview)}}) {
			return permission.ErrorPermissionDenied
		}
	}
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, session)
	if onMutation != nil {
		ctx = tools.WithMutationObserver(ctx, func([]tools.FileChange) error { return onMutation() })
	}
	ctx = tools.WithChangeObserver(ctx, func([]tools.FileChange) error { return s.markApplied(session, p) })
	_, err = tools.SecureTool(&applyTool{p.Changes}, permissions).Run(ctx, tools.ToolCall{Name: tools.PatchToolName, Input: `{}`})
	return err
}

type applyTool struct{ changes []tools.FileChange }

func (*applyTool) Info() tools.ToolInfo { return tools.ToolInfo{Name: tools.PatchToolName} }
func (t *applyTool) Run(ctx context.Context, _ tools.ToolCall) (tools.ToolResponse, error) {
	return tools.NewTextResponse("review applied"), tools.ApplyFileChanges(ctx, t.changes)
}

func mergeTurnChanges(turn *Turn, root string, changes []tools.FileChange) error {
	for _, change := range changes {
		if !filepath.IsAbs(change.Path) {
			change.Path = filepath.Join(root, change.Path)
		}
		change.Path = filepath.Clean(change.Path)
		at := slices.IndexFunc(turn.Changes, func(c tools.FileChange) bool { return c.Path == change.Path })
		if at < 0 {
			turn.Changes = append(turn.Changes, change)
		} else {
			if !same(turn.Changes[at].After, change.Before) {
				return fmt.Errorf("review snapshot conflicts with an external edit; start a new turn")
			}
			turn.Changes[at].After = change.After
		}
	}
	return nil
}

func reviewChangeDiff(change tools.FileChange, path string) string {
	before, after := "", ""
	if change.Before != nil {
		before = *change.Before
	}
	if change.After != nil {
		after = *change.After
	}
	text, _, _ := diff.GenerateDiff(before, after, path)
	if text == "" {
		text = fmt.Sprintf("%s: alteração da existência do arquivo\n", path)
	}
	return text
}

func (s *Store) markApplied(session string, p Preview) error {
	unlock, err := localstate.LockFile(s.path)
	if err != nil {
		return err
	}
	defer unlock()
	d, err := s.load()
	if err != nil {
		return err
	}
	at := slices.IndexFunc(d.Turns, func(t Turn) bool { return t.SessionID == session && t.ID == p.Turn.ID })
	if at < 0 || fingerprint(d.Turns[at]) != p.Revision {
		return fmt.Errorf("review changed during approval")
	}
	d.Turns[at].Undone = !p.Redo
	return localstate.Write(s.path, d, maxState)
}
