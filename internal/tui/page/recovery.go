package page

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/tools"
)

// The plan and recovery actions stay beside the composer, where the next
// instruction is entered. They never replace the conversation being read.
func (p *chatPage) composerPanel() string {
	var panels []string
	if view := p.workflowView(); view != "" {
		panels = append(panels, view)
	}
	if p.recoveryHeight() > 0 {
		state := "Execução interrompida"
		if p.recovery.resolved {
			state = "Execução retomada"
		}
		first := state + " · " + panelText(tools.RedactCredentials(p.recovery.err.Error()))
		panels = append(panels, ansi.Truncate(first, max(1, p.width), "…")+"\n"+ansi.Truncate(" /retry Retomar · /error Detalhes · /settings Configuração", max(1, p.width), "…"))
	}
	return strings.Join(panels, "\n")
}
func (p *chatPage) recoveryHeight() int {
	if p.recovery.err == nil || p.width < 20 || p.height < 12 {
		return 0
	}
	return 2
}
func (p *chatPage) composerPanelHeight() int { return p.workflowHeight() + p.recoveryHeight() }
func (p *chatPage) insertComposerPanel(view string) string {
	panel := p.composerPanel()
	if panel == "" {
		return view
	}
	_, h := p.layout.GetSize()
	_, editorHeight := p.editor.GetSize()
	lines := strings.Split(view, "\n")
	offset := max(0, min(len(lines), h-editorHeight))
	lines = append(lines[:offset], append(strings.Split(panel, "\n"), lines[offset:]...)...)
	return strings.Join(lines, "\n")
}
func (p *chatPage) recoveryMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	_, height := p.layout.GetSize()
	_, editorHeight := p.editor.GetSize()
	if msg.Y != height-editorHeight+p.workflowHeight()+1 {
		return nil
	}
	// Actions are also available as slash commands and in the command palette.
	if msg.X < 18 {
		return p.workspaceCommand("retry", "")
	}
	if msg.X < 37 {
		return p.workspaceCommand("error", "")
	}
	return p.workspaceCommand("settings", "")
}
func (p *chatPage) queueSummary() string {
	if p.app == nil || p.app.CoderAgent == nil {
		return ""
	}
	if queue, ok := p.app.CoderAgent.(agent.QueueControl); ok {
		if n := len(queue.QueuedInstructions(p.session.ID)); n > 0 {
			return fmt.Sprintf(" · Fila %d · /queue", n)
		}
	}
	return ""
}
func (p *chatPage) PersistDraft() error {
	if drafts, ok := p.composer.(interface{ PersistDraft() error }); ok {
		return drafts.PersistDraft()
	}
	return nil
}

func (p *chatPage) PrepareSessionDraft(id string) error {
	if id == p.session.ID {
		return nil
	}
	if drafts, ok := p.composer.(interface{ PrepareSessionDraft(string) error }); ok {
		return drafts.PrepareSessionDraft(id)
	}
	return nil
}
