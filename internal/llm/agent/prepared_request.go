package agent

import (
	"context"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

// Scoped to a single budget decision: every changed/compacted history prepares
// a new immutable envelope. Only the last successfully measured one is sent.
type requestPreparation struct {
	provider.Provider
	preparer provider.RequestPreparer
	prepared provider.PreparedRequest
}

func (p *requestPreparation) RequestFootprint(history []message.Message, definitions []tools.BaseTool) (provider.RequestFootprint, error) {
	p.prepared = nil
	prepared, err := p.preparer.PrepareRequest(history, definitions, true)
	if err != nil {
		return provider.RequestFootprint{}, err
	}
	footprint, err := prepared.RequestFootprint()
	if err == nil {
		p.prepared = prepared
	}
	return footprint, err
}

// This adapter is local to one approved request and still runs through the
// normal first-token/idle timeout and cancellation relay. It never runs tools.
type preparedStreamProvider struct {
	provider.Provider
	prepared provider.PreparedRequest
}

func (p *preparedStreamProvider) StreamResponse(ctx context.Context, _ []message.Message, _ []tools.BaseTool) <-chan provider.ProviderEvent {
	return p.prepared.StreamResponse(ctx)
}
