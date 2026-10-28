package provider

import (
	"context"
	"encoding/json"

	"github.com/raulaguila/opcode/internal/message"
)

func newProviderState(opts providerClientOptions, api string, data any) *message.ProviderState {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	return &message.ProviderState{Provider: opts.model.Provider, Model: opts.model.APIModel, API: api, Data: raw}
}

func providerState(msg message.Message, opts providerClientOptions, api string) json.RawMessage {
	finish := msg.FinishPart()
	if finish == nil || finish.ProviderState == nil {
		return nil
	}
	state := finish.ProviderState
	if state.Provider != opts.model.Provider || state.Model != opts.model.APIModel || state.API != api {
		return nil
	}
	return state.Data
}

func publishEvent(ctx context.Context, events chan<- ProviderEvent, event ProviderEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case events <- event:
		return true
	}
}
