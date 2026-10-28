package page

import (
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (p *chatPage) FooterState() util.FooterState {
	state := util.FooterState{Mode: string(p.mode()), Terminal: p.terminal.focused}
	if p.app != nil && p.app.CoderAgent != nil {
		state.Busy = p.app.CoderAgent.IsSessionBusy(p.session.ID)
		if queue, ok := p.app.CoderAgent.(agent.QueueControl); ok {
			state.Pending = len(queue.QueuedInstructions(p.session.ID))
		}
	}
	if state.Busy || p.workflow.question.ID != "" || p.outcome == "blocked" || p.outcome == "partial" {
		state.Activity = p.activityLabel()
		if state.Busy && p.progress == "" {
			state.Activity = config.Text("Trabalhando…", "Working…")
		}
	}
	if state.Activity == "" && p.terminal.snapshot.Running && !state.Terminal {
		state.Activity = config.Text("Terminal ativo · ", "Terminal running · ") + config.Shortcut("terminal")
	}
	return state
}
