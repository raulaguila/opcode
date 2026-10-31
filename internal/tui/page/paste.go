package page

import (
	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/tui/util"
)

// Pasted text stays data: it cannot submit a prompt or trigger a shortcut.
func (p *chatPage) paste(msg tea.PasteMsg) tea.Cmd {
	if p.terminal.focused {
		if p.terminal.snapshot.PTY && p.terminal.snapshot.Running {
			if err := p.app.Processes.Input(p.session.ID, p.terminal.selected, msg.Content); err != nil {
				return util.ReportError(err)
			}
		}
		return nil
	}
	if p.WorkspaceFocused() {
		return p.workspacePaste(msg)
	}
	if p.workflow.question.ID != "" {
		return p.questionPaste(msg)
	}
	if transcript, ok := p.transcript.(interface{ TranscriptPaste(tea.PasteMsg) bool }); ok && transcript.TranscriptPaste(msg) {
		return nil
	}
	_, cmd := p.editor.Update(msg)
	return cmd
}

func (p *chatPage) questionPaste(msg tea.PasteMsg) tea.Cmd {
	if p.workflow.details {
		return nil
	}
	q := p.workflow.question
	count := len(q.Choices)
	if len(q.Questions) > 0 {
		if p.workflow.questionIndex >= len(q.Questions) {
			return nil
		}
		count = len(q.Questions[p.workflow.questionIndex].Choices)
	}
	p.workflow.choice = count
	focus := p.focusQuestionInput(count)
	var cmd tea.Cmd
	p.workflow.answer, cmd = p.workflow.answer.Update(msg)
	return tea.Batch(focus, cmd)
}

func (p *chatPage) workspacePaste(msg tea.PasteMsg) tea.Cmd {
	if p.workspace.itemEditor != nil {
		_, _, cmd := p.workspace.itemEditor.Update(msg)
		return cmd
	}
	if p.workspace.kind != "contract" || p.workspace.focus < 0 || p.workspace.focus >= len(p.workspace.fields) {
		return nil
	}
	var cmd tea.Cmd
	p.workspace.fields[p.workspace.focus], cmd = p.workspace.fields[p.workspace.focus].Update(msg)
	return cmd
}
