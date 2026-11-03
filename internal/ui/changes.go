package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/review"
)

// reviewedMsg brings the outcome of an undo or redo.
type reviewedMsg struct {
	redo bool
	err  error
}

func reviewStore() *review.Store {
	cfg := config.Get()
	return review.New(cfg.WorkingDir, cfg.Data.Directory)
}

// openChanges lists the turns of this conversation that edited files, newest
// first, beside what Enter would do: undo an applied turn, redo an undone one.
func (m *Model) openChanges() tea.Cmd {
	if m.session.ID == "" {
		m.info = textFor("Nenhuma alteração nesta conversa", "No changes in this conversation")
		return nil
	}
	store := reviewStore()
	turns, err := store.List(m.session.ID)
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	var items []Item
	for _, turn := range slices.Backward(turns) {
		preview, err := store.Preview(m.session.ID, turn.ID, turn.Undone)
		if err != nil {
			continue
		}
		var names []string
		for _, change := range turn.Changes {
			names = append(names, filepath.Base(change.Path))
		}
		state := textFor("aplicada", "applied")
		if turn.Undone {
			state = textFor("desfeita", "undone")
		}
		items = append(items, Item{ID: turn.ID, Title: time.Unix(turn.CreatedAt, 0).Format("02/01 15:04"), Desc: strings.Join(names, ", "), Right: state, Faint: turn.Undone, Value: preview})
	}
	if len(items) == 0 {
		m.info = textFor("Nenhuma alteração de arquivo nesta conversa", "No file changes in this conversation")
		return nil
	}
	m.dialog = &listDialog{
		title:    textFor("Alterações", "Changes"),
		legend:   textFor("↑↓ escolher · Enter desfazer ou refazer · Esc fechar", "↑↓ choose · Enter undo or redo · Esc close"),
		noSearch: true,
		width:    110,
		split:    40,
		list:     List{Items: items, Cursor: "◆"},
		meta:     func(d *listDialog) string { return fmt.Sprint(len(d.list.Items)) },
		side: func(m *Model, item Item, width int) []string {
			p := item.Value.(review.Preview)
			head := textFor("Enter desfaz:", "Enter undoes:")
			if p.Redo {
				head = textFor("Enter refaz:", "Enter redoes:")
			}
			rows, _, _ := diffPreview(p.Diff, width, listRows)
			return append([]string{Line(on(P.Surface, P.Muted).Render(head), width, P.Surface)}, rows...)
		},
		choose: func(m *Model, item Item) tea.Cmd {
			m.dialog = nil
			return m.applyReview(item.Value.(review.Preview))
		},
	}
	return nil
}

// undoLast undoes the newest applied turn, or redoes the undone one; each
// file still goes through the approval card, which shows its diff.
func (m *Model) undoLast(redo bool) tea.Cmd {
	if m.session.ID == "" {
		m.info = textFor("Nada para desfazer nesta conversa", "Nothing to undo in this conversation")
		return nil
	}
	preview, err := reviewStore().Preview(m.session.ID, "", redo)
	switch {
	case errors.Is(err, review.ErrNothingToReview) && redo:
		m.info = textFor("Nada para refazer", "Nothing to redo")
	case errors.Is(err, review.ErrNothingToReview):
		m.info = textFor("Nada para desfazer nesta conversa", "Nothing to undo in this conversation")
	case err != nil:
		m.notice = err.Error()
	default:
		return m.applyReview(preview)
	}
	return nil
}

func (m *Model) applyReview(p review.Preview) tea.Cmd {
	if m.busy {
		m.notice = textFor("Aguarde ou cancele a execução antes de desfazer", "Wait for or cancel the run before undoing")
		return nil
	}
	id, permissions, flows := m.session.ID, m.app.Permissions, m.app.Workflows
	return func() tea.Msg {
		ctx := context.Background()
		var changed func() error
		if flows != nil {
			changed = func() error { return flows.Mutated(ctx, id) }
		}
		return reviewedMsg{redo: p.Redo, err: reviewStore().Apply(ctx, id, p, permissions, changed)}
	}
}

func (m *Model) reviewed(msg reviewedMsg) {
	switch {
	case errors.Is(msg.err, permission.ErrorPermissionDenied):
		m.info = textFor("Nada foi alterado: aprovação recusada", "Nothing changed: approval denied")
	case msg.err != nil:
		m.notice = msg.err.Error()
	case msg.redo:
		m.info = textFor("Interação refeita · rode as verificações de novo", "Turn redone · rerun your checks")
	default:
		m.info = textFor("Interação desfeita · comandos executados não são desfeitos", "Turn undone · commands that ran are not undone")
	}
}
