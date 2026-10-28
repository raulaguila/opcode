package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/extensions"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/workflow"
)

var errSetupRequired = fmt.Errorf("configure a provider and choose a model in Settings (Ctrl+O, Ctrl+P) before sending a message")

// configurableCoder is a stable service identity during setup and reconfiguration.
// Subscribers belong to this broker; exactly one delegate relay is owned at once.
type configurableCoder struct {
	*pubsub.Broker[agent.AgentEvent]
	app                  *App
	ctx                  context.Context
	mu                   sync.RWMutex
	delegate             agent.Service
	cancel               context.CancelFunc
	relay                sync.WaitGroup
	closed               bool
	connection           config.Provider
	hasConnection        bool
	auxiliaryConnections map[config.AgentName]boundConnection
}

func newConfigurableCoder(ctx context.Context, app *App) *configurableCoder {
	return &configurableCoder{Broker: pubsub.NewBroker[agent.AgentEvent](), app: app, ctx: ctx}
}
func (p *configurableCoder) bind(next agent.Service) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		if c, ok := next.(interface{ Close() error }); ok {
			_ = c.Close()
		}
		return fmt.Errorf("agent is closed")
	}
	if p.delegate != nil && p.delegate.IsBusy() {
		if c, ok := next.(interface{ Close() error }); ok {
			_ = c.Close()
		}
		return agent.ErrSessionBusy
	}
	if p.cancel != nil {
		p.cancel()
		p.relay.Wait()
	}
	if c, ok := p.delegate.(interface{ Close() error }); ok {
		_ = c.Close()
	}
	p.delegate = next
	p.auxiliaryConnections = map[config.AgentName]boundConnection{}
	if cfg := config.Get(); cfg != nil {
		for _, role := range []config.AgentName{config.AgentTitle, config.AgentSummarizer} {
			model, ok := models.SupportedModels[cfg.Agents[role].Model]
			if ok {
				if connection, err := config.ProviderForModel(model); err == nil {
					p.auxiliaryConnections[role] = boundConnection{model: model, provider: connection}
				}
			}
		}
	}
	p.connection = config.Provider{}
	p.hasConnection = false
	if connection, err := config.ProviderForModel(next.Model()); err == nil {
		p.connection = connection
		p.hasConnection = true
	}
	ctx, cancel := context.WithCancel(p.ctx)
	p.cancel = cancel
	events := next.Subscribe(ctx)
	p.relay.Add(1)
	go func() {
		defer p.relay.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				p.Publish(event.Type, event.Payload)
			}
		}
	}()
	return nil
}
func (p *configurableCoder) Model() models.Model {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.delegate == nil {
		return models.Model{}
	}
	return p.delegate.Model()
}
func (p *configurableCoder) SetMode(ctx context.Context, id string, mode workflow.Mode) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.delegate == nil {
		return errSetupRequired
	}
	return p.delegate.SetMode(ctx, id, mode)
}
func (p *configurableCoder) Run(ctx context.Context, id, content string, attachments ...message.Attachment) (<-chan agent.AgentEvent, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.delegate == nil {
		return nil, errSetupRequired
	}
	if err := p.validateConnection(); err != nil {
		return nil, err
	}
	return p.delegate.Run(ctx, id, content, attachments...)
}
func (p *configurableCoder) Submit(ctx context.Context, id, content string, attachments ...message.Attachment) (<-chan agent.AgentEvent, bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.delegate == nil {
		return nil, false, errSetupRequired
	}
	if err := p.validateConnection(); err != nil {
		return nil, false, err
	}
	return p.delegate.Submit(ctx, id, content, attachments...)
}
func (p *configurableCoder) Cancel(id string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.delegate != nil {
		p.delegate.Cancel(id)
	}
}
func (p *configurableCoder) IsSessionBusy(id string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.delegate != nil && p.delegate.IsSessionBusy(id)
}
func (p *configurableCoder) IsBusy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.delegate != nil && p.delegate.IsBusy()
}
func (p *configurableCoder) Update(name config.AgentName, id models.ModelID) (models.Model, error) {
	model, ok := models.SupportedModels[id]
	if !ok {
		return models.Model{}, fmt.Errorf("unknown model %s", id)
	}
	return p.SelectModel(name, model)
}
func (p *configurableCoder) SelectModel(name config.AgentName, model models.Model) (models.Model, error) {
	p.mu.Lock()
	delegate := p.delegate
	if delegate != nil {
		defer p.mu.Unlock()
		selected, err := delegate.SelectModel(name, model)
		if err == nil {
			if name == config.AgentCoder {
				p.connection, _ = config.ProviderForModel(delegate.Model())
				p.hasConnection = true
			}
			if name == config.AgentTitle || name == config.AgentSummarizer {
				if connection, e := config.ProviderForModel(selected); e == nil {
					p.auxiliaryConnections[name] = boundConnection{model: selected, provider: connection}
				}
			}
		}
		return selected, err
	}
	p.mu.Unlock()
	if name != config.AgentCoder {
		return models.Model{}, fmt.Errorf("choose the main model before the auxiliary model")
	}
	if err := config.UpdateAgentModelDefinition(name, model); err != nil {
		return models.Model{}, err
	}
	if err := p.app.ConfigureCoderAgent(); err != nil {
		return models.Model{}, err
	}
	return p.Model(), nil
}
func (p *configurableCoder) Summarize(ctx context.Context, id string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.delegate == nil {
		return errSetupRequired
	}
	if err := p.validateAuxiliaryConnection(config.AgentSummarizer); err != nil {
		return err
	}
	return p.delegate.Summarize(ctx, id)
}
func (p *configurableCoder) QueuedInstructions(id string) []agent.QueuedInstruction {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if c, ok := p.delegate.(agent.QueueControl); ok {
		return c.QueuedInstructions(id)
	}
	return nil
}
func (p *configurableCoder) TakeInstruction(id, instruction string) (agent.QueuedInstruction, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if c, ok := p.delegate.(agent.QueueControl); ok {
		return c.TakeInstruction(id, instruction)
	}
	return agent.QueuedInstruction{}, errSetupRequired
}
func (p *configurableCoder) ContextStatus(ctx context.Context, id string) (agent.ContextStatus, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if c, ok := p.delegate.(agent.ContextControl); ok {
		return c.ContextStatus(ctx, id)
	}
	return agent.ContextStatus{}, errSetupRequired
}
func (p *configurableCoder) CompactContext(ctx context.Context, id string) (agent.ContextStatus, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if c, ok := p.delegate.(agent.ContextControl); ok {
		return c.CompactContext(ctx, id)
	}
	return agent.ContextStatus{}, errSetupRequired
}
func (p *configurableCoder) Skills(ctx context.Context) ([]extensions.Skill, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if c, ok := p.delegate.(agent.ExtensionControl); ok {
		return c.Skills(ctx)
	}
	return nil, errSetupRequired
}
func (p *configurableCoder) InvokeSkill(ctx context.Context, id, name, args string) (<-chan agent.AgentEvent, bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if c, ok := p.delegate.(agent.ExtensionControl); ok {
		if err := p.validateConnection(); err != nil {
			return nil, false, err
		}
		return c.InvokeSkill(ctx, id, name, args)
	}
	return nil, false, errSetupRequired
}
func (p *configurableCoder) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.cancel != nil {
		p.cancel()
		p.relay.Wait()
	}
	var err error
	if c, ok := p.delegate.(interface{ Close() error }); ok {
		err = c.Close()
	}
	p.Broker.Shutdown()
	return err
}

// Called with the proxy lock held. A failed settings rebuild must never leave
// requests using credentials or an endpoint from the previous connection.
func (p *configurableCoder) validateConnection() error {
	current, err := config.ProviderForModel(p.delegate.Model())
	if err != nil {
		return err
	}
	if p.hasConnection && current != p.connection {
		return fmt.Errorf("provider settings changed; choose a model again to activate the connection")
	}
	for _, role := range []config.AgentName{config.AgentTitle, config.AgentSummarizer} {
		if err := p.validateAuxiliaryConnection(role); err != nil {
			return err
		}
	}
	return nil
}

type boundConnection struct {
	model    models.Model
	provider config.Provider
}

func (p *configurableCoder) validateAuxiliaryConnection(role config.AgentName) error {
	bound, ok := p.auxiliaryConnections[role]
	if !ok {
		return nil
	}
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration unavailable")
	}
	current, err := config.ProviderForModel(bound.model)
	if err != nil {
		return err
	}
	if cfg.Agents[role].Model != bound.model.ID || current != bound.provider {
		return fmt.Errorf("provider settings changed for %s; choose a model again to activate the connection", role)
	}
	return nil
}
