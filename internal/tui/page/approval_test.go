package page

import (
	"context"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

type approvalSubmitProbe struct {
	*workflowProbe
	policies permission.ApprovalPolicies
	seen     permission.ApprovalPolicy
}

func approvalPage(t *testing.T) *chatPage {
	t.Helper()
	ensureSetupTestConfig(t)
	previous := config.Get().WorkingDir
	previousData := config.Get().Data.Directory
	config.Get().WorkingDir = t.TempDir()
	config.Get().Data.Directory = t.TempDir()
	t.Cleanup(func() {
		config.Get().WorkingDir = previous
		config.Get().Data.Directory = previousData
	})
	p := workflowPage(t)
	p.app.Permissions = permission.NewPermissionService()
	return p
}

func (a *approvalSubmitProbe) Submit(_ context.Context, id, _ string, _ ...message.Attachment) (<-chan agent.AgentEvent, bool, error) {
	a.seen = a.policies.ApprovalPolicy(id)
	return nil, false, nil
}

func TestApprovalSelectionBeforeSessionAppliesBeforeSubmissionAndKeepsDraft(t *testing.T) {
	p := approvalPage(t)
	p.session = session.Session{}
	p.editor.Update(chat.RestoreDraftMsg{Text: "KEEP_DRAFT"})
	flattenCommands(p.chooseApprovalPolicy(permission.ApprovalProjectEdits))
	require.Empty(t, p.session.ID, "choosing a policy does not create a conversation")
	require.Equal(t, permission.ApprovalProjectEdits, p.approvalPolicy())
	draft, _ := p.composer.(chat.DraftControl).Draft()
	require.Equal(t, "KEEP_DRAFT", draft)
	p.session = session.Session{ID: "created-on-first-message"}
	probe := &approvalSubmitProbe{workflowProbe: p.app.CoderAgent.(*workflowProbe), policies: p.app.Permissions.(permission.ApprovalPolicies)}
	p.app.CoderAgent = probe
	flattenCommands(p.submitAgentMessage("create file", nil, "create file"))
	require.Equal(t, permission.ApprovalProjectEdits, probe.seen)
	require.Empty(t, p.pendingApproval)
}

func TestApprovalBusyChangeDoesNotIncreasePermissionsOrAlterMode(t *testing.T) {
	p := approvalPage(t)
	p.app.CoderAgent.(*workflowProbe).busy = true
	msgs := flattenCommands(p.chooseApprovalPolicy(permission.ApprovalProjectEdits))
	require.Equal(t, util.InfoTypeWarn, msgs[0].(util.InfoMsg).Type)
	require.Equal(t, permission.ApprovalPrompt, p.approvalPolicy())
	require.Equal(t, workflow.Execute, p.mode())
}

func TestApprovalResetsWhenSwitchingConversationAndRevoking(t *testing.T) {
	p := approvalPage(t)
	flattenCommands(p.chooseApprovalPolicy(permission.ApprovalProjectEdits))
	require.Equal(t, permission.ApprovalProjectEdits, p.approvalPolicy())
	policies := p.app.Permissions.(permission.ApprovalPolicies)
	// Rootless reset remains valid even when no configuration has been loaded.
	p.resetApprovalPolicy("s")
	require.Equal(t, permission.ApprovalPrompt, policies.ApprovalPolicy("s"))
	flattenCommands(p.chooseApprovalPolicy(permission.ApprovalProjectEdits))
	p.Update(chat.SessionSelectedMsg(session.Session{ID: "reopened"}))
	require.Equal(t, permission.ApprovalPrompt, p.approvalPolicy())
	require.Equal(t, permission.ApprovalPrompt, policies.ApprovalPolicy("s"))
	flattenCommands(p.chooseApprovalPolicy(permission.ApprovalProjectEdits))
	p.openPermissions()
	_, handled := workspaceKeyChatPageR(p)
	require.True(t, handled)
	require.Equal(t, permission.ApprovalPrompt, p.approvalPolicy())
}

func TestPendingApprovalResetsForNewConversationAndModeChange(t *testing.T) {
	p := approvalPage(t)
	p.session = session.Session{}
	flattenCommands(p.chooseApprovalPolicy(permission.ApprovalProjectEdits))
	require.Equal(t, permission.ApprovalProjectEdits, p.approvalPolicy())
	flattenCommands(p.newConversation())
	require.Equal(t, permission.ApprovalPrompt, p.approvalPolicy())
	flattenCommands(p.chooseApprovalPolicy(permission.ApprovalProjectEdits))
	p.session = session.Session{ID: "created-by-mode-command"}
	_, err := p.applyWorkflowCommand("mode", "ask")
	require.NoError(t, err)
	require.Empty(t, p.pendingApproval)
	require.Equal(t, permission.ApprovalPrompt, p.approvalPolicy())
	require.Equal(t, workflow.Ask, p.mode())
}

func TestReopenedPageDoesNotRestoreSessionApprovalPolicy(t *testing.T) {
	p := approvalPage(t)
	flattenCommands(p.chooseApprovalPolicy(permission.ApprovalProjectEdits))
	reopened := NewChatPage(p.app).(*chatPage)
	reopened.Update(chat.SessionSelectedMsg(p.session))
	require.Equal(t, permission.ApprovalPrompt, reopened.approvalPolicy())
	require.Equal(t, permission.ApprovalPrompt, p.approvalPolicy())
}

func TestApprovalSelectionExplainsReadOnlyModesWithoutRepeatingExecuteState(t *testing.T) {
	for _, mode := range []workflow.Mode{workflow.Ask, workflow.Plan, workflow.Execute} {
		p := approvalPage(t)
		p.workflow.task.Mode = mode
		cmd := p.chooseApprovalPolicy(permission.ApprovalProjectEdits)
		require.Equal(t, permission.ApprovalProjectEdits, p.approvalPolicy())
		require.Equal(t, mode, p.mode())
		if mode == workflow.Execute {
			require.Nil(t, cmd, "the selected footer value already confirms ordinary execute changes")
		} else {
			msgs := flattenCommands(cmd)
			require.Len(t, msgs, 1)
			require.Equal(t, config.Text("Somente leitura neste modo; edições só em Executar.", "Read only in this mode; edits only apply in Execute."), msgs[0].(util.InfoMsg).Msg)
		}
	}
}

func TestApprovalCommandOpensChooserWithoutChangingConsent(t *testing.T) {
	p := approvalPage(t)
	cmd, handled := p.handleWorkspaceMessage("/approval", nil)
	require.True(t, handled, "a UI command must not be forwarded to the model")
	msgs := flattenCommands(cmd)
	require.Len(t, msgs, 2)
	require.IsType(t, OpenApprovalChooserMsg{}, msgs[0])
	require.Equal(t, chat.SubmissionResultMsg{Text: "/approval"}, msgs[1])
	require.Equal(t, permission.ApprovalPrompt, p.approvalPolicy())
	require.Equal(t, workflow.Execute, p.mode())
}
