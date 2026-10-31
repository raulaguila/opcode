package page

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
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
type OpenModeChooserMsg struct{}
type workflowPanel struct {
	task            workflow.Task
	lastRead        time.Time
	expanded        bool
	question        interaction.Question
	answer          textinput.Model
	choice          int
	offset          int
	details         bool
	detailView      viewport.Model
	questionIndex   int
	batchAnswers    []interaction.AnswerItem
	selectedChoices map[int]bool
}

func (p *chatPage) refreshWorkflow() {
	p.refreshWorkflowTask()
	p.refreshQuestion()
}
func (p *chatPage) pollWorkflow() {
	if p.workflow.lastRead.IsZero() || (p.liveActivity() && time.Since(p.workflow.lastRead) >= time.Second) {
		p.refreshWorkflowTask()
	}
	p.refreshQuestion()
}
func (p *chatPage) refreshWorkflowTask() {
	if p.app == nil {
		return
	}
	p.workflow.lastRead = time.Now()
	if p.app.Workflows != nil && p.session.ID != "" {
		if task, err := p.app.Workflows.Get(context.Background(), p.session.ID); err == nil {
			p.workflow.task = task
		}
	}
}
func (p *chatPage) refreshQuestion() {
	if p.app == nil || p.app.Questions == nil {
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
		input.Prompt = config.Text("Resposta: ", "Answer: ")
		p.workflow.details = false
		p.workflow.detailView = viewport.New(viewport.WithWidth(max(1, p.width-2)), viewport.WithHeight(max(1, p.height-8)))
		p.workflow.answer = input
		p.workflow.choice = -1
		p.workflow.questionIndex = 0
		p.workflow.batchAnswers = make([]interaction.AnswerItem, len(q.Questions))
		p.workflow.selectedChoices = map[int]bool{}
	}
	p.workflow.question = q
	count := len(q.Choices)
	if len(q.Questions) > 0 && p.workflow.questionIndex < len(q.Questions) {
		count = len(q.Questions[p.workflow.questionIndex].Choices)
	}
	p.focusQuestionInput(count)
}
func (p *chatPage) mode() workflow.Mode {
	if p.workflow.task.Mode.Valid() {
		return p.workflow.task.EffectiveMode()
	}
	if cfg := config.Get(); cfg != nil {
		return workflow.Mode(cfg.Runtime.Defaults().Mode)
	}
	return workflow.Execute
}
func modeLabel(mode workflow.Mode) string {
	switch mode {
	case workflow.Ask:
		return config.Text("Perguntar", "Ask")
	case workflow.Plan:
		return config.Text("Planejar", "Plan")
	default:
		return config.Text("Executar", "Execute")
	}
}
func (p *chatPage) planRows() []string {
	task := p.workflow.task
	rows := []string{config.Text(" Sessão: ", " Session: ") + p.session.ID}
	planRowsTaskContract(&task, &rows)
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
		rows = append(rows, config.Text(" Critério: ", " Criterion: ")+panelText(criterion))
	}
	if len(task.Checks) > 0 {
		rows = append(rows, config.Text(" Verificações: ", " Checks: ")+strings.Join(task.Checks, ", "))
	}
	if task.Summary != "" {
		rows = append(rows, " "+panelText(task.Summary))
	}
	rows = append(rows, styles.PanelHint().Render(config.Text(" Alt+↑/↓ rola · /plan recolhe · /resume continua", " Alt+↑/↓ scroll · /plan collapse · /resume continue")))
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
	if !p.workflow.expanded && p.workflow.question.ID == "" {
		return 0
	}
	h := 1
	if p.workflow.question.ID != "" {
		return p.questionPanelHeight()
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
	if p.workflow.question.ID != "" {
		return p.questionPanelView(height)
	}
	w := max(1, p.width-2)
	task := p.workflow.task
	line := fmt.Sprintf(" %s · /mode ask|plan|execute · /plan", modeLabel(p.mode()))
	workflowViewTaskSteps(p, &task, &line)
	if task.Contract != nil {
		line = fmt.Sprintf(" %s · %s: %s · /contract · /plan", modeLabel(p.mode()), config.Text("Tarefa", "Task"), contractLabel(task.Contract.Kind))
	}
	lines := []string{ansi.Truncate(line+" · "+p.activityLabel()+p.queueSummary(), w, "…")}
	if p.workflow.expanded && p.workflow.question.ID == "" {
		rows := p.planRows()
		offset := p.planOffset()
		limit := min(len(rows)-offset, max(1, min(8, p.height/3)))
		for _, row := range rows[offset : offset+limit] {
			lines = append(lines, ansi.Truncate(row, w, "…"))
		}
	}

	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(1, p.width), "…")
	}
	t := theme.CurrentTheme()
	return styles.BaseStyle().Foreground(t.Text()).Width(max(0, p.width)).MaxHeight(height).Render(strings.Join(lines, "\n"))
}
func (p *chatPage) questionKey(msg tea.KeyPressMsg) tea.Cmd {
	q := p.workflow.question
	if len(q.Questions) > 0 {
		return p.batchQuestionKey(msg)
	}
	if msg.String() == "f2" {
		p.workflow.details = !p.workflow.details
		return p.resizeContent()
	}
	if result0, handled := questionKeyWorkflowDetails(p, &msg); handled {
		return result0
	}
	switch msg.String() {
	case "esc":
		p.app.CoderAgent.Cancel(p.session.ID)
		return nil
	case "up", "down":
		return p.navigateQuestionAnswer(msg.String(), len(q.Choices))
	case "enter":
		selected := p.selectedQuestionAnswer(interaction.QuestionItem{Choices: q.Choices})
		answer := selected.Text
		if len(selected.Choices) > 0 {
			answer = selected.Choices[0]
		}
		if err := p.app.Questions.Answer(p.session.ID, q.ID, answer); err != nil {
			return util.ReportError(err)
		}
		p.workflow.question = interaction.Question{}
		return p.resizeContent()
	default:
		return p.editQuestionAnswer(msg, len(q.Choices))
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
	case "setup-read":
		return p.setupRead()
	case "mode":
		if value == "" {
			return util.CmdHandler(OpenModeChooserMsg{}), nil
		}
		if err := p.app.CoderAgent.SetMode(ctx, p.session.ID, workflow.Mode(value)); err != nil {
			return nil, err
		}
		p.resetTerminal()
		p.refreshWorkflow()
		return tea.Batch(p.resizeContent(), util.ReportInfo(config.Text("Modo: ", "Mode: ")+modeLabel(p.mode()))), nil
	case "plan", "task":
		return applyWorkflowCommandCasePlanTask(p, command, value, ctx)
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

func questionKeyWorkflowDetails(p *chatPage, msg *tea.KeyPressMsg) (extractedResult0 tea.Cmd, handled bool) {
	if p.workflow.details {
		if msg.String() == "esc" || msg.String() == "enter" {
			p.workflow.details = false
			return p.resizeContent(), true
		}
		var cmd tea.Cmd
		p.workflow.detailView, cmd = p.workflow.detailView.Update((*msg))
		return cmd, true
	}
	return extractedResult0, false
}

func applyWorkflowCommandCasePlanTask(p *chatPage, command string, value string, ctx context.Context) (extractedResult0 tea.Cmd, extractedResult1 error) {
	if (command == "task" || command == "plan" && p.workflow.task.Objective != "") && value == "" {
		return p.openTask(), nil
	}
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

}

func workflowViewTaskSteps(p *chatPage, task *workflow.Task, line *string) {
	if len(task.Steps) > 0 {
		done := 0
		for _, step := range task.Steps {
			if step.Status == "completed" {
				done++
			}
		}
		(*line) = fmt.Sprintf(config.Text(" %s · Plano %d/%d · %s · /plan", " %s · Plan %d/%d · %s · /plan"), modeLabel(p.mode()), done, len(task.Steps), task.Status)
	}
}

func planRowsTaskContract(task *workflow.Task, rows *[]string) {
	if task.Contract != nil {
		(*rows) = append((*rows), " "+config.Text("Resultado esperado: ", "Expected outcome: ")+task.Contract.Kind)
		for _, target := range task.Contract.Targets {
			(*rows) = append((*rows), " "+config.Text("Alvo: ", "Target: ")+panelText(target))
		}
		if len(task.Contract.Checks) > 0 {
			(*rows) = append((*rows), " "+config.Text("Checks exigidos: ", "Required checks: ")+strings.Join(task.Contract.Checks, ", "))
		}
		for _, criterion := range task.Contract.Acceptance {
			(*rows) = append((*rows), " "+config.Text("Aceite: ", "Acceptance: ")+panelText(criterion))
		}
	}
}
