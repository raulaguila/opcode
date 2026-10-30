package agent

import (
	"context"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

// Each auxiliary operation owns its last measured nonstreaming envelope. A
// compacted history creates a replacement; an earlier candidate is never sent.
// Providers/test doubles without preparation retain their existing budget path.
type auxiliaryRequestPreparation struct {
	provider.Provider
	preparer provider.RequestPreparer
	prepared provider.PreparedRequest
}

func (p *auxiliaryRequestPreparation) RequestFootprint(history []message.Message, definitions []tools.BaseTool) (provider.RequestFootprint, error) {
	p.prepared = nil
	prepared, err := p.preparer.PrepareRequest(history, definitions, false)
	if err != nil {
		return provider.RequestFootprint{}, err
	}
	footprint, err := prepared.RequestFootprint()
	if err == nil {
		p.prepared = prepared
	}
	return footprint, err
}

type preparedAuxiliaryProvider struct {
	provider.Provider
	prepared provider.PreparedRequest
}

func (p *preparedAuxiliaryProvider) SendMessages(ctx context.Context, _ []message.Message, _ []tools.BaseTool) (*provider.ProviderResponse, error) {
	return p.prepared.SendMessages(ctx)
}

func sendAuxiliaryRequest(ctx context.Context, name config.AgentName, connection provider.Provider, history []message.Message, definitions []tools.BaseTool) (*provider.ProviderResponse, error) {
	budgetProvider := connection
	preparation := &auxiliaryRequestPreparation{Provider: connection}
	if preparer, ok := connection.(provider.RequestPreparer); ok {
		preparation.preparer = preparer
		budgetProvider = preparation
	}
	bounded, err := auxiliaryBudgetTools(name, budgetProvider, history, definitions)
	if err != nil {
		return nil, err
	}
	if preparation.prepared != nil {
		connection = &preparedAuxiliaryProvider{Provider: connection, prepared: preparation.prepared}
	}
	// The adapter remains inside the usual deadline/cancellation boundary.
	response, err := timedProvider(connection).SendMessages(ctx, bounded, definitions)
	if err != nil {
		err = publicProviderFailure(err, connection.Model())
	}
	return response, err
}
