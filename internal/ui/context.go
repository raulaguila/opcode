package ui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/llm/agent"
)

// showContext reports how much of the model's window the next request takes;
// compact first folds finished tool output and old turns, without a model call.
func (m *Model) showContext(compact bool) tea.Cmd {
	control, ok := m.app.CoderAgent.(agent.ContextControl)
	if !ok || m.session.ID == "" {
		m.info = textFor("Sem conversa para medir", "No conversation to measure")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := control.ContextStatus(ctx, m.session.ID)
	if compact && err == nil {
		status, err = control.CompactContext(ctx, m.session.ID)
	}
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	m.info = contextLine(status, compact)
	if usage, ok := m.app.CoderAgent.(agent.UsageControl); ok {
		if metrics, err := usage.Usage(ctx, m.session.ID); err == nil {
			m.info += cacheLine(metrics)
		}
	}
	return nil
}

// cacheLine shows how much of the prompt the provider served from its cache.
// A drop means the request prefix changed between steps.
func cacheLine(metrics agent.UsageMetrics) string {
	if metrics.CachedInputTokens <= 0 || metrics.InputTokens <= 0 {
		return ""
	}
	return fmt.Sprintf(textFor(" · cache: %d%% do prompt reaproveitado", " · cache: %d%% of the prompt reused"), metrics.CachedInputTokens*100/metrics.InputTokens)
}

func contextLine(status agent.ContextStatus, compact bool) string {
	used := status.EstimatedInputTokens + status.ReservedOutputTokens + status.SafetyTokens
	line := fmt.Sprintf(textFor("Contexto: %d de %d tokens", "Context: %d of %d tokens"), used, status.ContextWindow)
	if status.ContextWindow > 0 {
		line += fmt.Sprintf(" (%d%%)", used*100/status.ContextWindow)
	}
	if compact {
		line += fmt.Sprintf(textFor(" · %d mensagens compactadas", " · %d messages compacted"), status.CompactedMessages)
	}
	if !status.Fits {
		line += textFor(" · não cabe: compacte ou troque de modelo", " · does not fit: compact or change model")
	}
	return line
}
