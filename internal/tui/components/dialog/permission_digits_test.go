package dialog

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestPermissionDigitsMatchTheNumberedButtons(t *testing.T) {
	for digit, binding := range map[rune]key.Binding{'1': permissionsKeys.Allow, '2': permissionsKeys.AllowSession, '3': permissionsKeys.Deny} {
		require.True(t, key.Matches(tea.KeyPressMsg{Code: digit, Text: string(digit)}, binding), string(digit))
	}
}
