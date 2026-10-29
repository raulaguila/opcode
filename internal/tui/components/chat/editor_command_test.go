package chat

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEditorArgumentsAreLiteralWithoutShellEvaluation(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want []string
	}{
		{`code --wait`, []string{"code", "--wait"}},
		{`"/path with spaces/editor" --name 'two words'`, []string{"/path with spaces/editor", "--name", "two words"}},
		{`editor '$(do-not-run)' ';' '$TOKEN'`, []string{"editor", "$(do-not-run)", ";", "$TOKEN"}},
	} {
		got, err := editorArguments(tc.raw)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
	for _, raw := range []string{"", `editor "unfinished`, `editor \`} {
		_, err := editorArguments(raw)
		require.Error(t, err)
	}
}
