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
	applyAgentFooterState(p, &state)
	state.Activity = p.footerActivity(state)
	if state.Activity == "" && p.terminal.snapshot.Running && !state.Terminal {
		state.Activity = config.Text("Terminal ativo · ", "Terminal running · ") + config.Shortcut("terminal")
	}
	if state.Busy && !p.activityStarted.IsZero() {
		state.Activity += fmt.Sprintf(" · %ds", int(time.Since(p.activityStarted).Seconds()))
	}
	return state
}

func applyAgentFooterState(p *chatPage, state *util.FooterState) {
	if p.app != nil && p.app.CoderAgent != nil {
		state.Busy = p.app.CoderAgent.IsSessionBusy(p.session.ID)
		model := models.Model{}
		if cfg := config.Get(); cfg != nil {
			model = config.ModelWithProviderDefaults(models.SupportedModels[cfg.Agents[config.AgentCoder].Model])
		}
		state.ContextWindow = model.ContextWindow
		readFooterUsageMetrics(p, state, &model)
		if queue, ok := p.app.CoderAgent.(agent.QueueControl); ok {
			state.Pending = len(queue.QueuedInstructions(p.session.ID))
		}
	}
}

func readFooterUsageMetrics(p *chatPage, state *util.FooterState, model *models.Model) {
	if usage, ok := p.app.CoderAgent.(agent.UsageControl); ok {
		applyFooterUsageMetrics(p, state, model, usage)
	}
}

func applyFooterUsageMetrics(p *chatPage, state *util.FooterState, model *models.Model, usage agent.UsageControl) {
	if metrics, err := usage.Usage(context.Background(), p.session.ID); err == nil {
		if metrics.ActiveModel == model.ID && metrics.ActiveContextWindow > 0 {
			state.ContextWindow = metrics.ActiveContextWindow
		}
		if metrics.ContextWindow > 0 {
			state.ContextWindow = metrics.ContextWindow
			state.ContextTokens = metrics.ContextTokens
			state.ContextKnown = true
			state.ContextEstimated = metrics.ContextEstimated
		}
		if state.Busy && metrics.StreamActive && metrics.StreamModel == model.ID {
			state.AwaitingModel = metrics.StreamOutputBytes == 0
			state.SpeedLive = !state.AwaitingModel
			state.SpeedEstimated = true
			// The initial burst can contain server-buffered data. Measure
			// only subsequent output between real sample timestamps.
			if metrics.StreamSamples >= 2 && metrics.StreamSampleDurationNS >= int64(250*time.Millisecond) {
				state.TokensPerSecond = metrics.StreamSampleTokens * 1e9 / float64(metrics.StreamSampleDurationNS)
			}
		} else if !state.Busy && metrics.LastModel == model.ID && metrics.LastOutputTokens > 0 && metrics.LastOutputDurationNS > 0 {
			state.TokensPerSecond = float64(metrics.LastOutputTokens) * 1e9 / float64(metrics.LastOutputDurationNS)
			state.SpeedEstimated = metrics.LastDurationSource != "provider"
		}
	}
}

// A live request owns the phase; a prior message cannot label newer deltas.
func (p *chatPage) footerActivity(state util.FooterState) string {
	if p.workflow.question.ID != "" {
		return config.Text("Aguardando sua resposta", "Waiting for your answer")
	}
	if state.Busy {
		if state.AwaitingModel {
			return config.Text("Aguardando modelo", "Awaiting model")
		}
		if state.SpeedLive {
			return config.Text("Gerando resposta", "Generating response")
		}
		if p.progress != "" {
			return p.progress
		}
		return config.Text("Trabalhando…", "Working…")
	}
	if p.outcome != "" {
		return p.activityLabel()
	}
	return ""
}
