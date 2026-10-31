package chat

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
)

func TestTurnProcessCanCollapseWithoutHidingResponse(t *testing.T) {
	m := NewMessagesCmp(&app.App{}).(*messagesCmp)
	m.SetSize(80, 30)
	m.session = session.Session{ID: "s"}
	m.messages = []message.Message{
		{ID: "u", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "question"}}},
		{ID: "a", Role: message.Assistant, Parts: []message.ContentPart{message.ReasoningContent{Thinking: "process details"}}},
		{ID: "final", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "visible response"}}},
	}
	m.renderView()
	require.Contains(t, ansi.Strip(m.viewport.View()), config.Text("Detalhes", "Details"))
	require.NotContains(t, ansi.Strip(m.viewport.View()), "Copiar resposta")
	require.NotContains(t, ansi.Strip(m.viewport.View()), "process details")
	require.Contains(t, ansi.Strip(m.viewport.View()), "visible response")
	m.Update(ToggleProcessMsg{})
	require.Contains(t, ansi.Strip(m.viewport.View()), "process details")
	require.Contains(t, ansi.Strip(m.viewport.View()), "visible response")
	m.Update(ToggleProcessMsg{})
	require.NotContains(t, ansi.Strip(m.viewport.View()), "process details")
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	action, ok := cmd().(ResponseActionsMsg)
	require.True(t, ok)
	require.Equal(t, "visible response", action.Text)
}
func TestUnreadIndicatorAndJumpToLatest(t *testing.T) {
	m := scrollableMessages(t)
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	old := m.viewport.YOffset
	updated := m.messages[0]
	updated.AppendContent("new streaming text")
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: updated})
	require.True(t, m.unread)
	require.Equal(t, old, m.viewport.YOffset)
	require.Contains(t, m.working(), config.Text("Novas mensagens", "New messages"))
	m.Update(JumpToLatestMsg{})
	require.False(t, m.unread)
	require.True(t, m.viewport.AtBottom())
}
