package page

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (p *chatPage) batchQuestionHeight() int {
	if p.workflow.details {
		return min(max(3, p.height-5), 18)
	}
	if p.workflow.questionIndex >= len(p.workflow.question.Questions) {
		return 4 + len(p.workflow.question.Questions)
	}
	item := p.workflow.question.Questions[p.workflow.questionIndex]
	h := 4 + len(item.Choices)
	if item.Explanation != "" {
		h++
	}
	return h
}

func (p *chatPage) batchQuestionView(height int) string {
	w := max(1, p.width-2)
	line := func(s string) string { return ansi.Truncate(panelText(s), w, "…") }
	q := p.workflow.question
	index := p.workflow.questionIndex
	rows := []string{}
	if index >= len(q.Questions) {
		rows = append(rows, config.Text(" Revisar respostas", " Review answers"))
		for i, item := range q.Questions {
			answer := p.workflow.batchAnswers[i]
			value := answer.Text
			if len(answer.Choices) > 0 {
				value = strings.Join(answer.Choices, ", ")
			}
			rows = append(rows, fmt.Sprintf(" %d. %s: %s", i+1, item.Question, value))
		}
		rows = append(rows, config.Text(" Enter envia · Shift+Tab volta · F2 detalhes · Esc cancela", " Enter send · Shift+Tab back · F2 details · Esc cancel"))
	} else {
		item := q.Questions[index]
		rows = append(rows, fmt.Sprintf(config.Text(" Pergunta %d/%d · %s", " Question %d/%d · %s"), index+1, len(q.Questions), item.Question))
		if item.Explanation != "" {
			rows = append(rows, " "+item.Explanation)
		}
		for i, c := range item.Choices {
			mark := "○"
			if p.workflow.selectedChoices[i] {
				mark = "●"
			}
			if item.Type == "multi_choice" {
				mark = "[ ]"
				if p.workflow.selectedChoices[i] {
					mark = "[✓]"
				}
			}
			cursor := " "
			if p.workflow.choice == i {
				cursor = "›"
			}
			rows = append(rows, fmt.Sprintf(" %s %s %s", cursor, mark, c))
		}
		p.workflow.answer.Width = max(1, w-4)
		rows = append(rows, " "+p.workflow.answer.View())
		hint := config.Text(" Enter/Tab próxima · Shift+Tab volta · F2 detalhes · Esc cancela", " Enter/Tab next · Shift+Tab back · F2 details · Esc cancel")
		if len(item.Choices) > 0 {
			hint = config.Text(" ↑↓ opção · Espaço marca · digitar responde · Enter próxima · F2", " ↑↓ option · Space select · type alternative · Enter next · F2")
		}
		rows = append(rows, hint)
	}
	if p.workflow.details {
		p.workflow.detailView.Width = w
		p.workflow.detailView.Height = max(1, height-1)
		// Full questions and review values remain scrollable without mutating answers.
		p.workflow.detailView.SetContent(ansi.Hardwrap(strings.Join(rows, "\n\n"), w, true))
		return p.workflow.detailView.View() + "\n" + line(config.Text(" F2/Esc volta · ↑↓/PgUp/PgDn rola", " F2/Esc back · ↑↓/PgUp/PgDn scroll"))
	}
	for i := range rows {
		if !(index < len(q.Questions) && i == len(rows)-2) {
			rows[i] = line(rows[i])
		}
	}
	if len(rows) > height {
		// Keep the question and response visible in a short terminal.
		if height >= 3 {
			if index < len(q.Questions) && len(q.Questions[index].Choices) > 0 && height >= 4 {
				choiceStart := 1
				if q.Questions[index].Explanation != "" {
					choiceStart++
				}
				count := max(1, height-3)
				start := choiceStart + max(0, min(p.workflow.choice-count+1, len(q.Questions[index].Choices)-count))
				tail := rows[len(rows)-2:]
				rows = append([]string{rows[0]}, append(rows[start:min(start+count, choiceStart+len(q.Questions[index].Choices))], tail...)...)
			} else {
				rows = append([]string{rows[0]}, rows[len(rows)-(height-1):]...)
			}
		} else {
			rows = rows[len(rows)-height:]
		}
	}
	return styles.BaseStyle().Foreground(theme.CurrentTheme().Text()).Width(max(0, p.width)).Render(strings.Join(rows, "\n"))
}

func (p *chatPage) saveBatchAnswer() {
	i := p.workflow.questionIndex
	if i >= len(p.workflow.question.Questions) {
		return
	}
	item := p.workflow.question.Questions[i]
	answer := interaction.AnswerItem{QuestionID: item.ID, Text: p.workflow.answer.Value()}
	if strings.TrimSpace(answer.Text) == "" {
		answer.Text = ""
		for j, c := range item.Choices {
			if p.workflow.selectedChoices[j] {
				answer.Choices = append(answer.Choices, c)
			}
		}
		if len(answer.Choices) == 0 && item.Type != "multi_choice" && p.workflow.choice >= 0 {
			answer.Choices = []string{item.Choices[p.workflow.choice]}
		}
	}
	p.workflow.batchAnswers[i] = answer
}

func (p *chatPage) restoreBatchAnswer() {
	p.workflow.answer.SetValue("")
	p.workflow.choice = -1
	p.workflow.selectedChoices = map[int]bool{}
	i := p.workflow.questionIndex
	if i >= len(p.workflow.question.Questions) {
		return
	}
	answer := p.workflow.batchAnswers[i]
	p.workflow.answer.SetValue(answer.Text)
	for j, c := range p.workflow.question.Questions[i].Choices {
		for _, selected := range answer.Choices {
			if c == selected {
				p.workflow.selectedChoices[j] = true
				p.workflow.choice = j
			}
		}
	}
}

func (p *chatPage) batchQuestionKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if key == "f2" {
		p.workflow.details = !p.workflow.details
		return p.resizeContent()
	}
	if p.workflow.details {
		if key == "esc" || key == "enter" {
			p.workflow.details = false
			return p.resizeContent()
		}
		var cmd tea.Cmd
		p.workflow.detailView, cmd = p.workflow.detailView.Update(msg)
		return cmd
	}
	if key == "esc" {
		p.app.CoderAgent.Cancel(p.session.ID)
		return nil
	}
	q := p.workflow.question
	index := p.workflow.questionIndex
	if key == "shift+tab" {
		p.saveBatchAnswer()
		if index > 0 {
			p.workflow.questionIndex--
			p.restoreBatchAnswer()
		}
		return p.resizeContent()
	}
	if index >= len(q.Questions) {
		if key == "enter" || key == "ctrl+s" {
			if err := p.app.Questions.AnswerBatch(p.session.ID, q.ID, p.workflow.batchAnswers); err != nil {
				return util.ReportError(err)
			}
			p.workflow.question = interaction.Question{}
			p.workflow.batchAnswers = nil
			return p.resizeContent()
		}
		return nil
	}
	item := q.Questions[index]
	switch key {
	case "up", "down":
		if len(item.Choices) > 0 {
			delta := 1
			if key == "up" {
				delta = -1
				if p.workflow.choice < 0 {
					p.workflow.choice = 0
				}
			}
			p.workflow.choice = (p.workflow.choice + delta + len(item.Choices)) % len(item.Choices)
		}
		return nil
	case " ":
		if p.workflow.choice >= 0 && p.workflow.answer.Value() == "" {
			if item.Type != "multi_choice" {
				p.workflow.selectedChoices = map[int]bool{p.workflow.choice: true}
			} else {
				p.workflow.selectedChoices[p.workflow.choice] = !p.workflow.selectedChoices[p.workflow.choice]
			}
			return nil
		}
	case "enter", "tab":
		p.saveBatchAnswer()
		single := interaction.Question{Questions: []interaction.QuestionItem{item}}
		if err := interaction.ValidateAnswers(single, []interaction.AnswerItem{p.workflow.batchAnswers[index]}); err != nil {
			return util.ReportError(err)
		}
		p.workflow.questionIndex++
		p.restoreBatchAnswer()
		return p.resizeContent()
	}
	var cmd tea.Cmd
	p.workflow.answer, cmd = p.workflow.answer.Update(msg)
	return cmd
}
