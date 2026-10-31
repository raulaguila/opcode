package page

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestChatFrameFitsWithDraftPlanRecoveryAndQuestion(t *testing.T) {
	for _, state := range []string{"empty", "draft", "plan", "recovery", "question"} {
		t.Run(state, func(t *testing.T) {
			p := workflowPage(t)
			p.app.CoderAgent = &workspaceAgent{}
			p.workflow.expanded = state == "plan"
			p.workflow.task.Steps = []workflow.Step{{Title: "Verificar renderização", Status: "in_progress"}}
			p.recovery = recoveryState{}
			p.workflow.question = interaction.Question{}
			if state == "recovery" {
				p.recovery.err = errors.New(strings.Repeat("erro explicável ", 40))
			}
			if state == "question" {
				p.workflow.question = interaction.Question{ID: "q", Question: "Qual opção?", Choices: []string{"ação", "漢字"}}
				p.workflow.answer = textinput.New()
				p.workflow.answer.Prompt = "Answer: "
			}
			text := ""
			if state != "empty" {
				text = strings.Repeat("ação 漢字\n", 7) + "última linha"
			}
			p.editor.Update(chat.RestoreDraftMsg{Text: text})
			testChatFrameFitsWithDraftPlanRecoveryAndQuestionState(state, t, p)
		})
	}
}

type composerFocusProbe struct {
	chat.SlashCompleter
	focused bool
}

func (p *composerFocusProbe) SetInputFocused(value bool) tea.Cmd { p.focused = value; return nil }

func TestComposerAndQuestionReleaseFocusForTerminalWorkspaceAndModals(t *testing.T) {
	p := workflowPage(t)
	probe := &composerFocusProbe{SlashCompleter: p.composer}
	p.composer = probe
	p.SetInputFocused(true)
	require.True(t, probe.focused)
	p.terminal.focused = true
	p.syncInputFocus()
	require.False(t, probe.focused)
	p.terminal.focused = false
	p.workspace.kind = "context"
	p.workspace.fields = []textinput.Model{textinput.New(), textinput.New()}
	p.workspace.focus = 1
	p.workspace.fields[1].SetValue("draft connection")
	p.syncInputFocus()
	require.False(t, probe.focused)
	require.True(t, p.workspace.fields[1].Focused())
	p.SetInputFocused(false)
	require.False(t, p.workspace.fields[1].Focused())
	p.SetInputFocused(true)
	require.True(t, p.workspace.fields[1].Focused())
	require.False(t, p.workspace.fields[0].Focused())
	require.Equal(t, "draft connection", p.workspace.fields[1].Value())
	p.workspace.kind = ""
	p.workflow.question = interaction.Question{ID: "q"}
	p.workflow.answer = textinput.New()
	p.syncInputFocus()
	require.False(t, probe.focused)
	require.True(t, p.workflow.answer.Focused())
	p.SetInputFocused(false)
	require.False(t, p.workflow.answer.Focused())
	p.SetInputFocused(true)
	require.True(t, p.workflow.answer.Focused())
	p.workflow.question = interaction.Question{}
	p.syncInputFocus()
	require.True(t, probe.focused)
	require.False(t, p.workflow.answer.Focused())
}

func TestBareModeOpensChooserWithoutChangingSessionMode(t *testing.T) {
	p := workflowPage(t)
	before := p.mode()
	cmd, handled := p.handleWorkflowMessage("/mode")
	require.True(t, handled)
	found := false
	for _, msg := range flattenCommands(cmd) {
		if _, ok := msg.(OpenModeChooserMsg); ok {
			found = true
		}
	}
	require.True(t, found)
	require.Equal(t, before, p.mode())
}

func testChatFrameFitsWithDraftPlanRecoveryAndQuestionState(state string, t *testing.T, p *chatPage) {
	for _, size := range [][2]int{{180, 40}, {80, 24}, {60, 18}, {36, 12}} {
		p.SetSize(size[0], size[1])
		view := p.View()
		require.Equal(t, size[1], lipgloss.Height(view), state)
		require.LessOrEqual(t, lipgloss.Width(view), size[0], state)
		prompts := 0
		for line := range strings.SplitSeq(ansi.Strip(view), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), ">") {
				prompts++
			}
		}
		require.Equal(t, 1, prompts, state)
	}
}
