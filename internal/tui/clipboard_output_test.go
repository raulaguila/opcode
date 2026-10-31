package tui

import (
	"bytes"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/clipboard"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestOSC52UsesSerializedTerminalWriter(t *testing.T) {
	var out bytes.Buffer
	writer := NewTerminalWriter(&out)
	sequence, err := clipboard.OSC52("ação")
	require.NoError(t, err)
	a := appModel{terminalOutput: writer}
	cmd, handled := a.productivityMessage(page.TerminalClipboardMsg{Sequence: sequence})
	require.True(t, handled)
	require.NotNil(t, cmd)
	require.IsType(t, page.WorkspaceResultMsg{}, cmd())
	require.Equal(t, sequence, out.String())
	require.NotContains(t, out.String(), "ação")
	require.False(t, clipboard.ValidOSC52("\x1b]52;c;"+strings.Repeat("a", 100000)+"\x07"))
}

func TestExplicitSelectionCopyFallsBackThroughSerializedTerminalOutput(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	t.Setenv("PATH", t.TempDir())
	var out bytes.Buffer
	a := appModel{terminalOutput: NewTerminalWriter(&out)}
	text := "selected text only"
	cmd, handled := a.productivityMessage(util.CopyTextMsg{Text: text})
	require.True(t, handled)
	result := cmd().(util.InfoMsg)
	require.NotEqual(t, util.InfoTypeError, result.Type)
	want, err := clipboard.OSC52(text)
	require.NoError(t, err)
	require.Equal(t, want, out.String())
}

func TestTerminalOutputAcceptsInitialPointerAndUpdatedValue(t *testing.T) {
	for _, initial := range []tea.Model{&appModel{}, appModel{}} {
		var out bytes.Buffer
		model := WithTerminalOutput(initial, &out)
		var a appModel
		switch m := model.(type) {
		case *appModel:
			a = *m
		case appModel:
			a = m
		default:
			t.Fatalf("unexpected model type %T", model)
		}
		sequence, err := clipboard.OSC52("initial model")
		require.NoError(t, err)
		cmd, handled := a.productivityMessage(page.TerminalClipboardMsg{Sequence: sequence})
		require.True(t, handled)
		require.IsType(t, page.WorkspaceResultMsg{}, cmd())
		require.Equal(t, sequence, out.String())
	}
}
