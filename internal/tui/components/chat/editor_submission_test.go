package chat

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/stretchr/testify/require"
)

func TestNewlineShortcutsPreserveDraftAndEnterStillSubmits(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 'j', Mod: tea.ModCtrl}, {Code: tea.KeyEnter, Mod: tea.ModAlt}, {Code: tea.KeyEnter, Mod: tea.ModShift}} {
		t.Run(key.String(), func(t *testing.T) {
			m := slashEditor()
			enterText(m, "first line")
			require.Nil(t, editorKey(m, key))
			enterText(m, "second line")
			require.Equal(t, "first line\nsecond line", m.textarea.Value())
			require.False(t, m.submitting)
			require.NotNil(t, editorKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}))
			require.True(t, m.submitting)
		})
	}
	m := slashEditor()
	enterText(m, "legacy\\")
	require.Nil(t, editorKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}))
	require.Equal(t, "legacy\n", m.textarea.Value())
}

func TestEditorPreservesDraftUntilSubmissionIsAccepted(t *testing.T) {
	m := NewEditorCmp(&app.App{}).(*editorCmp)
	m.textarea.SetValue("new instruction")
	require.NotNil(t, m.send())
	require.Equal(t, "new instruction", m.textarea.Value())
	require.Nil(t, m.send(), "do not submit twice while awaiting acceptance")
	m.Update(SubmissionResultMsg{Text: "new instruction", Error: errors.New("queue full")})
	require.Equal(t, "new instruction", m.textarea.Value())
	require.NotNil(t, m.send())
	m.Update(SubmissionResultMsg{Text: "new instruction"})
	require.Empty(t, m.textarea.Value())
}

func TestSubmissionDoesNotEraseTextTypedAfterSend(t *testing.T) {
	m := NewEditorCmp(&app.App{}).(*editorCmp)
	m.textarea.SetValue("first")
	m.send()
	m.textarea.SetValue("second draft")
	m.Update(SubmissionResultMsg{Text: "first"})
	require.Equal(t, "second draft", m.textarea.Value())
}
