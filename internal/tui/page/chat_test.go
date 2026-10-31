package page

import (
	"github.com/raulaguila/opcode/internal/tui/util"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/stretchr/testify/require"
)

type mouseMessages struct {
	layout.Container
	received []tea.Msg
}

func (m *mouseMessages) GetSize() (int, int) { return 70, 20 }
func (m *mouseMessages) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	m.received = append(m.received, msg)
	return m, nil
}

func TestChatWheelOnlyReachesMessagesUnderPointer(t *testing.T) {
	for _, tc := range []struct {
		name       string
		x, y       int
		completion bool
		want       int
	}{
		{name: "messages", x: 10, y: 10, want: 1},
		{name: "sidebar", x: 75, y: 10},
		{name: "editor", x: 10, y: 21},
		{name: "outside", x: -1, y: 10},
		{name: "completion dialog", x: 10, y: 10, completion: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			messages := &mouseMessages{}
			p := &chatPage{messages: messages, showCompletionDialog: tc.completion}
			p.Update(tea.MouseWheelMsg{X: tc.x, Y: tc.y, Button: tea.MouseWheelUp})
			require.Len(t, messages.received, tc.want)
		})
	}
}
