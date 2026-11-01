package ui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/interaction"
)

// question is the agent's pending question and the answer being composed.
type question struct {
	q        interaction.Question
	index    int // current item of a batch
	cursor   int // focused choice; len(choices) is the free-text row
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
	return interaction.QuestionItem{Question: q.q.Question, Explanation: q.q.Explanation, Choices: q.q.Choices, Type: "single_choice"}
}

// poll reads the pending question and the saved plan; neither is published
// as an event, so the screen asks while a run is live.
func (m *Model) poll() {
	if m.app == nil || m.session.ID == "" {
		return
	}
	if m.app.Questions != nil {
		if q, ok := m.app.Questions.Pending(m.session.ID); ok {
			if m.question == nil || m.question.q.ID != q.ID {
				m.question = &question{q: q, cursor: -1, checked: map[int]bool{}}
			}
		} else {
			m.question = nil
		}
	}
	if m.app.Workflows != nil {
		if task, err := m.app.Workflows.Get(context.Background(), m.session.ID); err == nil {
			m.task = task
		}
	}
	m.layout()
}

func (m *Model) questionKey(msg tea.KeyPressMsg) tea.Cmd {
	q := m.question
	item := q.item()
	count := len(item.Choices)
	key := msg.String()
	switch {
	case key == "esc":
		m.app.CoderAgent.Cancel(m.session.ID)
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
		q.index, q.cursor, q.checked, q.text = q.index+1, -1, map[int]bool{}, ""
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
	box := Box{Title: textFor("Pergunta do agente", "Agent question"), Width: m.width - 2*gutter + 2, PadX: 1, PadY: 1, Accent: P.Ask, TitleColor: P.Ask}
	if n := len(q.q.Questions); n > 1 {
		box.Meta = fmt.Sprintf(textFor("%d de %d", "%d of %d"), q.index+1, n)
	}
	keys := fmt.Sprintf("1–%d", max(1, min(9, len(item.Choices))))
	box.Legend = fmt.Sprintf(textFor("%s escolher · ↑↓ navegar · Enter responder · Esc cancela", "%s choose · ↑↓ navigate · Enter answer · Esc cancels"), keys)
	if item.Type == "multi_choice" {
		box.Legend = fmt.Sprintf(textFor("%s marcar · Espaço alternar · Enter responder · Esc cancela", "%s check · Space toggle · Enter answer · Esc cancels"), keys)
	}
	if q.required {
		box.Legend = textFor("Escolha uma opção ou escreva uma resposta", "Choose an option or write an answer")
	}
	inner := box.Inner()
	var rows []string
	for _, line := range strings.Split(ansi.Wrap(item.Question, inner-2, ""), "\n") {
		rows = append(rows, on(P.Surface, P.Strong).Bold(true).Render(" "+line))
	}
	if item.Explanation != "" {
		rows = append(rows, on(P.Surface, P.Muted).Render(" "+ansi.Truncate(item.Explanation, inner-2, "…")))
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
		bg, fg := P.Surface, P.Text
		bar := on(bg, bg).Render(" ")
		if i == q.cursor {
			bg, fg = P.Selected, P.Strong
			bar = on(P.Surface, P.Ask).Render("▌")
		}
		row := bar + on(bg, P.Muted).Render(number) + on(bg, markColor).Render(mark) + on(bg, fg).Render(" "+choice)
		rows = append(rows, Line(row, inner, bg))
	}
	if len(item.Choices) > 0 || item.Type == "text" || len(item.Choices) == 0 {
		bg, fg := P.Surface, P.Faint
		bar := on(bg, bg).Render(" ")
		if q.cursor == len(item.Choices) {
			bg, fg = P.Selected, P.Strong
			bar = on(P.Surface, P.Ask).Render("▌")
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
		rows = append(rows, Line(bar+on(bg, P.Muted).Render(number)+on(bg, P.Faint).Render("✎")+on(bg, fg).Render(" "+text), inner, bg))
	}
	for i := range rows {
		rows[i] = Line(rows[i], inner, P.Surface)
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
	box := Box{
		Title:  fmt.Sprintf(textFor("Plano · %d de %d concluídas", "Plan · %d of %d completed"), done, len(steps)),
		Meta:   modeLabel(string(m.task.Mode)) + textFor(" · tarefa salva", " · task saved"),
		Legend: textFor("/plan recolher · /task detalhes", "/plan fold · /task details"),
		Width:  m.width - 2*gutter + 2, PadX: 1, PadY: 1, Accent: P.Plan, TitleColor: P.Plan,
	}
	inner := box.Inner()
	label := fmt.Sprintf(" %d%%", done*100/max(1, len(steps)))
	span := max(1, inner-len(label)-2)
	filled := done * span / max(1, len(steps))
	rows := []string{" " + on(P.Surface, P.Plan).Render(strings.Repeat("█", filled)) + on(P.Surface, P.Rule).Render(strings.Repeat("░", span-filled)) + on(P.Surface, P.Muted).Render(label), ""}
	for i, step := range steps {
		bg, fg, mark, markColor := P.Surface, P.Text, "○", P.Faint
		title := on(bg, fg).Render(fmt.Sprintf("%d. %s", i+1, step.Title))
		switch step.Status {
		case "completed":
			mark, markColor = "✓", P.Success
			title = on(bg, P.Muted).Strikethrough(true).Render(fmt.Sprintf("%d. %s", i+1, step.Title))
		case "in_progress":
			bg, mark, markColor = P.Selected, "●", P.Plan
			title = on(bg, P.Strong).Bold(true).Render(fmt.Sprintf("%d. %s", i+1, step.Title))
		}
		row := on(bg, markColor).Render(" "+mark+" ") + title
		rows = append(rows, Line(row, inner, bg))
	}
	if objective := strings.TrimSpace(m.task.Objective); objective != "" {
		rows = append(rows, "", on(P.Surface, P.Muted).Render(" "+ansi.Truncate(textFor("Objetivo: ", "Objective: ")+objective, inner-2, "…")))
	}
	for i := range rows {
		rows[i] = Line(rows[i], inner, P.Surface)
	}
	return indentBlock(box.Render(strings.Join(rows, "\n")))
}

// planStrip is the folded plan: progress, the current step and the key.
func (m *Model) planStrip() string {
	steps := m.task.Steps
	done, current := 0, ""
	for i, step := range steps {
		if step.Status == "completed" {
			done++
		}
		if current == "" && step.Status != "completed" {
			current = fmt.Sprintf("%d. %s", i+1, step.Title)
			if step.Status == "in_progress" {
				current = "● " + current
			} else {
				current = "○ " + current
			}
		}
	}
	head := on(P.Raised, P.Plan).Bold(true).Render(fmt.Sprintf(textFor(" Plano %d/%d ", " Plan %d/%d "), done, len(steps)))
	tail := on(P.Raised, P.Muted).Render(" /plan ")
	middle := on(P.Raised, P.Text).Render(" " + current)
	strip := Spread(head+middle, tail, m.width-2*gutter, P.Raised)
	return on(P.Ground, P.Ground).Render(strings.Repeat(" ", gutter)) + strip + on(P.Ground, P.Ground).Render(strings.Repeat(" ", gutter))
}

// workView is the card above the composer: approval, question or plan.
func (m *Model) workView() string {
	switch {
	case m.approval != nil:
		return m.approvalView()
	case m.question != nil:
		return m.questionView()
	case m.failure != nil:
		return m.failureView()
	case len(m.task.Steps) > 0 && m.showPlan:
		return m.planView()
	case len(m.task.Steps) > 0 && m.width >= 40:
		return m.planStrip()
	}
	return ""
}

func (m *Model) togglePlan() tea.Cmd {
	m.poll()
	m.showPlan = !m.showPlan
	m.layout()
	return nil
}

// failureView is the proposal's error card: what stopped, that the draft and
// progress were kept, and the ways forward as buttons.
func (m *Model) failureView() string {
	box := Box{Title: "■ " + textFor("Execução interrompida", "Run stopped"), Width: m.width - 2*gutter + 2, PadX: 1, PadY: 1, Accent: P.Danger, TitleColor: P.Danger, Background: P.Surface,
		Legend: textFor("Ctrl+R retomar · Ctrl+O modelo · Ctrl+E detalhes · Esc dispensar", "Ctrl+R resume · Ctrl+O model · Ctrl+E details · Esc dismiss")}
	inner := box.Inner()
	reason := strings.Join(strings.Fields(m.failure.Error()), " ")
	rows := []string{
		on(P.Surface, P.Strong).Bold(true).Render(" " + ansi.Truncate(failureTitle(reason), inner-2, "…")),
		on(P.Surface, P.Muted).Render(" " + textFor("Rascunho e progresso foram preservados.", "Draft and progress were kept.")),
	}
	if m.showFailure {
		for _, line := range strings.Split(ansi.Wrap(reason, inner-2, ""), "\n") {
			rows = append(rows, on(P.Surface, P.Faint).Render(" "+line))
		}
	}
	rows = append(rows, "", " "+Button(textFor("Retomar · Ctrl+R", "Resume · Ctrl+R"), true, P.Danger)+on(P.Surface, P.Surface).Render("  ")+
		Button(textFor("Trocar modelo · Ctrl+O", "Change model · Ctrl+O"), false, P.Danger)+on(P.Surface, P.Surface).Render("  ")+
		Button(textFor("Detalhes · Ctrl+E", "Details · Ctrl+E"), false, P.Danger))
	for i := range rows {
		rows[i] = Line(rows[i], inner, P.Surface)
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
