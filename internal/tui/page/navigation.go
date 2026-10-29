package page

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/raulaguila/opcode/internal/workflow"
)

func (p *chatPage) backWorkspace() {
	if len(p.workspaceHistory) == 0 {
		p.closeWorkspace()
		return
	}
	p.resetWorkspace()
	seq := p.workspace.seq
	last := len(p.workspaceHistory) - 1
	p.workspace = p.workspaceHistory[last]
	p.workspaceHistory = p.workspaceHistory[:last]
	p.workspace.seq = seq // Reject results belonging to the previous visit.
	p.refreshTaskPanel()
}
func (p *chatPage) refreshTaskPanel() {
	if p.workspace.kind != "task" {
		return
	}
	var selected workspaceAction
	if p.workspace.selected >= 0 && p.workspace.selected < len(p.workspace.actions) {
		selected = p.workspace.actions[p.workspace.selected]
	}
	p.workspace.actions = p.taskActions()
	p.workspace.selected = min(p.workspace.selected, max(0, len(p.workspace.actions)-1))
	for i, action := range p.workspace.actions {
		if action.kind == selected.kind && action.text == selected.text {
			p.workspace.selected = i
			break
		}
	}
	body := safePanel(p.taskBody())
	if body == p.workspace.body {
		return
	}
	offset := p.workspace.view.YOffset
	p.workspace.body = body
	p.workspace.view.SetContent(body)
	p.workspace.view.SetYOffset(offset)
}
func (p *chatPage) planAction(action string) tea.Cmd {
	if p.app == nil || p.app.CoderAgent == nil {
		return nil
	}
	if p.app.CoderAgent.IsSessionBusy(p.session.ID) {
		return util.ReportWarn(config.Text("Aguarde ou cancele a atividade antes de alterar o plano.", "Wait or cancel the activity before changing the plan."))
	}
	switch action {
	case "adjust-plan":
		// Preserve an existing draft instead of overwriting it with a template.
		draft, ok := p.composer.(chat.DraftControl)
		if !ok || p.editor == nil {
			return nil
		}
		text, attachments := draft.Draft()
		if text != "" || len(attachments) > 0 {
			return util.ReportWarn(config.Text("Envie ou guarde o rascunho antes de preparar o ajuste.", "Send or save the draft before preparing the adjustment."))
		}
		if err := p.app.CoderAgent.SetMode(context.Background(), p.session.ID, workflow.Plan); err != nil {
			return util.ReportError(err)
		}
		p.closeWorkspace()
		p.refreshWorkflow()
		_, cmd := p.editor.Update(chat.RestoreDraftMsg{Text: config.Text("Ajuste o plano atual: ", "Adjust the current plan: ")})
		return cmd
	case "execute-plan":
		p.showWorkspace("plan-confirm", config.Text("Executar plano com revisão", "Execute plan with review"), config.Text("Ao confirmar, o modo muda para Executar e o plano atual é enviado para continuação. Comandos e alterações continuam exigindo as permissões normais.", "Confirming switches to Execute and submits the current plan for continuation. Commands and edits still require normal permissions."))
		p.workspace.actions = []workspaceAction{{kind: "confirm-plan", label: config.Text("Confirmar e continuar em Executar", "Confirm and continue in Execute")}}
	case "confirm-plan":
		if err := p.app.CoderAgent.SetMode(context.Background(), p.session.ID, workflow.Execute); err != nil {
			return util.ReportError(err)
		}
		p.refreshWorkflow()
		p.closeWorkspace()
		return p.submitAgentMessage(resumeInstruction, nil, "", true)
	}
	return nil
}
