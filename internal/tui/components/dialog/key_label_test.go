package dialog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeyLabelMatchesFooterStyle(t *testing.T) {
	for in, want := range map[string]string{
		"ctrl+l":                  "Ctrl+L",
		"alt+pgdn":                "Alt+PgDn",
		"shift+enter / alt+enter": "Shift+Enter / Alt+Enter",
		"ctrl+r+{i}":              "Ctrl+R+{i}",
		"@":                       "@",
		"esc":                     "Esc",
	} {
		require.Equal(t, want, keyLabel(in))
	}
}
