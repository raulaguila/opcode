package page

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/raulaguila/opcode/internal/workflow"
)

type WorkflowCommandMsg struct{ Command, Value string }
type workflowPanel struct {
	task       workflow.Task
	expanded   bool
	question   interaction.Question
	answer     textinput.Model
	choice     int
	offset     int
	details    bool
	detailView viewport.Model
}

func (p *chatPage) refreshWorkflow() {
	if p.app.Workflows != nil && p.session.ID != "" {
		if task, err := p.app.Workflows.Get(context.Background(), p.session.ID); err == nil {
			if len(p.workflow.task.Steps) == 0 && len(task.Steps) > 0 {
				p.workflow.expanded = true
				p.workflow.offset = -1
			}
			p.workflow.task = task
		}
	}
	if p.app.Questions == nil {
		return
	}
	q, ok := p.app.Questions.Pending(p.session.ID)
	if !ok {
		p.workflow.question = interaction.Question{}
		return
	}
	if q.ID != p.workflow.question.ID {
		input := textinput.New()
		input.CharLimit = 4000
		input.Placeholder = "Escreva sua resposta ou escolha com ↑/↓"
		input.Focus()
		p.workflow.details = false
		p.workflow.detailView = viewport.New(max(1, p.width-2), max(1, p.height-8))
		p.workflow.answer = input
		p.workflow.choice = -1
	}
	p.workflow.question = q
}
func (p *chatPage) mode() workflow.Mode {
	if p.workflow.task.Mode.Valid() {
		return p.workflow.task.Mode
	}
	if cfg := config.Get(); cfg != nil {
		return workflow.Mode(cfg.Runtime.Defaults().Mode)
	}
	return workflow.Execute
}
func modeLabel(mode workflow.Mode) string {
	switch mode {
	case workflow.Ask:
		return "Perguntar"
	case workflow.Plan:
		return "Planejar"
	default:
		return "Executar"
	}
}
func (p *chatPage) planRows() []string {
	task := p.workflow.task
	rows := []string{" Sessão: " + p.session.ID}
	if task.Objective != "" {
		rows = append(rows, " "+panelText(task.Objective))
	}
	for i, step := range task.Steps {
		mark := "○"
		if step.Status == "completed" {
			mark = "✓"
		}
		if step.Status == "in_progress" {
			mark = "→"
		}
		rows = append(rows, fmt.Sprintf(" %s %d. %s", mark, i+1, panelText(step.Title)))
	}
	for _, criterion := range task.Criteria {
		rows = append(rows, " Critério: "+panelText(criterion))
	}
	if len(task.Checks) > 0 {
		rows = append(rows, " Verificações: "+strings.Join(task.Checks, ", "))
	}
	if task.Summary != "" {
		rows = append(rows, " "+panelText(task.Summary))
	}
	rows = append(rows, " Alt+↑/↓ rola · /plan recolhe · /resume continua")
	return rows
}
func (p *chatPage) planOffset() int {
	limit := max(1, min(8, p.height/3))
	offset := p.workflow.offset
	if offset < 0 {
		offset = 0
		for i, step := range p.workflow.task.Steps {
			if step.Status == "in_progress" {
				offset = max(0, i+1)
				break
			}
		}
	}
	return min(offset, max(0, len(p.planRows())-limit))
}
func (p *chatPage) workflowHeight() int {
	if p.app == nil || p.app.Workflows == nil || p.width < 20 || p.height < 8 {
		return 0
	}
	h := 1
	if p.workflow.question.ID != "" {
		if p.workflow.details {
			return min(max(3, p.height-5), 18)
		}
		h = 4 + len(p.workflow.question.Choices)
		if p.workflow.question.Explanation != "" {
			h++
		}
	} else if p.workflow.expanded {
		h += min(len(p.planRows()), max(1, min(8, p.height/3)))
	}
	return min(h, max(1, p.height-5))
}
func (p *chatPage) workflowView() string {
	height := p.workflowHeight()
	if height == 0 {
		return ""
	}
	w := max(1, p.width-2)
	task := p.workflow.task
	line := fmt.Sprintf(" %s · /mode ask|plan|execute · /plan", modeLabel(p.mode()))
	if len(task.Steps) > 0 {
		done := 0
		for _, step := range task.Steps {
			if step.Status == "completed" {
				done++
			}
		}
		line = fmt.Sprintf(" %s · Plano %d/%d · %s · /plan", modeLabel(p.mode()), done, len(task.Steps), task.Status)
	}
	lines := []string{ansi.Truncate(line+p.queueSummary(), w, "…")}
	if p.workflow.expanded && p.workflow.question.ID == "" {
		rows := p.planRows()
		offset := p.planOffset()
		limit := min(len(rows)-offset, max(1, min(8, p.height/3)))
		for _, row := range rows[offset : offset+limit] {
			lines = append(lines, ansi.Truncate(row, w, "…"))
		}
	}

	q := p.workflow.question
	if q.ID != "" && p.workflow.details {
		rows := []string{"Pergunta: " + panelText(q.Question), panelText(q.Explanation)}
		for i, choice := range q.Choices {
			rows = append(rows, fmt.Sprintf("%d. %s", i+1, panelText(choice)))
		}
		p.workflow.detailView.Width = w
		p.workflow.detailView.Height = max(1, height-2)
		p.workflow.detailView.SetContent(ansi.Hardwrap(strings.Join(rows, "\n\n"), w, true))
		return strings.Join([]string{lines[0], p.workflow.detailView.View(), " F2/Esc volta · ↑/↓/PgUp/PgDn rola"}, "\n")
	}
	if q.ID != "" {
		// Keep the decision and answer visible even when the plan is expanded.
		lines = lines[:1]
		lines = append(lines, ansi.Truncate(" Decisão: "+panelText(q.Question), w, "…"))
		if q.Explanation != "" {
			lines = append(lines, ansi.Truncate(" "+panelText(q.Explanation), w, "…"))
		}
		for i, choice := range q.Choices {
			mark := " "
			if p.workflow.choice == i {
				mark = "›"
			}
			lines = append(lines, ansi.Truncate(fmt.Sprintf(" %s %d. %s", mark, i+1, panelText(choice)), w, "…"))
		}
		p.workflow.answer.Width = max(1, w-4)
		lines = append(lines, " "+p.workflow.answer.View(), " ↑/↓ escolhe · digitar responde · Enter envia · F2 detalhes · Esc cancela")
	}
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	t := theme.CurrentTheme()
	return styles.BaseStyle().Foreground(t.Text()).Width(max(0, p.width)).Render(strings.Join(lines, "\n"))
}
func (p *chatPage) questionKey(msg tea.KeyMsg) tea.Cmd {
	q := p.workflow.question
	if msg.String() == "f2" {
		p.workflow.details = !p.workflow.details
		return p.resizeContent()
	}
	if p.workflow.details {
		if msg.String() == "esc" || msg.String() == "enter" {
			p.workflow.details = false
			return p.resizeContent()
		}
		var cmd tea.Cmd
		p.workflow.detailView, cmd = p.workflow.detailView.Update(msg)
		return cmd
	}
	switch msg.String() {
	case "esc":
		p.app.CoderAgent.Cancel(p.session.ID)
		return nil
	case "up", "down":
		if len(q.Choices) > 0 {
			if msg.String() == "down" {
				p.workflow.choice = (p.workflow.choice + 1) % len(q.Choices)
			} else {
				if p.workflow.choice < 0 {
					p.workflow.choice = 0
				}
				p.workflow.choice = (p.workflow.choice + len(q.Choices) - 1) % len(q.Choices)
			}
			p.workflow.answer.SetValue("")
		}
		return nil
	case "enter":
		answer := p.workflow.answer.Value()
		if strings.TrimSpace(answer) == "" && p.workflow.choice >= 0 {
			answer = q.Choices[p.workflow.choice]
		}
		if err := p.app.Questions.Answer(p.session.ID, q.ID, answer); err != nil {
			return util.ReportError(err)
		}
		p.workflow.question = interaction.Question{}
		return p.resizeContent()
	default:
		p.workflow.choice = -1
		var cmd tea.Cmd
		p.workflow.answer, cmd = p.workflow.answer.Update(msg)
		return cmd
	}
}

const resumeInstruction = "Continue a tarefa e o plano salvos, confira os arquivos atuais e respeite as aprovações."

func (p *chatPage) workflowCommand(command, value string) tea.Cmd {
	cmd, err := p.applyWorkflowCommand(command, value)
	if err != nil {
		return util.ReportError(err)
	}
	return cmd
}
func (p *chatPage) applyWorkflowCommand(command, value string) (tea.Cmd, error) {
	if p.app.Workflows == nil {
		return nil, fmt.Errorf("task storage unavailable")
	}
	ctx := context.Background()
	switch command {
	case "mode":
		if value == "" {
			return util.ReportInfo("Modo: " + modeLabel(p.mode()) + " · /mode ask|plan|execute"), nil
		}
		if err := p.app.CoderAgent.SetMode(ctx, p.session.ID, workflow.Mode(value)); err != nil {
			return nil, err
		}
		p.resetTerminal()
		p.refreshWorkflow()
		return tea.Batch(p.resizeContent(), util.ReportInfo("Modo: "+modeLabel(p.mode()))), nil
	case "plan", "task":
		if value == "new" {
			if err := p.app.CoderAgent.SetMode(ctx, p.session.ID, p.mode()); err != nil {
				return nil, err
			}
			if _, err := p.app.Workflows.Reset(ctx, p.session.ID); err != nil {
				return nil, err
			}
		}
		p.workflow.expanded = !p.workflow.expanded
		if p.workflow.expanded {
			p.workflow.offset = -1
		}
		p.refreshWorkflow()
		return p.resizeContent(), nil
	case "resume":
		return p.submitAgentMessage(resumeInstruction, nil, resumeInstruction), nil
	}
	return nil, nil
}

func (p *chatPage) handleWorkflowMessage(text string) (tea.Cmd, bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil, false
	}
	command := strings.TrimPrefix(fields[0], "/")
	if fields[0] == command || (command != "mode" && command != "plan" && command != "task" && command != "resume") {
		return nil, false
	}
	value := strings.Join(fields[1:], " ")
	if command == "resume" && p.app.Workflows != nil {
		return p.submitAgentMessage(resumeInstruction, nil, text), true
	}
	cmd, err := p.applyWorkflowCommand(command, value)
	if err != nil {
		return tea.Batch(util.ReportError(err), util.CmdHandler(chat.SubmissionResultMsg{Text: text, Error: err})), true
	}
	return tea.Batch(cmd, util.CmdHandler(chat.SubmissionResultMsg{Text: text})), true
}

func panelText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(tools.RedactCredentials(value)))
}
