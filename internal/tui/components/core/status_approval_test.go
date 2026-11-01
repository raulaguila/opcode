package core

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/stretchr/testify/require"
)

func TestFooterApprovalLabelOnlyPromisesEditsInExecuteAndWhenItFits(t *testing.T) {
	state := util.FooterState{Mode: "execute", ApprovalPolicy: "project_edits", Activity: "Generating response", Busy: true, ElapsedKnown: true, ElapsedSeconds: 3}
	for _, width := range []int{60, 80, 120, 180} {
		m := statusCmp{width: width}
		view := ansi.Strip(m.ViewFor(state))
		if width >= 140 {
			require.Contains(t, view, config.Text("Edições no projeto", "Edits allowed"))
		} else {
			require.NotContains(t, view, "Edits allowed")
		}
		require.Equal(t, "mode", m.ActionAt(1, state))
		require.Equal(t, "models", m.ActionAt(width-2, state))
	}
	state.Mode = "ask"
	require.NotContains(t, ansi.Strip((statusCmp{width: 180}).ViewFor(state)), "Edits allowed")
}
