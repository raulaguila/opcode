package chat

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
)

func scrollableMessages(t *testing.T) *messagesCmp {
	t.Helper()
	m := NewMessagesCmp(&app.App{}).(*messagesCmp)
	m.SetSize(60, 14)
	m.session = session.Session{ID: "session"}
	m.currentMsgID = "reply"
	m.messages = []message.Message{{
		ID: "reply", SessionID: "session", Role: message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("history line\n\n", 60)}},
	}}
	m.renderView()
	m.viewport.GotoBottom()
	require.Positive(t, m.viewport.YOffset)
	return m
}

func wheel(button tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{X: 3, Y: 3, Button: button, Action: tea.MouseActionPress}
}

func TestMouseWheelScrollsMessages(t *testing.T) {
	m := scrollableMessages(t)
	bottom := m.viewport.YOffset
	m.Update(wheel(tea.MouseButtonWheelUp))
	require.Less(t, m.viewport.YOffset, bottom)
	m.Update(wheel(tea.MouseButtonWheelDown))
	require.Equal(t, bottom, m.viewport.YOffset)
}

func TestMessageUpdatesRespectScrollPosition(t *testing.T) {
	for _, follow := range []bool{false, true} {
		t.Run(map[bool]string{false: "reading history", true: "following response"}[follow], func(t *testing.T) {
			m := scrollableMessages(t)
			if !follow {
				m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
				require.False(t, m.viewport.AtBottom())
			}
			offset := m.viewport.YOffset
			updated := m.messages[0]
			updated.Parts = []message.ContentPart{message.TextContent{Text: strings.Repeat("history line\n\n", 80)}}
			m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: updated})
			if follow {
				require.True(t, m.viewport.AtBottom())
				require.Greater(t, m.viewport.YOffset, offset)
			} else {
				require.Equal(t, offset, m.viewport.YOffset)
				m.Update(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: message.Message{
					ID: "next-reply", SessionID: "session", Role: message.Assistant,
					Parts: []message.ContentPart{message.TextContent{Text: "next response"}},
				}})
				require.Equal(t, offset, m.viewport.YOffset, "a new assistant message must not pull the reader to the bottom")
			}
		})
	}
}

func TestReturningToBottomResumesFollowingResponse(t *testing.T) {
	m := scrollableMessages(t)
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	require.False(t, m.viewport.AtBottom())
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	require.True(t, m.viewport.AtBottom())
	updated := m.messages[0]
	updated.Parts = []message.ContentPart{message.TextContent{Text: strings.Repeat("new line\n\n", 100)}}
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: updated})
	require.True(t, m.viewport.AtBottom())
}

func TestSendingMessageReturnsToBottom(t *testing.T) {
	m := scrollableMessages(t)
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m.Update(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: message.Message{
		ID: "user", SessionID: "session", Role: message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "new question"}},
	}})
	require.True(t, m.viewport.AtBottom())
}

func TestPageNavigationPreservesEditorText(t *testing.T) {
	editor := NewEditorCmp(&app.App{}).(*editorCmp)
	editor.textarea.SetValue("unfinished question")
	for _, typ := range []tea.KeyType{tea.KeyPgUp, tea.KeyPgDown} {
		editor.Update(tea.KeyMsg{Type: typ})
		require.Equal(t, "unfinished question", editor.textarea.Value())
	}
}

type scrollProgram struct {
	messages *messagesCmp
	received bool
}

func (m *scrollProgram) Init() tea.Cmd { return nil }
func (m *scrollProgram) View() string  { return m.messages.viewport.View() }
func (m *scrollProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.MouseMsg); ok {
		m.messages.Update(msg)
		m.received = true
		return m, tea.Quit
	}
	return m, nil
}

func TestTerminalWheelInputScrollsMessages(t *testing.T) {
	m := scrollableMessages(t)
	bottom := m.viewport.YOffset
	harness := &scrollProgram{messages: m}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var output bytes.Buffer
	program := tea.NewProgram(harness,
		tea.WithContext(ctx), tea.WithAltScreen(), tea.WithMouseCellMotion(),
		tea.WithInput(strings.NewReader("\x1b[<64;4;4M")), // SGR wheel-up event
		tea.WithOutput(&output), tea.WithoutSignalHandler(),
	)
	_, err := program.Run()
	require.NoError(t, err)
	require.True(t, harness.received)
	require.Less(t, m.viewport.YOffset, bottom)
	require.Contains(t, output.String(), "\x1b[?1002h", "enable terminal mouse events")
	require.Contains(t, output.String(), "\x1b[?1002l", "restore terminal mouse state on exit")
}
