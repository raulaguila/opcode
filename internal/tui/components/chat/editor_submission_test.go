package chat

import (
	"errors"
	"testing"

	"github.com/raulaguila/opcode/internal/app"
	"github.com/stretchr/testify/require"
)

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
