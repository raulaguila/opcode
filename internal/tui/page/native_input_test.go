package page

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/stretchr/testify/require"
)

func TestNativePasteTargetsQuestionWithoutSendingOrChangingDraft(t *testing.T) {
	p := workflowPage(t)
	p.editor.Update(chat.RestoreDraftMsg{Text: "composer draft"})
	p.workflow.answer = textinput.New()
	p.workflow.question = interaction.Question{ID: "q", Question: "Choose next step", Choices: []string{"A", "B"}}
	p.workflow.choice = 0
	p.paste(tea.PasteMsg{Content: "Custom response"})
	require.Equal(t, 2, p.workflow.choice)
	require.Equal(t, "Custom response", p.workflow.answer.Value())
	require.Contains(t, p.editor.View(), "composer draft")
	require.Equal(t, "q", p.workflow.question.ID, "paste must not submit the selected option")
	require.Zero(t, p.app.CoderAgent.(*workflowProbe).canceled)
}

func TestTerminalChromeKeepsDividerAndFitsLongHeader(t *testing.T) {
	p := workflowPage(t)
	p.SetSize(60, 20)
	p.terminal.snapshot.Command = strings.Repeat("long command ", 10)
	p.terminal.snapshot.ID = "running"
	p.terminal.snapshot.Running = true
	view := ansi.Strip(p.terminalView(true))
	lines := strings.Split(view, "\n")
	require.Len(t, lines, 20)
	require.Equal(t, strings.Repeat("─", 60), lines[len(lines)-2])
	require.Contains(t, lines[len(lines)-1], "Ctrl+P")
}
