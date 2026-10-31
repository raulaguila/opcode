package page

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/raulaguila/opcode/internal/workflow"
)

func (p *chatPage) openContract() tea.Cmd {
	if p.app.Workflows == nil {
		return util.ReportWarn("Task storage unavailable")
	}
	p.showWorkspace("contract-kinds", config.Text("Resultado esperado", "Expected outcome"), config.Text("Escolha o resultado. Alvos e critérios orientam o agente; não concedem permissões. O contrato permanece na tarefa até ser removido.", "Choose an outcome. Targets and criteria guide the agent; they never grant permissions. The contract stays with this task until cleared."))
	for _, choice := range []struct{ kind, pt, en string }{{"explain", "Explicar · somente leitura", "Explain · read only"}, {"plan", "Planejar · salvar etapas e critérios", "Plan · save steps and criteria"}, {"change", "Alterar · exige mudança ou justificativa explícita", "Change · requires changes or explicit justification"}, {"verify", "Verificar · exige verificações configuradas", "Verify · requires configured checks"}, {"clear", "Remover contrato · identificar pelo texto", "Clear contract · infer from message"}} {
		p.workspace.actions = append(p.workspace.actions, workspaceAction{kind: "contract-kind", text: choice.kind, label: config.Text(choice.pt, choice.en)})
	}
	return nil
}
func (p *chatPage) editContract(kind string) tea.Cmd {
	if p.app.CoderAgent != nil && p.app.CoderAgent.IsBusy() {
		return util.ReportWarn(config.Text("Aguarde ou cancele a execução antes de alterar a tarefa.", "Wait or cancel the current run before changing the task."))
	}
	var created tea.Cmd
	if p.session.ID == "" {
		s, err := p.app.Sessions.Create(context.Background(), config.Text("Nova tarefa", "New task"))
		if err != nil {
			return util.ReportError(err)
		}
		p.session = s
		created = util.CmdHandler(chat.SessionSelectedMsg(s))
		p.refreshWorkflow()
	}
	if kind == "clear" {
		_, err := p.app.Workflows.SetContract(context.Background(), p.session.ID, nil)
		if err != nil {
			return util.ReportError(err)
		}
		p.refreshWorkflow()
		p.closeWorkspace()
		return tea.Batch(created, p.resizeContent())
	}
	current := workflow.Contract{}
	if p.workflow.task.Contract != nil {
		current = *p.workflow.task.Contract
	}
	p.showWorkspace("contract", config.Text("Configurar tarefa: ", "Configure task: ")+kind, "")
	p.workspace.contractKind = kind
	for _, value := range []string{strings.Join(current.Targets, ", "), strings.Join(current.Acceptance, "; "), strings.Join(current.Checks, ", ")} {
		f := textinput.New()
		f.CharLimit = 4000
		f.SetWidth(max(8, p.width-8))
		f.SetValue(value)
		p.workspace.fields = append(p.workspace.fields, f)
	}
	p.workspace.itemValues = map[int][]string{0: append([]string(nil), current.Targets...), 1: append([]string(nil), current.Acceptance...), 2: append([]string(nil), current.Checks...)}
	p.workspace.itemText = map[int]string{}
	for i, f := range p.workspace.fields {
		p.workspace.itemText[i] = f.Value()
	}
	p.workspace.fields[0].Focus()
	return tea.Batch(created, textinput.Blink)
}
func splitContractField(s, separator string) []string {
	var result []string
	for v := range strings.SplitSeq(s, separator) {
		if v = strings.TrimSpace(v); v != "" {
			result = append(result, v)
		}
	}
	return result
}
func (p *chatPage) contractKey(msg tea.KeyPressMsg) tea.Cmd {
	if result0, handled := contractKeyWorkspaceItemEditor(p, &msg); handled {
		return result0
	}

	switch msg.String() {
	case "enter":
		i := p.workspace.focus
		p.workspace.itemEditor = dialog.NewItemEditor(p.contractItems(i))
		return textinput.Blink
	case "esc":
		p.backWorkspace()
		return nil
	case "tab", "down", "shift+tab", "up":
		p.workspace.fields[p.workspace.focus].Blur()
		step := 1
		if msg.String() == "up" || msg.String() == "shift+tab" {
			step = -1
		}
		p.workspace.focus = (p.workspace.focus + step + len(p.workspace.fields)) % len(p.workspace.fields)
		return p.workspace.fields[p.workspace.focus].Focus()
	case "ctrl+s":
		return contractKeyChatPageCtrlS(p)
	}
	var cmd tea.Cmd
	p.workspace.fields[p.workspace.focus], cmd = p.workspace.fields[p.workspace.focus].Update(msg)
	return cmd
}
func (p *chatPage) contractView() string {
	if p.workspace.itemEditor != nil {
		return p.workspace.itemEditor.View(max(1, p.width-8), p.height)
	}
	labels := []string{config.Text("Arquivos relativos (vírgula, opcional)", "Relative files (comma separated, optional)"), config.Text("Critérios de aceite (ponto e vírgula)", "Acceptance criteria (semicolon separated)"), config.Text("Verificações configuradas (vírgula)", "Configured checks (comma separated)")}
	lines := []string{styles.PanelTitle().Render(p.workspace.title), config.Text("Não amplia o modo atual nem dispensa aprovações.", "Does not widen the current mode or waive approvals.")}
	count := max(1, min(len(p.workspace.fields), (p.height-8)/2))
	start := max(0, min(p.workspace.focus-count+1, len(p.workspace.fields)-count))
	for i := start; i < start+count; i++ {
		f := p.workspace.fields[i]
		f.SetWidth(max(1, p.width-6))
		style := styles.BaseStyle().Background(theme.CurrentTheme().BackgroundSecondary())
		inputStyles := f.Styles()
		inputStyles.Focused.Text, inputStyles.Focused.Prompt = style, style
		inputStyles.Blurred.Text, inputStyles.Blurred.Prompt = style, style
		f.SetStyles(inputStyles)
		label := labels[i]
		if i == p.workspace.focus {
			label = "› " + label
		}
		lines = append(lines, label, f.View())
	}
	if p.workspace.note != "" {
		lines = append(lines, panelText(p.workspace.note))
	}
	mode := fmt.Sprintf(config.Text("Modo atual: %s · ", "Current mode: %s · "), modeLabel(p.mode()))
	legend := styles.PanelHint().Render(config.Text("Enter edita itens · Tab campo · Ctrl+S salva · Esc volta", "Enter edit items · Tab field · Ctrl+S save · Esc back"))
	lines = append(lines, styles.PanelDivider(max(1, p.width-2)), mode+legend)
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(1, p.width-2), "…")
	}
	return strings.Join(lines, "\n")
}

func contractLabel(kind string) string {
	switch kind {
	case "explain":
		return config.Text("Explicar", "Explain")
	case "plan":
		return config.Text("Planejar", "Plan")
	case "change":
		return config.Text("Alterar", "Change")
	case "verify":
		return config.Text("Verificar", "Verify")
	}
	return kind
}

func (p *chatPage) contractItems(i int) []string {
	if text, ok := p.workspace.itemText[i]; ok && text == p.workspace.fields[i].Value() {
		return p.workspace.itemValues[i]
	}
	separator := ","
	if i == 1 {
		separator = ";"
	}
	return splitContractField(p.workspace.fields[i].Value(), separator)
}

func contractKeyChatPageCtrlS(p *chatPage) (extractedResult0 tea.Cmd) {
	if p.app.CoderAgent != nil && p.app.CoderAgent.IsBusy() {
		return util.ReportWarn(config.Text("Aguarde a execução.", "Wait for the current run."))
	}
	c := workflow.Contract{Kind: p.workspace.contractKind, Targets: p.contractItems(0), Acceptance: p.contractItems(1), Checks: p.contractItems(2)}
	if err := c.Validate(); err != nil {
		p.workspace.note = err.Error()
		return nil
	}
	for _, check := range c.Checks {
		if config.Get().Checks[check].Command == "" {
			p.workspace.note = config.Text("Verificação não configurada: ", "Check not configured: ") + panelText(check)
			return nil
		}
	}
	if err := p.app.CoderAgent.SetMode(context.Background(), p.session.ID, p.workflow.task.Mode); err != nil {
		return util.ReportError(err)
	}
	if _, err := p.app.Workflows.SetContract(context.Background(), p.session.ID, &c); err != nil {
		return util.ReportError(err)
	}
	p.refreshWorkflow()
	p.closeWorkspace()
	return tea.Batch(p.resizeContent(), util.ReportInfo(config.Text("Contrato salvo. Use /mode para escolher o nível de execução.", "Contract saved. Use /mode to choose execution permissions.")))

}

func contractKeyWorkspaceItemEditor(p *chatPage, msg *tea.KeyPressMsg) (extractedResult0 tea.Cmd, handled bool) {
	if e := p.workspace.itemEditor; e != nil {
		done, save, cmd := e.Update((*msg))
		if done {
			if save {
				if p.workspace.itemValues == nil {
					p.workspace.itemValues = map[int][]string{}
					p.workspace.itemText = map[int]string{}
				}
				i := p.workspace.focus
				separator := ","
				if i == 1 {
					separator = ";"
				}
				p.workspace.itemValues[i] = e.Items()
				p.workspace.itemText[i] = strings.Join(e.Items(), separator+" ")
				p.workspace.fields[i].SetValue(p.workspace.itemText[i])
			}
			p.workspace.itemEditor = nil
		}
		return cmd, true
	}
	return extractedResult0, false
}
