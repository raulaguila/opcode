package page

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestWorkspaceBackRestoresPositionAndRejectsLateResults(t *testing.T) {
	p, _ := workspacePage(t)
	p.openTask()
	p.workspace.kind = "changes"
	p.workspace.body = strings.Repeat("Task line\n", 50)
	p.workspace.view.SetContent(p.workspace.body)
	p.workspace.view.SetYOffset(8)
	for i, a := range p.workspace.actions {
		if a.text == "context" {
			p.workspace.selected = i
		}
	}
	selected := p.workspace.selected
	cmd := p.workspaceChoose()
	require.NotNil(t, cmd)
	require.Equal(t, "context", p.workspace.kind)
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, "changes", p.workspace.kind)
	require.Equal(t, selected, p.workspace.selected)
	require.Equal(t, 8, p.workspace.view.YOffset)
	// Late I/O must not replace the parent after returning.
	p.workspaceResult(cmd().(WorkspaceResultMsg))
	require.Equal(t, "changes", p.workspace.kind)
	require.NotContains(t, p.workspace.body, "Estimated room")
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.False(t, p.WorkspaceFocused())
}

type planActionProbe struct {
	*workflowProbe
	submitted int
}

func (p *planActionProbe) Submit(context.Context, string, string, ...message.Attachment) (<-chan agent.AgentEvent, bool, error) {
	p.submitted++
	return nil, false, nil
}

func TestPlanActionsRequireExplicitExecutionAndPreserveDraft(t *testing.T) {
	p := workflowPage(t)
	probe := &planActionProbe{workflowProbe: p.app.CoderAgent.(*workflowProbe)}
	p.app.CoderAgent = probe
	_, err := p.app.Workflows.UpdatePlan(context.Background(), "s", "Work", []workflow.Step{{Title: "Review", Status: "pending"}}, []string{"Reviewed"}, nil)
	require.NoError(t, err)
	require.NoError(t, probe.SetMode(context.Background(), "s", workflow.Plan))
	p.refreshWorkflow()
	p.editor.Update(chat.RestoreDraftMsg{Text: "my draft", Attachments: []message.Attachment{{FileName: "preserved.txt", Content: []byte("draft")}}})
	p.openTask()
	p.planAction("execute-plan")
	require.Equal(t, workflow.Plan, p.mode())
	require.Zero(t, probe.submitted)
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.Zero(t, probe.submitted)
	p.planAction("adjust-plan")
	text, _ := p.composer.(chat.DraftControl).Draft()
	require.Equal(t, "my draft", text)
	p.planAction("execute-plan")
	cmd := p.planAction("confirm-plan")
	require.Equal(t, 1, probe.submitted)
	require.Equal(t, workflow.Execute, p.mode())
	for _, msg := range flattenCommands(cmd) {
		p.editor.Update(msg)
	}
	text, _ = p.composer.(chat.DraftControl).Draft()
	require.Equal(t, "my draft", text)
	p.editor.Update(chat.SubmissionResultMsg{Text: "my draft"})
	p.editor.Update(chat.RestoreDraftMsg{Attachments: []message.Attachment{{FileName: "only-attachment.txt", Content: []byte("keep")}}})
	for _, msg := range flattenCommands(p.planAction("confirm-plan")) {
		p.editor.Update(msg)
	}
	_, files := p.composer.(chat.DraftControl).Draft()
	require.Len(t, files, 1)
	require.Equal(t, "only-attachment.txt", files[0].FileName)
}

func TestContextDetailsAreExplicitAndDoNotCompact(t *testing.T) {
	p, a := workspacePage(t)
	cmd := p.openContext(false)
	p.workspaceResult(cmd().(WorkspaceResultMsg))
	require.Contains(t, p.workspace.body, config.Text("Há espaço estimado", "Estimated room"))
	require.NotContains(t, p.workspace.body, "\"context_window\"")
	require.Contains(t, p.workspace.details, "\"context_window\"")
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	require.True(t, p.workspace.showDetails)
	require.Zero(t, a.compact)
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	require.False(t, p.workspace.showDetails)
}

func TestTaskPanelsReturnAndRefreshWithoutLosingDraft(t *testing.T) {
	p := workflowPage(t)
	_, err := p.app.Workflows.UpdatePlan(context.Background(), "s", "Review safely", []workflow.Step{{Title: "Inspect", Status: "pending"}}, []string{"Explained"}, nil)
	require.NoError(t, err)
	p.editor.Update(chat.RestoreDraftMsg{Text: "keep this"})
	p.openTask()
	for i, a := range p.workspace.actions {
		if a.text == "contract" {
			p.workspace.selected = i
		}
	}
	selected := p.workspace.selected
	p.workspaceChoose()
	require.Equal(t, "contract-kinds", p.workspace.kind)
	p.workspaceChoose()
	require.Equal(t, "contract", p.workspace.kind)
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, "contract-kinds", p.workspace.kind)
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, "task", p.workspace.kind)
	require.Equal(t, selected, p.workspace.selected)
	_, err = p.app.Workflows.UpdatePlan(context.Background(), "s", "Updated objective", []workflow.Step{{Title: "Inspect", Status: "completed"}}, []string{"Explained"}, nil)
	require.NoError(t, err)
	p.Update(TerminalTickMsg{})
	require.Contains(t, p.workspace.body, "Updated objective")
	require.Equal(t, selected, p.workspace.selected)
	draft, _ := p.composer.(chat.DraftControl).Draft()
	require.Equal(t, "keep this", draft)
	p.workspaceCommand("terminal", "")
	require.False(t, p.WorkspaceFocused())
	require.True(t, p.terminal.focused)
}

func TestTaskAddsPlanActionsWithoutMovingSelectedCommand(t *testing.T) {
	p := workflowPage(t)
	p.openTask()
	for i, action := range p.workspace.actions {
		require.NotEqual(t, "execute-plan", action.kind)
		if action.text == "context" {
			p.workspace.selected = i
		}
	}
	_, err := p.app.Workflows.UpdatePlan(context.Background(), "s", "New objective", []workflow.Step{{Title: "Inspect", Status: "pending"}}, []string{"Explained"}, nil)
	require.NoError(t, err)
	p.Update(TerminalTickMsg{})
	require.Equal(t, "context", p.workspace.actions[p.workspace.selected].text)
	require.Equal(t, "execute-plan", p.workspace.actions[1].kind)
	require.Contains(t, p.workspace.body, "New objective")
}

func TestContractListEditorPreservesAcceptanceItems(t *testing.T) {
	p := workflowPage(t)
	p.editContract("explain")
	p.workspace.focus = 1
	p.contractKey(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, p.workspace.itemEditor)
	p.workspace.itemEditor.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	p.contractKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Explain A; preserve B")})
	p.contractKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	require.Nil(t, p.workspace.itemEditor)
	require.Equal(t, []string{"Explain A; preserve B"}, p.contractItems(1))
	require.Equal(t, "contract", p.workspace.kind)
}

func TestReopenedContractKeepsItemSeparators(t *testing.T) {
	p := workflowPage(t)
	_, err := p.app.Workflows.SetContract(context.Background(), "s", &workflow.Contract{Kind: "explain", Acceptance: []string{"Explain A; preserve B"}, Targets: []string{"file,with,commas.go"}})
	require.NoError(t, err)
	p.refreshWorkflow()
	p.editContract("explain")
	require.Equal(t, []string{"Explain A; preserve B"}, p.contractItems(1))
	require.Equal(t, []string{"file,with,commas.go"}, p.contractItems(0))
}

func TestWorkspaceMouseOnlyChoosesVisibleActions(t *testing.T) {
	p := workflowPage(t)
	p.width = 80
	p.height = 24
	p.showWorkspace("parent", "Parent", "")
	p.showWorkspace("task", "Task", "body")
	p.workspaceHistory = []workspacePanel{{title: "Parent"}}
	for range 8 {
		p.workspace.actions = append(p.workspace.actions, workspaceAction{kind: "unused", label: "Action"})
	}
	first, rows, _ := p.workspaceActionGeometry()
	require.Equal(t, 3, rows)
	p.workspaceMouse(tea.MouseMsg{Y: first + rows, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	require.True(t, p.workspace.allActions)
	require.Zero(t, p.workspace.selected)
	p.workspace.allActions = false
	p.workspaceMouse(tea.MouseMsg{Y: first + rows + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	require.Zero(t, p.workspace.selected)
	p.editContract("explain")
	p.workspace.focus = 1
	p.contractKey(tea.KeyMsg{Type: tea.KeyEnter})
	p.workspaceMouse(tea.MouseMsg{Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	require.Equal(t, 1, p.workspace.focus)
}
