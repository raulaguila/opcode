package util

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestTranslateMousePreservesKindButtonAndModifiers(t *testing.T) {
	m := tea.Mouse{X: 17, Y: 12, Button: tea.MouseLeft, Mod: tea.ModShift | tea.ModAlt}
	for _, msg := range []tea.MouseMsg{tea.MouseClickMsg(m), tea.MouseReleaseMsg(m), tea.MouseMotionMsg(m), tea.MouseWheelMsg(m)} {
		got := TranslateMouse(msg, -4, -3)
		require.Equal(t, reflect.TypeOf(msg), reflect.TypeOf(got))
		require.Equal(t, tea.Mouse{X: 13, Y: 9, Button: m.Button, Mod: m.Mod}, got.Mouse())
	}
}
