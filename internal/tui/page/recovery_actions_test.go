package page

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/stretchr/testify/require"
)

func recoveryActionY(p *chatPage, row int) int {
	_, height := p.layout.GetSize()
	_, editorHeight := p.editor.GetSize()
	return height - editorHeight + p.workflowHeight() + 1 + row
}

func TestRecoveryTaskClickOpensTaskAndKeepsDraft(t *testing.T) {
	p, _ := workspacePage(t)
	p.recovery.err = errors.New("agent step limit reached")
	p.editor.Update(chat.RestoreDraftMsg{Text: "KEEP_RECOVERY_DRAFT"})
	p.SetSize(90, 30)
	rows, actions := p.recoveryLayout()
	require.Contains(t, rows[actions[0].row], "/task")
	p.recoveryMouse(tea.MouseClickMsg{X: actions[0].start + 1, Y: recoveryActionY(p, actions[0].row), Button: tea.MouseLeft})
	require.Equal(t, "task", p.workspace.kind)
	draft, _ := p.composer.(chat.DraftControl).Draft()
	require.Equal(t, "KEEP_RECOVERY_DRAFT", draft)
}

func TestRecoveryClickIgnoresBlankOutsideAndHiddenActions(t *testing.T) {
	p, _ := workspacePage(t)
	p.recovery.err = errors.New("agent step limit reached")
	p.SetSize(90, 30)
	_, actions := p.recoveryLayout()
	y := recoveryActionY(p, actions[0].row)
	for _, point := range [][2]int{{-1, y}, {0, y}, {actions[0].end, y}, {actions[len(actions)-1].end, y}, {p.width, y}, {actions[0].start, y - 1}, {actions[0].start, y + 1}} {
		require.Nil(t, p.recoveryMouse(tea.MouseClickMsg{X: point[0], Y: point[1], Button: tea.MouseLeft}))
		require.Empty(t, p.workspace.kind)
	}
	p.recovery.resolved = true
	require.Nil(t, p.recoveryMouse(tea.MouseClickMsg{X: actions[0].start, Y: y, Button: tea.MouseLeft}))
	p.recovery.resolved = false
	p.SetSize(19, 30)
	require.Nil(t, p.recoveryMouse(tea.MouseClickMsg{X: 1, Y: y, Button: tea.MouseLeft}))
	require.Empty(t, p.workspace.kind)
}

func TestRecoveryCompactActionsPreserveCommandsAndExactVisibleCells(t *testing.T) {
	ensureSetupTestConfig(t)
	previous := config.Get().TUI.Language
	t.Cleanup(func() { config.Get().TUI.Language = previous })
	for _, language := range []string{"en", "pt"} {
		config.Get().TUI.Language = language
		for _, width := range []int{20, 36, 60, 90, 180} {
			for _, text := range []string{"model output token limit reached", "source citation remains missing after repair", "agent step limit reached", "401 unauthorized", "context budget exceeded", "out of memory", "connection refused", "modified externally", "interrupted"} {
				t.Run(fmt.Sprintf("%s/%d/%s", language, width, text), func(t *testing.T) { assertRecoveryActionLayout(t, width, errors.New(text)) })
			}
		}
	}
}

func assertRecoveryActionLayout(t *testing.T, width int, err error) {
	t.Helper()
	rows, actions := layoutRecoveryActions(err, width, false)
	require.Len(t, actions, len(recoveryActionOptions(err, false)))
	if width >= 36 {
		require.Len(t, rows, 1, "compact recovery groups must fit the normal two-row panel")
	}
	for _, row := range rows {
		require.LessOrEqual(t, ansi.StringWidth(row), width)
		require.NotContains(t, row, "…", "actions must remain available instead of clipped")
	}
	for _, action := range actions {
		visible := ansi.Cut(rows[action.row], action.start, action.end)
		require.Equal(t, "/"+action.command, strings.Fields(visible)[0])
		require.Equal(t, ansi.StringWidth(visible), action.end-action.start)
	}
}

func TestRecoveryWrappedActionUsesItsRenderedRow(t *testing.T) {
	p, _ := workspacePage(t)
	p.recovery.err = errors.New("out of memory")
	p.SetSize(20, 20)
	rows, actions := p.recoveryLayout()
	require.Greater(t, len(rows), 1)
	require.Equal(t, 2+len(rows), p.recoveryHeight())
	last := actions[len(actions)-1]
	require.Equal(t, "error", last.command)
	p.recoveryMouse(tea.MouseClickMsg{X: last.start, Y: recoveryActionY(p, last.row), Button: tea.MouseLeft})
	require.Equal(t, "error", p.workspace.kind)
}

func TestRecoveryOffersUndoOnlyWhenTheTaskChangedFiles(t *testing.T) {
	err := errors.New("context budget exceeded")
	require.NotContains(t, recoveryActionText(recoveryActionOptions(err, false)[0], false), "/undo")
	require.Equal(t, "undo", recoveryActionOptions(err, true)[0].command)
	for _, width := range []int{36, 60, 110} {
		rows, actions := layoutRecoveryActions(err, width, true)
		for _, row := range rows {
			require.LessOrEqual(t, ansi.StringWidth(row), width)
		}
		require.Len(t, actions, len(recoveryActionOptions(err, true)))
	}
}
