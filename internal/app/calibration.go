package app

import (
	"context"
	"github.com/raulaguila/opcode/internal/llm/agent"
)

func (p *configurableCoder) RecalibrateContext(ctx context.Context, id string) (agent.ContextStatus, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if c, ok := p.delegate.(agent.ContextCalibrationControl); ok {
		return c.RecalibrateContext(ctx, id)
	}
	return agent.ContextStatus{}, errSetupRequired
}
