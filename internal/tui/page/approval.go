package page

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/raulaguila/opcode/internal/workflow"
)

type ApprovalPolicyMsg struct{ Policy permission.ApprovalPolicy }
type OpenApprovalChooserMsg struct{}

func (p *chatPage) approvalPolicy() permission.ApprovalPolicy {
	if p.pendingApproval != "" {
		return p.pendingApproval
	}
	if p.app != nil {
		if policies, ok := p.app.Permissions.(permission.ApprovalPolicies); ok {
			return policies.ApprovalPolicy(p.session.ID)
		}
	}
	return permission.ApprovalPrompt
}

func (p *chatPage) chooseApprovalPolicy(policy permission.ApprovalPolicy) tea.Cmd {
	if policy != permission.ApprovalPrompt && policy != permission.ApprovalProjectEdits {
		return util.ReportError(fmt.Errorf("invalid approval policy"))
	}
	if p.app.CoderAgent != nil && p.app.CoderAgent.IsBusy() {
		return util.ReportWarn(config.Text("Aguarde ou cancele a execução antes de alterar as aprovações.", "Wait or cancel the current run before changing approvals."))
	}
	if _, ok := p.app.Permissions.(permission.ApprovalPolicies); !ok {
		return util.ReportError(fmt.Errorf("approval policies unavailable"))
	}
	p.pendingApproval = policy
	if err := p.applyPendingApproval(); err != nil {
		p.pendingApproval = ""
		return util.ReportError(err)
	}
	if p.mode() != workflow.Execute {
		return util.ReportInfo(config.Text("Somente leitura neste modo; edições só em Executar.", "Read only in this mode; edits only apply in Execute."))
	}
	return nil
}

func (p *chatPage) applyPendingApproval() error {
	if p.pendingApproval == "" || p.session.ID == "" {
		return nil
	}
	policies, ok := p.app.Permissions.(permission.ApprovalPolicies)
	if !ok {
		return fmt.Errorf("approval policies unavailable")
	}
	root := ""
	if cfg := config.Get(); cfg != nil {
		root = cfg.WorkingDir
	}
	if err := policies.SetApprovalPolicy(p.session.ID, p.pendingApproval, root); err != nil {
		return err
	}
	p.pendingApproval = ""
	return nil
}

func (p *chatPage) resetApprovalPolicy(sessionID string) {
	p.pendingApproval = ""
	if p.app != nil && sessionID != "" {
		if policies, ok := p.app.Permissions.(permission.ApprovalPolicies); ok {
			_ = policies.SetApprovalPolicy(sessionID, permission.ApprovalPrompt, "")
		}
	}
}

func approvalLabel(policy permission.ApprovalPolicy) string {
	if policy == permission.ApprovalProjectEdits {
		return config.Text("Permitir edições no projeto", "Allow project edits")
	}
	return config.Text("Pedir aprovação", "Ask for approval")
}
