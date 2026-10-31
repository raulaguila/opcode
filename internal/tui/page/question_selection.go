package page

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/interaction"
)

func (p *chatPage) customQuestionAnswer(count int) bool {
	return count == 0 || p.workflow.choice == count
}

func (p *chatPage) focusQuestionInput(count int) tea.Cmd {
	if p.customQuestionAnswer(count) {
		if !p.workflow.answer.Focused() {
			return p.workflow.answer.Focus()
		}
	} else {
		p.workflow.answer.Blur()
	}
	return nil
}

func (p *chatPage) navigateQuestionAnswer(key string, count int) tea.Cmd {
	if count > 0 {
		delta := 1
		if key == "up" {
			delta = -1
			if p.workflow.choice < 0 {
				p.workflow.choice = 0
			}
		}
		p.workflow.choice = (p.workflow.choice + delta + count + 1) % (count + 1)
	}
	return p.focusQuestionInput(count)
}

func (p *chatPage) editQuestionAnswer(msg tea.KeyPressMsg, count int) tea.Cmd {
	if msg.Text != "" || msg.Code == tea.KeySpace || msg.Keystroke() == "ctrl+v" {
		p.workflow.choice = count
	}
	if !p.customQuestionAnswer(count) {
		return nil
	}
	focus := p.focusQuestionInput(count)
	var cmd tea.Cmd
	p.workflow.answer, cmd = p.workflow.answer.Update(msg)
	return tea.Batch(focus, cmd)
}

// The active row determines the answer; an inactive text draft never overrides it.
func (p *chatPage) selectedQuestionAnswer(item interaction.QuestionItem) interaction.AnswerItem {
	answer := interaction.AnswerItem{QuestionID: item.ID}
	if p.customQuestionAnswer(len(item.Choices)) {
		if strings.TrimSpace(p.workflow.answer.Value()) != "" {
			answer.Text = p.workflow.answer.Value()
		}
		return answer
	}
	if item.Type == "multi_choice" {
		for i, value := range item.Choices {
			if p.workflow.selectedChoices[i] {
				answer.Choices = append(answer.Choices, value)
			}
		}
	} else if p.workflow.choice >= 0 && p.workflow.choice < len(item.Choices) {
		answer.Choices = []string{item.Choices[p.workflow.choice]}
	}
	return answer
}
