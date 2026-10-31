package dialog

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"
)

func TestInitializationLeavesFooterVisibleInShortTerminal(t *testing.T) {
	m := NewInitDialogCmp()
	m.SetSize(60, 20)
	view := m.View()
	require.LessOrEqual(t, lipgloss.Width(view), 59)
	require.LessOrEqual(t, lipgloss.Height(view), 18, "leave one row for the shadow and one for the footer")
}
