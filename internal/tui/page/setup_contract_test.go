package page

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

type contractProbe struct{ *workflowProbe }

func (p *contractProbe) IsBusy() bool      { return p.busy }
func (*contractProbe) Model() models.Model { return models.Model{ID: "test", APIModel: "test-model"} }
func TestContractFormPreservesDraftDoesNotRaiseModeAndRejectsBusyEdits(t *testing.T) {
	p := workflowPage(t)
	probe := &contractProbe{p.app.CoderAgent.(*workflowProbe)}
	p.app.CoderAgent = probe
	_, err := probe.store.SetMode(context.Background(), "s", workflow.Ask)
	require.NoError(t, err)
	p.refreshWorkflow()
	p.editor.Update(chat.RestoreDraftMsg{Text: "draft intact"})
	p.openContract()
	p.workspace.selected = 2
	p.workspaceChoose()
	require.Equal(t, "contract", p.workspace.kind)
	p.workspace.fields[0].SetValue("main.go")
	p.workspace.fields[1].SetValue("Expected change; Preserve behavior")
	flattenCommands(p.workspaceKey(tea.KeyMsg{Type: tea.KeyCtrlS}))
	saved, err := probe.store.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, workflow.Ask, saved.Mode)
	require.Equal(t, "change", saved.Contract.Kind)
	require.Equal(t, []string{"main.go"}, saved.Contract.Targets)
	draft, _ := p.composer.(chat.DraftControl).Draft()
	require.Equal(t, "draft intact", draft)
	probe.busy = true
	flattenCommands(p.editContract("verify"))
	require.NotEqual(t, "contract", p.workspace.kind)
	saved, _ = probe.store.Get(context.Background(), "s")
	require.Equal(t, "change", saved.Contract.Kind)
}
func TestSetupIsOptionalRequiresConfirmationAndEscapePreservesDraft(t *testing.T) {
	ensureSetupTestConfig(t)
	p := workflowPage(t)
	p.app.CoderAgent = &contractProbe{p.app.CoderAgent.(*workflowProbe)}
	p.editor.Update(chat.RestoreDraftMsg{Text: "draft"})
	require.Nil(t, p.openSetup())
	require.Equal(t, "setup", p.workspace.kind)
	require.Len(t, p.workspace.actions, 2)
	require.Equal(t, "setup-done", p.workspace.actions[0].kind)
	require.Nil(t, p.setupChoose("setup-details"))
	for i, item := range p.workspace.actions {
		if item.kind == "setup-probe" {
			p.workspace.selected = i
		}
	}
	require.Nil(t, p.workspaceChoose())
	require.Equal(t, "setup-probe-confirm", p.workspace.kind)
	require.False(t, p.workspace.loading)
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.False(t, p.WorkspaceFocused())
	draft, _ := p.composer.(chat.DraftControl).Draft()
	require.Equal(t, "draft", draft)
}
func TestGitWorkspaceIgnoresStaleResultsAndSummarizesCurrentBranch(t *testing.T) {
	ensureSetupTestConfig(t)
	p := workflowPage(t)
	p.showWorkspace("worktrees", "Git", "")
	seq := p.workspace.seq
	p.gitWorkspaceResult(GitWorkspaceMsg{Session: "other", Sequence: seq, Trees: []tools.Worktree{{Path: "/other"}}})
	require.Empty(t, p.workspace.actions)
	p.gitWorkspaceResult(GitWorkspaceMsg{Session: "s", Sequence: seq, Trees: []tools.Worktree{{Path: config.WorkingDirectory(), Branch: "refs/heads/main"}, {Path: "/another", Detached: true}}, Status: `{"entries":[{"status":" M","path":"main.go"}]}`})
	require.Contains(t, p.workspace.body, "main")
	require.Contains(t, p.workspace.body, "main.go")
	require.Equal(t, "worktree-open", p.workspace.actions[0].kind)
	require.Nil(t, p.workspaceChoose())
	require.Equal(t, "worktree-open-confirm", p.workspace.kind)
	p.app.CoderAgent = &contractProbe{&workflowProbe{store: p.app.Workflows, busy: true}}
	require.Error(t, p.canOpenWorktree("/another"))
}

func ensureSetupTestConfig(t *testing.T) {
	t.Helper()
	if config.Get() != nil {
		return
	}
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
	viper.SetConfigFile(path)
	_, err := config.LoadOffline(root)
	require.NoError(t, err)
}

func TestContractFormKeepsFocusedFieldsAndDraftOnSmallTerminal(t *testing.T) {
	p := workflowPage(t)
	p.app.CoderAgent = &contractProbe{p.app.CoderAgent.(*workflowProbe)}
	p.SetSize(36, 14)
	p.editContract("change")
	p.workspace.fields[0].SetValue("a-very-long-file-name-that-scrolls.go")
	require.LessOrEqual(t, lipgloss.Width(p.workspaceView()), 36)
	require.LessOrEqual(t, lipgloss.Height(p.workspaceView()), 14)
	p.workspaceMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: 5})
	require.Equal(t, 1, p.workspace.focus)
	p.workspaceKey(tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, 2, p.workspace.focus)
	require.Contains(t, p.workspace.fields[0].Value(), "a-very-long")
}

func TestUserWorktreeCreationKeepsReadOnlyModeRestrictions(t *testing.T) {
	p := workflowPage(t)
	p.app.CoderAgent = &contractProbe{p.app.CoderAgent.(*workflowProbe)}
	_, err := p.app.Workflows.SetMode(context.Background(), "s", workflow.Ask)
	require.NoError(t, err)
	p.refreshWorkflow()
	require.NotNil(t, p.gitChoose(workspaceAction{kind: "worktree-create"}))
	require.NotEqual(t, "worktree-create-confirm", p.workspace.kind)
	require.NotNil(t, p.gitChoose(workspaceAction{kind: "worktree-create-confirm", text: "blocked"}))
	require.NotEqual(t, "worktree-create-result", p.workspace.kind)
}
