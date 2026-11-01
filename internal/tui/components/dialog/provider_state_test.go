package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestFormStateFollowsPresetsAndFormWithoutTheSecret(t *testing.T) {
	m := settingsDialog(t)
	_, ok := m.FormState()
	require.False(t, ok, "the list is not a form")
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	state, ok := m.FormState()
	require.True(t, ok)
	require.NotEmpty(t, state.Presets)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	state, ok = m.FormState()
	require.True(t, ok)
	require.Empty(t, state.Presets)
	require.True(t, state.Fields[0].Focused)
	var auth ProviderFormField
	for _, field := range state.Fields {
		if len(field.Options) == 4 {
			auth = field
		}
	}
	require.Equal(t, "none", auth.Value)
	require.Len(t, auth.OptionLabels, 4)
}
