package page

import (
	"context"
	"fmt"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (p *chatPage) FooterState() util.FooterState {
	state := util.FooterState{Mode: string(p.mode()), Terminal: p.terminal.focused}
	if p.app != nil && p.app.CoderAgent != nil {
		model := models.Model{}
		if cfg := config.Get(); cfg != nil {
			model = config.ModelWithProviderDefaults(models.SupportedModels[cfg.Agents[config.AgentCoder].Model])
		}
		state.ContextWindow = model.ContextWindow
		if usage, ok := p.app.CoderAgent.(agent.UsageControl); ok && p.session.ID != "" {
			if metrics, err := usage.Usage(context.Background(), p.session.ID); err == nil {
				if metrics.ContextWindow > 0 {
					state.ContextWindow = metrics.ContextWindow
					state.ContextTokens = metrics.ContextTokens
					state.ContextKnown = true
					state.ContextEstimated = metrics.ContextEstimated
				}
				if metrics.LastModel == model.ID && metrics.LastOutputTokens > 0 && metrics.LastOutputDurationNS > 0 {
					state.TokensPerSecond = float64(metrics.LastOutputTokens) * 1e9 / float64(metrics.LastOutputDurationNS)
					state.SpeedEstimated = metrics.LastDurationSource != "provider"
				}
			}
		}
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
	if state.Busy && !p.activityStarted.IsZero() {
		state.Activity += fmt.Sprintf(" · %ds", int(time.Since(p.activityStarted).Seconds()))
	}
	return state
}
