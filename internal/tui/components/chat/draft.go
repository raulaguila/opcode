package chat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/localstate"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
)

type RestoreDraftMsg struct {
	Text        string
	Attachments []message.Attachment
}
type SaveDraftMsg struct{}
type DraftControl interface {
	Draft() (string, []message.Attachment)
}
type DraftPersistence interface{ PersistDraft() error }
type SessionDraftPreparation interface{ PrepareSessionDraft(string) error }
type preparedSessionDraft struct {
	id          string
	draft       editorDraft
	history     []string
	keepCurrent bool
}
type editorDraft struct {
	Text        string               `json:"text"`
	Attachments []message.Attachment `json:"attachments,omitempty"`
}
type draftDocument struct {
	Root   string                 `json:"root"`
	Drafts map[string]editorDraft `json:"drafts"`
}

const maxDraftDocument = 64 << 20
const maxDraftAttachments = 16 << 20

func cloneAttachments(items []message.Attachment) []message.Attachment {
	result := slices.Clone(items)
	for i := range result {
		result[i].Content = slices.Clone(result[i].Content)
	}
	return result
}
func (m *editorCmp) Draft() (string, []message.Attachment) {
	return m.textarea.Value(), cloneAttachments(m.attachments)
}
func draftLocation() (string, string) {
	cfg := config.Get()
	if cfg == nil {
		return "", ""
	}
	root, _ := filepath.Abs(cfg.WorkingDir)
	data := cfg.Data.Directory
	if data == "" {
		data = ".opcode"
	}
	if !filepath.IsAbs(data) {
		data = filepath.Join(root, data)
	}
	return root, filepath.Join(data, "composer-drafts.json")
}
func loadDraftDocument(root, path string) (draftDocument, error) {
	document := draftDocument{Root: root, Drafts: map[string]editorDraft{}}
	if err := localstate.Read(path, maxDraftDocument, &document); errors.Is(err, os.ErrNotExist) {
		return document, nil
	} else if err != nil {
		return document, err
	}
	if document.Root != root || len(document.Drafts) > 64 {
		return document, fmt.Errorf("invalid composer drafts or workspace mismatch")
	}
	if document.Drafts == nil {
		document.Drafts = map[string]editorDraft{}
	}
	for id, draft := range document.Drafts {
		if len(id) > 128 || len(draft.Text) > 64000 || len(draft.Attachments) > maxAttachments {
			return document, fmt.Errorf("invalid stored draft")
		}
		size := 0
		for _, attachment := range draft.Attachments {
			size += len(attachment.Content)
		}
		if size > maxDraftAttachments {
			return document, fmt.Errorf("stored draft attachments exceed 16 MiB")
		}
	}
	return document, nil
}
func (m *editorCmp) PersistDraft() error {
	if m.drafts == nil {
		m.drafts = map[string]editorDraft{}
	}
	value := editorDraft{Text: m.textarea.Value(), Attachments: cloneAttachments(m.attachments)}
	return m.persistDraftValue(m.session.ID, value)
}
func (m *editorCmp) persistDraftValue(id string, value editorDraft) error {
	if m.drafts == nil {
		m.drafts = map[string]editorDraft{}
	}
	m.drafts[id] = value
	root, path := m.draftRoot, m.draftPath
	if path == "" {
		return nil
	}
	if len(value.Text) > 64000 || len(value.Attachments) > maxAttachments {
		return fmt.Errorf("draft exceeds text or attachment limits")
	}
	size := 0
	for _, attachment := range value.Attachments {
		size += len(attachment.Content)
	}
	if size > maxDraftAttachments {
		return fmt.Errorf("draft attachments exceed 16 MiB; keep the app open or remove attachments before saving")
	}
	unlock, err := localstate.LockFile(path)
	if err != nil {
		return fmt.Errorf("lock composer drafts: %w", err)
	}
	defer unlock()
	document, err := loadDraftDocument(root, path)
	if err != nil {
		return fmt.Errorf("save draft: %w", err)
	}
	if value.Text == "" && len(value.Attachments) == 0 {
		delete(document.Drafts, id)
	} else {
		document.Drafts[id] = value
	}
	if len(document.Drafts) > 64 {
		return fmt.Errorf("at most 64 nonempty composer drafts can be stored")
	}
	if err := localstate.Write(path, document, maxDraftDocument); err != nil {
		return fmt.Errorf("save draft: %w", err)
	}
	return clearLegacyDraft(id, root, path)
}

func (m *editorCmp) sessionDraft(id string) (editorDraft, error) {
	if draft, ok := m.drafts[id]; ok {
		return draft, nil
	}
	root, path := m.draftRoot, m.draftPath
	if path != "" {
		unlock, err := localstate.LockFile(path)
		if err != nil {
			return editorDraft{}, fmt.Errorf("lock composer drafts: %w", err)
		}
		document, err := loadDraftDocument(root, path)
		unlock()
		if err != nil {
			return editorDraft{}, fmt.Errorf("restore draft: %w", err)
		}
		if draft, ok := document.Drafts[id]; ok {
			return draft, nil
		}
	}
	if path != "" && id != "" {
		snapshot, _, err := session.NewCatalog(root, filepath.Dir(path)).Snapshot()
		if err != nil {
			return editorDraft{}, fmt.Errorf("restore session draft index: %w", err)
		}
		return editorDraft{Text: snapshot[id].Draft}, nil
	}
	return editorDraft{}, nil
}
func (m *editorCmp) applyDraft(draft editorDraft) {
	m.textarea.SetValue(draft.Text)
	m.textarea.CursorEnd()
	m.attachments = cloneAttachments(draft.Attachments)
	m.browsingHistory = false
	m.slash = slashState{}
}
func (m *editorCmp) restoreSessionDraft() error {
	draft, err := m.sessionDraft(m.session.ID)
	if err != nil {
		return err
	}
	m.applyDraft(draft)
	return nil
}
func (m *editorCmp) historyForSession(id string) ([]string, error) {
	history := []string{}
	if m.app != nil && m.app.Messages != nil && id != "" {
		var err error
		history, err = readPromptHistory(context.Background(), m.app.Messages, id)
		if err != nil {
			return nil, fmt.Errorf("load prompt history: %w", err)
		}
	}
	return history, nil
}

func (m *editorCmp) PrepareSessionDraft(id string) error {
	if m.preparedDraft != nil && m.preparedDraft.id == id {
		return nil
	}
	if id == m.session.ID {
		return nil
	}
	if m.session.ID == "" && m.submitting {
		m.preparedDraft = &preparedSessionDraft{id: id, keepCurrent: true}
		return nil
	}
	draft, err := m.sessionDraft(id)
	if err != nil {
		return err
	}
	history, err := m.historyForSession(id)
	if err != nil {
		return err
	}
	if err := m.PersistDraft(); err != nil {
		return err
	}
	m.preparedDraft = &preparedSessionDraft{id: id, draft: draft, history: history}
	return nil
}
func (m *editorCmp) promptHistoryKey(msg tea.KeyPressMsg) bool {
	if msg.Code != tea.KeyUp && msg.Code != tea.KeyDown {
		return false
	}
	if len(m.history) == 0 || m.deleteMode || m.submitting {
		return false
	}
	if msg.Code == tea.KeyUp && (m.browsingHistory || m.textarea.Line() == 0) {
		if !m.browsingHistory {
			m.historyDraft = m.textarea.Value()
			m.historyIndex = len(m.history)
			m.browsingHistory = true
		}
		m.historyIndex = max(0, m.historyIndex-1)
		m.textarea.SetValue(m.history[m.historyIndex])
		m.textarea.CursorEnd()
		return true
	}
	if msg.Code == tea.KeyDown && m.browsingHistory {
		m.historyIndex++
		if m.historyIndex >= len(m.history) {
			m.textarea.SetValue(m.historyDraft)
			m.browsingHistory = false
		} else {
			m.textarea.SetValue(m.history[m.historyIndex])
		}
		m.textarea.CursorEnd()
		return true
	}
	return false
}

func sameAttachments(a, b []message.Attachment) bool {
	return slices.EqualFunc(a, b, func(x, y message.Attachment) bool {
		return x.FileName == y.FileName && x.FilePath == y.FilePath && x.MimeType == y.MimeType && bytes.Equal(x.Content, y.Content)
	})
}

// The canonical document owns draft contents. Clear the old index only after
// the complete draft (including attachments) has been saved successfully.
func clearLegacyDraft(id, root, path string) error {
	if id == "" || path == "" {
		return nil
	}
	if err := session.NewCatalog(root, filepath.Dir(path)).SaveDraft(id, ""); err != nil {
		return fmt.Errorf("clear legacy session draft: %w", err)
	}
	return nil
}
