package page

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/tui/util"
)

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
	if result0, handled := batchQuestionKeyWorkflowDetails(p, &msg, key); handled {
		return result0
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
	if result0, handled := batchQuestionKeyIndexQQuestions(p, key, &q, index); handled {
		return result0
	}
	item := q.Questions[index]
	switch key {
	case "up", "down":
		return batchQuestionKeyChatPageUp(p, key, &item)
	case " ":
		if result0, handled := batchQuestionKeyChatPageDefault(p, &item); handled {
			return result0
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

func batchQuestionKeyChatPageUp(p *chatPage, key string, item *interaction.QuestionItem) (extractedResult0 tea.Cmd) {
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

}

func batchQuestionKeyChatPageDefault(p *chatPage, item *interaction.QuestionItem) (extractedResult0 tea.Cmd, handled bool) {
	if p.workflow.choice >= 0 && p.workflow.answer.Value() == "" {
		if item.Type != "multi_choice" {
			p.workflow.selectedChoices = map[int]bool{p.workflow.choice: true}
		} else {
			p.workflow.selectedChoices[p.workflow.choice] = !p.workflow.selectedChoices[p.workflow.choice]
		}
		return nil, true
	}
	return extractedResult0, false
}

func batchQuestionKeyIndexQQuestions(p *chatPage, key string, q *interaction.Question, index int) (extractedResult0 tea.Cmd, handled bool) {
	if index >= len(q.Questions) {
		if key == "enter" || key == "ctrl+s" {
			if err := p.app.Questions.AnswerBatch(p.session.ID, q.ID, p.workflow.batchAnswers); err != nil {
				return util.ReportError(err), true
			}
			p.workflow.question = interaction.Question{}
			p.workflow.batchAnswers = nil
			return p.resizeContent(), true
		}
		return nil, true
	}
	return extractedResult0, false
}

func batchQuestionKeyWorkflowDetails(p *chatPage, msg *tea.KeyMsg, key string) (extractedResult0 tea.Cmd, handled bool) {
	if p.workflow.details {
		if key == "esc" || key == "enter" {
			p.workflow.details = false
			return p.resizeContent(), true
		}
		var cmd tea.Cmd
		p.workflow.detailView, cmd = p.workflow.detailView.Update((*msg))
		return cmd, true
	}
	return extractedResult0, false
}
