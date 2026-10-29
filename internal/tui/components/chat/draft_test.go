package chat

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func draftEditor(root string) *editorCmp {
	return newEditorWithDraftStorage(&app.App{}, root, filepath.Join(root, "state", "composer-drafts.json"))
}
func TestFirstSessionSelectionKeepsSubmittingDraftAndAttachmentsOnFailure(t *testing.T) {
	m := NewEditorCmp(&app.App{}).(*editorCmp)
	m.textarea.SetValue("new request")
	m.attachments = []message.Attachment{{FileName: "plot.png", Content: []byte{1, 2}}}
	m.send()
	m.Update(SessionSelectedMsg(session.Session{ID: "first"}))
	require.Equal(t, "new request", m.textarea.Value())
	require.Len(t, m.attachments, 1)
	m.Update(SubmissionResultMsg{Text: "new request", Error: errors.New("queue rejected")})
	require.Equal(t, "new request", m.textarea.Value())
	require.Len(t, m.attachments, 1)
}
func TestDraftPersistenceRestoresUnassignedAndSessionAttachments(t *testing.T) {
	root := t.TempDir()
	m := draftEditor(root)
	m.textarea.SetValue("not sent yet")
	m.attachments = []message.Attachment{{FileName: "image.png", FilePath: "/selected/image.png", MimeType: "image/png", Content: []byte("imagebytes")}}
	require.NoError(t, m.PersistDraft())
	restored := draftEditor(root)
	require.NoError(t, restored.draftInitError)
	require.Equal(t, "not sent yet", restored.textarea.Value())
	require.Equal(t, m.attachments, restored.attachments)
	restored.attachments[0].Content[0] = 'X'
	require.Equal(t, byte('i'), m.attachments[0].Content[0])
	m.Update(SessionSelectedMsg(session.Session{ID: "session-a"}))
	m.textarea.SetValue("session draft")
	m.attachments = []message.Attachment{{FileName: "a.png", Content: []byte("a")}}
	require.NoError(t, m.PersistDraft())
	fresh := draftEditor(root)
	fresh.Update(SessionSelectedMsg(session.Session{ID: "session-a"}))
	require.Equal(t, "session draft", fresh.textarea.Value())
	require.Len(t, fresh.attachments, 1)
	info, err := os.Stat(filepath.Join(root, "state", "composer-drafts.json"))
	require.NoError(t, err)
	require.EqualValues(t, 0600, info.Mode().Perm())
}
func TestDraftSaveAndRestoreFailuresAreReportedAndDoNotSwitchSession(t *testing.T) {
	root := t.TempDir()
	m := draftEditor(root)
	m.session = session.Session{ID: "old"}
	m.textarea.SetValue("keep draft")
	m.attachments = []message.Attachment{{FileName: "keep.png"}}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "state"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "state", "composer-drafts.json"), []byte("broken"), 0600))
	_, cmd := m.Update(SessionSelectedMsg(session.Session{ID: "next"}))
	require.NotNil(t, cmd)
	notice := cmd().(util.InfoMsg)
	require.Equal(t, util.InfoTypeError, notice.Type)
	require.Equal(t, "old", m.session.ID)
	require.Equal(t, "keep draft", m.textarea.Value())
	require.Len(t, m.attachments, 1)
	require.Error(t, m.PersistDraft())
	newEditor := draftEditor(root)
	require.Error(t, newEditor.draftInitError)
	require.NotNil(t, newEditor.Init())
	_, cmd = m.Update(SaveDraftMsg{})
	require.NotNil(t, cmd)
	require.Equal(t, util.InfoTypeError, cmd().(util.InfoMsg).Type)
}
func TestAcceptedFirstSubmissionDoesNotResurrectUnassignedDraft(t *testing.T) {
	root := t.TempDir()
	m := draftEditor(root)
	m.textarea.SetValue("send once")
	require.NoError(t, m.PersistDraft())
	m.send()
	m.Update(SessionSelectedMsg(session.Session{ID: "created"}))
	m.Update(SubmissionResultMsg{Text: "send once"})
	require.Empty(t, m.textarea.Value())
	fresh := draftEditor(root)
	require.NoError(t, fresh.draftInitError)
	require.Empty(t, fresh.textarea.Value())
}
func TestPromptHistoryRestoresOriginalDraftAndAttachments(t *testing.T) {
	m := NewEditorCmp(&app.App{}).(*editorCmp)
	m.textarea.SetValue("work in progress")
	m.attachments = []message.Attachment{{FileName: "draft.png", Content: []byte("kept")}}
	m.history = []string{"old request"}
	require.True(t, m.promptHistoryKey(tea.KeyMsg{Type: tea.KeyUp}))
	require.Equal(t, "old request", m.textarea.Value())
	require.True(t, m.promptHistoryKey(tea.KeyMsg{Type: tea.KeyDown}))
	require.Equal(t, "work in progress", m.textarea.Value())
	require.Equal(t, "draft.png", m.attachments[0].FileName)
	require.Equal(t, []byte("kept"), m.attachments[0].Content)
}

func TestExternalEditorResultPreservesEditedDraftAndAttachmentsUntilAck(t *testing.T) {
	m := NewEditorCmp(&app.App{}).(*editorCmp)
	m.textarea.SetValue("old draft")
	m.attachments = []message.Attachment{{FileName: "keep.png", Content: []byte("image")}}
	_, cmd := m.Update(editorResultMsg{Text: "edited draft"})
	require.Nil(t, cmd, "closing the editor must not submit")
	require.Equal(t, "edited draft", m.textarea.Value())
	require.Len(t, m.attachments, 1)
	require.False(t, m.submitting)
	require.NotNil(t, m.send(), "sending remains explicit")
	m.Update(SubmissionResultMsg{Text: "edited draft", Error: errors.New("rejected")})
	require.Equal(t, "edited draft", m.textarea.Value())
	require.Len(t, m.attachments, 1)
}

func TestSubmissionAckPreservesAttachmentAddedAfterSend(t *testing.T) {
	m := NewEditorCmp(&app.App{}).(*editorCmp)
	m.textarea.SetValue("prompt")
	m.send()
	m.attachments = []message.Attachment{{FileName: "new.png", Content: []byte("new")}}
	m.Update(SubmissionResultMsg{Text: "prompt"})
	require.Equal(t, "prompt", m.textarea.Value())
	require.Len(t, m.attachments, 1)
}

func TestPreparedSessionDraftUsesSnapshotAfterRootSelectionWithoutSecondIO(t *testing.T) {
	root := t.TempDir()
	m := draftEditor(root)
	m.session = session.Session{ID: "origin"}
	m.textarea.SetValue("original draft")
	require.NoError(t, m.persistDraftValue("target", editorDraft{Text: "target draft", Attachments: []message.Attachment{{FileName: "target.png", Content: []byte("kept")}}}))
	require.NoError(t, m.PrepareSessionDraft("target"))
	require.NotNil(t, m.preparedDraft)
	require.NoError(t, os.WriteFile(filepath.Join(root, "state", "composer-drafts.json"), []byte("changed after preflight"), 0600))
	require.NoError(t, m.PrepareSessionDraft("target"), "repeated preflight must use its prepared snapshot")
	_, cmd := m.Update(SessionSelectedMsg(session.Session{ID: "target"}))
	require.Nil(t, cmd)
	require.Equal(t, "target", m.session.ID)
	require.Equal(t, "target draft", m.textarea.Value())
	require.Equal(t, "target.png", m.attachments[0].FileName)
}
func TestPreparedDraftIsInvalidatedByNewInput(t *testing.T) {
	root := t.TempDir()
	m := draftEditor(root)
	m.session = session.Session{ID: "origin"}
	m.textarea.SetValue("original")
	require.NoError(t, m.PrepareSessionDraft("target"))
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" changed")})
	require.Nil(t, m.preparedDraft)
	require.NoError(t, os.WriteFile(filepath.Join(root, "state", "composer-drafts.json"), []byte("broken"), 0600))
	require.Error(t, m.PrepareSessionDraft("target"))
	require.Equal(t, "origin", m.session.ID)
	require.Contains(t, m.textarea.Value(), "changed")
}
func TestPreparedClearAndInitialSubmissionKeepTheirDrafts(t *testing.T) {
	root := t.TempDir()
	m := draftEditor(root)
	m.session = session.Session{ID: "origin"}
	m.textarea.SetValue("remember origin")
	require.NoError(t, m.PrepareSessionDraft(""))
	require.NoError(t, os.WriteFile(filepath.Join(root, "state", "composer-drafts.json"), []byte("corrupt later"), 0600))
	_, cmd := m.Update(SessionClearedMsg{})
	require.Nil(t, cmd)
	require.Empty(t, m.session.ID)
	require.Empty(t, m.textarea.Value())
	pending := NewEditorCmp(&app.App{}).(*editorCmp)
	pending.textarea.SetValue("new submission")
	pending.send()
	require.NoError(t, pending.PrepareSessionDraft("created"))
	pending.Update(SessionSelectedMsg(session.Session{ID: "created"}))
	require.Equal(t, "new submission", pending.textarea.Value())
	require.True(t, pending.submitting)
}
