package page

import (
	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (p *chatPage) saveBatchAnswer() {
	i := p.workflow.questionIndex
	if i >= len(p.workflow.question.Questions) {
		return
	}
	item := p.workflow.question.Questions[i]
	p.workflow.batchAnswers[i] = p.selectedQuestionAnswer(item)
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
	if answer.Text != "" {
		p.workflow.choice = len(p.workflow.question.Questions[i].Choices)
	}
	for j, c := range p.workflow.question.Questions[i].Choices {
		for _, selected := range answer.Choices {
			if c == selected {
				p.workflow.selectedChoices[j] = true
				p.workflow.choice = j
			}
		}
	}
	p.focusQuestionInput(len(p.workflow.question.Questions[i].Choices))
}

func (p *chatPage) batchQuestionKey(msg tea.KeyPressMsg) tea.Cmd {
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
		return p.navigateQuestionAnswer(key, len(item.Choices))
	case "space":
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
	return p.editQuestionAnswer(msg, len(item.Choices))
}

func batchQuestionKeyChatPageDefault(p *chatPage, item *interaction.QuestionItem) (extractedResult0 tea.Cmd, handled bool) {
	if p.workflow.choice >= 0 && p.workflow.choice < len(item.Choices) {
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

func batchQuestionKeyWorkflowDetails(p *chatPage, msg *tea.KeyPressMsg, key string) (extractedResult0 tea.Cmd, handled bool) {
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
