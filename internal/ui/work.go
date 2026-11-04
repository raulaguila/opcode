package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/workflow"
)

// question is the agent's pending question and the answer being composed.
type question struct {
	q        interaction.Question
	index    int // current item of a batch
	cursor   int // focused choice, the first at start; len(choices) is the free-text row
	checked  map[int]bool
	text     string
	answers  []interaction.AnswerItem
	required bool
}

// item is the question being answered now, batch or single.
func (q *question) item() interaction.QuestionItem {
	if len(q.q.Questions) > 0 {
		return q.q.Questions[q.index]
	}
	return interaction.QuestionItem{Question: q.q.Question, Explanation: q.q.Explanation, Choices: q.q.Choices, Notes: q.q.Notes, Type: "single_choice"}
}

// poll reads the pending question and the saved plan; neither is published
// as an event, so the screen asks while a run is live.
func (m *Model) poll() {
	m.pollProcesses()
	if m.app == nil || m.session.ID == "" {
		return
	}
	if m.app.Questions != nil {
		if q, ok := m.app.Questions.Pending(m.session.ID); ok {
			if m.question == nil || m.question.q.ID != q.ID {
				m.question = &question{q: q, checked: map[int]bool{}}
			}
		} else {
			m.question = nil
		}
	}
	if m.app.Workflows != nil {
		if task, err := m.app.Workflows.Get(context.Background(), m.session.ID); err == nil {
			m.setTask(task)
		}
	}
	m.layout()
}

// setTask folds a finished plan into a receipt; a new step brings it back.
func (m *Model) setTask(task workflow.Task) {
	if planDone(task.Steps) != planDone(m.task.Steps) {
		m.foldPlan = planDone(task.Steps)
	}
	m.task = task
}

func (m *Model) questionKey(msg tea.KeyPressMsg) tea.Cmd {
	q := m.question
	item := q.item()
	count := len(item.Choices)
	key := msg.String()
	switch {
	case key == "esc":
		// Answer later: the card steps aside and the agent keeps waiting.
		m.laterQuestion = q.q.ID
		m.layout()
		return nil
	case key == "up":
		q.cursor = max(0, q.cursor-1)
	case key == "down":
		q.cursor = min(count, q.cursor+1)
	case len(key) == 1 && key[0] >= '1' && key[0] <= '9' && int(key[0]-'0') <= count && q.cursor != count:
		q.cursor = int(key[0] - '1')
		q.pick()
	case key == "space" && q.cursor >= 0 && q.cursor < count:
		q.pick()
	case key == "enter":
		return m.answerQuestion()
	case key == "backspace":
		if r := []rune(q.text); len(r) > 0 {
			q.text = string(r[:len(r)-1])
		}
	default:
		if msg.Text != "" {
			q.cursor, q.required = count, false
			q.text += msg.Text
		}
	}
	return nil
}

// pick selects the focused choice: one for single choice, toggled for many.
func (q *question) pick() {
	if q.item().Type == "multi_choice" {
		q.checked[q.cursor] = !q.checked[q.cursor]
		return
	}
	q.checked = map[int]bool{q.cursor: true}
}

func (m *Model) answerQuestion() tea.Cmd {
	q := m.question
	item := q.item()
	answer := interaction.AnswerItem{QuestionID: item.ID}
	if q.cursor == len(item.Choices) || len(item.Choices) == 0 {
		answer.Text = strings.TrimSpace(q.text)
	} else {
		if q.cursor >= 0 && item.Type != "multi_choice" {
			q.checked = map[int]bool{q.cursor: true}
		}
		for i, choice := range item.Choices {
			if q.checked[i] {
				answer.Choices = append(answer.Choices, choice)
			}
		}
	}
	if answer.Text == "" && len(answer.Choices) == 0 {
		q.required = true
		return nil
	}
	if len(q.q.Questions) == 0 {
		text := answer.Text
		if text == "" {
			text = answer.Choices[0]
		}
		if err := m.app.Questions.Answer(m.session.ID, q.q.ID, text); err != nil {
			m.notice = err.Error()
		}
		m.question = nil
		m.layout()
		return nil
	}
	q.answers = append(q.answers, answer)
	if q.index+1 < len(q.q.Questions) {
		q.index, q.cursor, q.checked, q.text = q.index+1, 0, map[int]bool{}, ""
		m.layout()
		return nil
	}
	if err := m.app.Questions.AnswerBatch(m.session.ID, q.q.ID, q.answers); err != nil {
		m.notice = err.Error()
	}
	m.question = nil
	m.layout()
	return nil
}

// questionView is the proposal's question card, in the Ask color.
func (m *Model) questionView() string {
	q := m.question
	item := q.item()
	box := Box{Title: textFor("Pergunta do agente", "Agent question"), Width: m.width - 2*gutter + 2, PadX: 1, PadY: 1, Accent: P.Ask, TitleColor: P.Ask, Background: P.AskCard, Outer: P.Ground}
	if n := len(q.q.Questions); n > 1 {
		box.Meta = fmt.Sprintf(textFor("%d de %d", "%d of %d"), q.index+1, n)
	}
	keys := fmt.Sprintf("1–%d", max(1, min(9, len(item.Choices)+1)))
	legend := fmt.Sprintf(textFor("%s escolher · ↑↓ navegar · Enter responder · Esc responder depois", "%s choose · ↑↓ navigate · Enter answer · Esc answer later"), keys)
	if item.Type == "multi_choice" {
		legend = fmt.Sprintf(textFor("%s marcar · Espaço alternar · Enter responder (%d) · Esc responder depois", "%s check · Space toggle · Enter answer (%d) · Esc answer later"), keys, len(q.checked))
	}
	if q.index+1 < len(q.q.Questions) {
		legend = strings.Replace(legend, " · Esc", textFor(" → próxima pergunta · Esc", " → next question · Esc"), 1)
	}
	inner := box.Inner()
	bg0 := P.AskCard
	var rows []string
	for _, line := range strings.Split(ansi.Wrap(item.Question, inner-2, ""), "\n") {
		rows = append(rows, on(bg0, P.Strong).Bold(true).Render("  "+line))
	}
	if item.Explanation != "" {
		rows = append(rows, on(bg0, P.Muted).Render("  "+ansi.Truncate(item.Explanation, inner-3, "…")))
	}
	rows = append(rows, "")
	for i, choice := range item.Choices {
		mark := "○"
		if item.Type == "multi_choice" {
			mark = "☐"
		}
		markColor := P.Faint
		if q.checked[i] {
			mark, markColor = "●", P.Success
			if item.Type == "multi_choice" {
				mark = "☑"
			}
		}
		number := "  "
		if i < 9 {
			number = fmt.Sprintf("%d ", i+1)
		}
		bg, fg := bg0, P.Text
		bar := on(bg, bg).Render(" ")
		if i == q.cursor {
			bg, fg = P.Selected, P.Strong
			bar = on(bg0, P.Ask).Render("▌")
		}
		row := bar + on(bg, P.Muted).Render(" "+number) + on(bg, markColor).Render(mark) + on(bg, fg).Render(" "+choice)
		if i < len(item.Notes) && item.Notes[i] != "" {
			row += on(bg, P.Muted).Render("  " + item.Notes[i])
		}
		rows = append(rows, Line(row, inner, bg))
	}
	if len(item.Choices) > 0 || item.Type == "text" || len(item.Choices) == 0 {
		bg, fg := bg0, P.Faint
		bar := on(bg, bg).Render(" ")
		if q.cursor == len(item.Choices) {
			bg, fg = P.Selected, P.Strong
			bar = on(bg0, P.Ask).Render("▌")
		}
		text := q.text
		if text == "" {
			text = textFor("Outra resposta…", "Other answer…")
		} else {
			text += "▏"
		}
		number := fmt.Sprintf("%d ", len(item.Choices)+1)
		if len(item.Choices) == 0 || len(item.Choices) >= 9 {
			number = "  "
		}
		rows = append(rows, Line(bar+on(bg, P.Muted).Render(" "+number)+on(bg, P.Faint).Render("✎")+on(bg, fg).Render(" "+text), inner, bg))
	}
	if q.required {
		rows = append(rows, on(bg0, P.Danger).Render("  "+textFor("Escolha uma opção ou escreva uma resposta", "Choose an option or write an answer")))
	}
	rows = append(rows, "")
	rows = append(rows, KeyRow(legend, inner, bg0)...)
	for i := range rows {
		rows[i] = Line(rows[i], inner, bg0)
	}
	return indentBlock(box.Render(strings.Join(rows, "\n")))
}

func indentBlock(block string) string {
	pad := on(P.Ground, P.Ground).Render(strings.Repeat(" ", gutter-1))
	lines := strings.Split(block, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

const (
	planFoldArrow    = "▴"
	planUnfoldArrow  = "▾"
	planArrowColumns = 8 // wider than the arrow, for a forgiving click
)

// planView is the proposal's plan card: title with the count, the mode in the
// corner, a progress row and the steps.
func (m *Model) planView() string {
	steps := m.task.Steps
	done := 0
	for _, step := range steps {
		if step.Status == "completed" {
			done++
		}
	}
	bg := P.PlanCard
	box := Box{
		Title: fmt.Sprintf(textFor("Plano · %d de %d concluídas", "Plan · %d of %d completed"), done, len(steps)),
		Meta:  modeLabel(string(m.task.Mode)) + textFor(" · tarefa salva ", " · task saved ") + planFoldArrow,
		Width: m.width - 2*gutter + 2, PadX: 1, PadY: 1, Accent: P.Plan, TitleColor: P.Plan, Background: bg, Outer: P.Ground,
	}
	inner := box.Inner()
	filled := done * inner / max(1, len(steps))
	rows := []string{on(bg, P.Plan).Render(strings.Repeat("━", filled)) + on(bg, P.Rule).Render(strings.Repeat("━", inner-filled)), ""}
	first, last := planWindow(steps, max(3, m.height/2-10))
	for i, step := range steps {
		if i < first || i >= last {
			continue
		}
		rowBg, mark, markColor := bg, "○", P.Faint
		bar := on(bg, bg).Render(" ")
		title := on(rowBg, P.Text).Render(fmt.Sprintf("%d. %s", i+1, step.Title))
		switch step.Status {
		case "completed":
			mark, markColor = "✓", P.Success
			title = on(rowBg, P.Muted).Strikethrough(true).Render(fmt.Sprintf("%d. %s", i+1, step.Title))
		case "in_progress":
			rowBg, mark, markColor = P.PlanStep, "●", P.Plan
			bar = on(bg, P.Plan).Render("▌")
			title = on(rowBg, P.Strong).Bold(true).Render(fmt.Sprintf("%d. %s", i+1, step.Title))
		}
		row := bar + on(rowBg, markColor).Render(" "+mark+" ") + title
		if step.Status == "in_progress" {
			row = Spread(row, on(rowBg, P.Plan).Render(textFor("agora ", "now ")), inner, rowBg)
		}
		rows = append(rows, Line(row, inner, rowBg))
	}
	if hidden := len(steps) - (last - first); hidden > 0 {
		rows = append(rows, on(bg, P.Faint).Render(fmt.Sprintf(textFor("   ↑ %d · ↓ %d etapas fora da vista", "   ↑ %d · ↓ %d steps out of view"), first, len(steps)-last)))
	}
	keys := KeyRow(textFor("Alt+↑↓ rolar · /plan recolher", "Alt+↑↓ scroll · /plan fold"), inner, bg)
	if objective := strings.TrimSpace(m.task.Objective); objective != "" {
		keys[1] = Spread(on(bg, P.Muted).Render(textFor("Resultado esperado: ", "Expected result: ")+objective), on(bg, P.Strong).Bold(true).Render("Alt+↑↓")+on(bg, P.Muted).Render(textFor(" rolar  ", " scroll  "))+on(bg, P.Strong).Bold(true).Render("/plan")+on(bg, P.Muted).Render(textFor(" recolher", " fold")), inner, bg)
	}
	rows = append(append(rows, ""), keys...)
	for i := range rows {
		rows[i] = Line(rows[i], inner, bg)
	}
	return indentBlock(box.Render(strings.Join(rows, "\n")))
}

// planDone says every step finished; a blocked or skipped one keeps the plan open.
func planDone(steps []workflow.Step) bool {
	return len(steps) > 0 && !slices.ContainsFunc(steps, func(step workflow.Step) bool { return step.Status != "completed" })
}

// planWindow keeps a long plan from filling the screen: limit steps around the
// first unfinished one, with the step before it for context.
func planWindow(steps []workflow.Step, limit int) (first, last int) {
	if len(steps) <= limit {
		return 0, len(steps)
	}
	active := slices.IndexFunc(steps, func(step workflow.Step) bool { return step.Status != "completed" })
	if active < 0 {
		active = len(steps) - 1
	}
	first = min(max(0, active-1), len(steps)-limit)
	return first, first + limit
}

// planStrip is the folded plan: progress, the current step and the arrow.
func (m *Model) planStrip() string {
	steps := m.task.Steps
	done, current := 0, ""
	for i, step := range steps {
		if step.Status == "completed" {
			done++
		}
		if current == "" && step.Status != "completed" {
			current = fmt.Sprintf("%d. %s", i+1, step.Title)
			current = "→ " + current
		}
	}
	head := on(P.Surface, P.Plan).Render(fmt.Sprintf(textFor(" Plano %d/%d ", " Plan %d/%d "), done, len(steps)))
	if planDone(steps) {
		head = on(P.Surface, P.Success).Render(fmt.Sprintf(textFor(" ✓ Plano concluído · %d/%d ", " ✓ Plan completed · %d/%d "), done, len(steps)))
	}
	tail := on(P.Surface, P.Plan).Render(" " + planUnfoldArrow + " ")
	middle := on(P.Surface, P.Muted).Render(" " + current)
	strip := Spread(head+middle, tail, m.width-2*gutter, P.Surface)
	return on(P.Ground, P.Ground).Render(strings.Repeat(" ", gutter)) + strip + on(P.Ground, P.Ground).Render(strings.Repeat(" ", gutter))
}

// workView is the card above the composer: approval, question or plan.
func (m *Model) workView() string {
	switch {
	case m.approval != nil:
		return m.approvalView()
	case m.question != nil && !m.answeringLater():
		return m.questionView()
	}
	rows := make([]string, 0, 2)
	switch {
	case len(m.task.Steps) == 0 || m.width < 40:
	case m.foldPlan:
		rows = append(rows, m.planStrip())
	default:
		rows = append(rows, m.planView())
	}
	if outcome := m.outcomeRow(); outcome != "" {
		rows = append(rows, outcome)
	}
	return strings.Join(rows, "\n")
}

// outcomeRow says a task stopped short; the answer alone may not make it clear.
func (m *Model) outcomeRow() string {
	var label string
	tone := P.Warning
	switch m.task.Status {
	case "partial":
		label = textFor("◐ Tarefa parcial", "◐ Task partial")
	case "blocked":
		label, tone = textFor("■ Tarefa bloqueada", "■ Task blocked"), P.Danger
	default:
		return ""
	}
	if m.busy || m.width < 20 || m.task.Summary == workflow.StaleValidation {
		return ""
	}
	row := " " + label
	if summary := strings.Join(strings.Fields(m.task.Summary), " "); summary != "" {
		row += " · " + summary
	}
	inner := m.width - 2*gutter
	pad := on(P.Ground, P.Ground).Render(strings.Repeat(" ", gutter))
	return pad + Line(on(P.Surface, tone).Render(ansi.Truncate(row, inner, "…")), inner, P.Surface) + pad
}

func (m *Model) togglePlan() tea.Cmd {
	m.poll()
	if len(m.task.Steps) == 0 {
		m.info = textFor("Sem plano ainda · peça um em Planejar", "No plan yet · ask for one in Plan")
		return nil
	}
	m.foldPlan = !m.foldPlan
	m.layout()
	return nil
}

// clickPlan folds or unfolds the plan from the arrow at the card's top right.
func (m *Model) clickPlan(x, y int) bool {
	work := m.workView()
	if work == "" || len(m.task.Steps) == 0 || m.approval != nil || m.question != nil && !m.answeringLater() {
		return false
	}
	top := m.height - footerRows - m.composerRows() - len(m.mention) - strings.Count(work, "\n") - 1
	if y != top || x < m.width-planArrowColumns {
		return false
	}
	m.togglePlan()
	return true
}

// failureView is the proposal's error card: what stopped, that the draft and
// progress were kept, and the ways forward as buttons.
func (m *Model) failureView() string {
	box := Box{Width: m.width - 2*gutter + 2, PadX: 2, PadY: 1, Accent: P.DangerEdge, Background: P.DangerCard, Outer: P.Ground}
	inner := box.Inner()
	reason := strings.Join(strings.Fields(m.failure.Error()), " ")
	bg := P.DangerCard
	rows := []string{
		on(bg, P.Danger).Bold(true).Render(ansi.Truncate("■ "+textFor("Parado: ", "Stopped: ")+failureTitle(reason), inner, "…")),
		on(bg, P.Muted).Render(ansi.Truncate(strings.TrimSpace(cause(reason)+" "+textFor("Rascunho e progresso foram preservados.", "Draft and progress were kept.")), inner, "…")),
	}
	if m.showFailure {
		for _, line := range strings.Split(ansi.Wrap(reason, inner, ""), "\n") {
			rows = append(rows, on(bg, P.Faint).Render(line))
		}
	}
	gap := on(bg, bg).Render("  ")
	rows = append(rows, "", Button(textFor("Retomar · Ctrl+R", "Resume · Ctrl+R"), false, P.Danger)+gap+
		Button(textFor("Detalhes · Ctrl+E", "Details · Ctrl+E"), false, P.Danger)+gap+
		Button(textFor("Trocar modelo · Ctrl+O", "Change model · Ctrl+O"), false, P.Danger)+gap+on(bg, P.Faint).Render(textFor("Esc dispensa", "Esc dismisses")))
	for i := range rows {
		rows[i] = Line(rows[i], inner, bg)
	}
	return indentBlock(box.Render(strings.Join(rows, "\n")))
}

// failureTitle names the common causes in the user's words.
func failureTitle(reason string) string {
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "context canceled"), strings.Contains(lower, "cancel"):
		return textFor("Execução cancelada", "Run cancelled")
	case strings.Contains(lower, "out of memory"):
		return textFor("Memória insuficiente no servidor do modelo", "The model server ran out of memory")
	case strings.Contains(lower, "connection refused"), strings.Contains(lower, "deadline exceeded"), strings.Contains(lower, "no such host"):
		return textFor("Provedor indisponível", "Provider unavailable")
	case strings.Contains(lower, "401"), strings.Contains(lower, "unauthorized"), strings.Contains(lower, "api key"):
		return textFor("Autenticação necessária", "Authentication required")
	case strings.Contains(lower, "repeated tool calls"), strings.Contains(lower, "failed 3 times"):
		return textFor("O modelo repetiu a mesma ação sem progresso", "The model repeated the same action without progress")
	case strings.Contains(lower, "step limit"), strings.Contains(lower, "max steps"):
		return textFor("Limite de etapas atingido", "Step limit reached")
	case strings.Contains(lower, "tool result budget"):
		return textFor("Limite de saída de ferramentas do turno atingido", "This turn's tool output limit was reached")
	case strings.Contains(lower, "context budget exceeded"):
		return textFor("A conversa não cabe mais no modelo", "The conversation no longer fits the model")
	}
	return textFor("A execução parou com um erro", "The run stopped with an error")
}

func (m *Model) failureKey(key string) (tea.Cmd, bool) {
	switch key {
	case "ctrl+r":
		m.failure, m.showFailure = nil, false
		if m.lastText != "" {
			m.composer.SetValue(m.lastText)
			return m.submit(), true
		}
		return nil, true
	case "ctrl+e":
		m.showFailure = !m.showFailure
		m.layout()
		return nil, true
	case "esc":
		m.failure, m.showFailure = nil, false
		m.layout()
		return nil, true
	}
	return nil, false
}

// cause explains a known failure in a sentence, without the raw error,
// which waits for Ctrl+E; an unknown failure has no sentence.
func cause(reason string) string {
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "connection refused"), strings.Contains(lower, "deadline exceeded"), strings.Contains(lower, "no such host"):
		return textFor("O servidor do modelo não respondeu.", "The model server did not answer.")
	case strings.Contains(lower, "401"), strings.Contains(lower, "unauthorized"), strings.Contains(lower, "api key"):
		return textFor("A chave da API foi recusada ou está ausente.", "The API key was refused or is missing.")
	case strings.Contains(lower, "out of memory"):
		return textFor("O modelo não coube na memória disponível.", "The model did not fit in the available memory.")
	case strings.Contains(lower, "repeated tool calls"), strings.Contains(lower, "failed 3 times"):
		return textFor("A mesma ação falhou várias vezes do mesmo jeito.", "The same action failed the same way several times.")
	case strings.Contains(lower, "step limit"), strings.Contains(lower, "max steps"):
		return textFor("A tarefa usou todas as etapas permitidas.", "The task used every step it was allowed.")
	case strings.Contains(lower, "tool result budget"):
		return textFor("Retomar continua pelo plano salvo; em tarefas grandes, aumente runtime.toolBudget.", "Resume continues from the saved plan; for large tasks, raise runtime.toolBudget.")
	case strings.Contains(lower, "context budget exceeded"):
		return textFor("Use Compactar conversa (/compact) e retome, ou troque de modelo.", "Use Compact conversation (/compact) and resume, or change model.")
	}
	return ""
}
