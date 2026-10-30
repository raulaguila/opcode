package page

import tea "github.com/charmbracelet/bubbletea"

func (p *chatPage) SetInputFocused(focused bool) tea.Cmd {
	p.inputEnabled = focused
	return p.syncInputFocus()
}

func (p *chatPage) syncInputFocus() tea.Cmd {
	enabled := p.inputEnabled && !p.terminal.focused && !p.WorkspaceFocused()
	question := p.workflow.question.ID != ""
	var cmds []tea.Cmd
	for i := range p.workspace.fields {
		wantsFocus := p.inputEnabled && p.WorkspaceFocused() && !p.terminal.focused && i == p.workspace.focus
		if wantsFocus && !p.workspace.fields[i].Focused() {
			cmds = append(cmds, p.workspace.fields[i].Focus())
		} else if !wantsFocus && p.workspace.fields[i].Focused() {
			p.workspace.fields[i].Blur()
		}
	}
	if editor, ok := p.composer.(interface{ SetInputFocused(bool) tea.Cmd }); ok {
		cmds = append(cmds, editor.SetInputFocused(enabled && !question))
	}
	wantsQuestion := enabled && p.questionInputActive()
	if wantsQuestion && !p.workflow.answer.Focused() {
		cmds = append(cmds, p.workflow.answer.Focus())
	} else if !wantsQuestion && p.workflow.answer.Focused() {
		p.workflow.answer.Blur()
	}
	return tea.Batch(cmds...)
}

func (p *chatPage) questionInputActive() bool {
	q := p.workflow.question
	if q.ID == "" || p.workflow.details {
		return false
	}
	count := len(q.Choices)
	if len(q.Questions) > 0 {
		if p.workflow.questionIndex < 0 || p.workflow.questionIndex >= len(q.Questions) {
			return false
		}
		count = len(q.Questions[p.workflow.questionIndex].Choices)
	}
	return p.customQuestionAnswer(count)
}
