package provider

import (
	"fmt"
)

// ContextWindowController clones a connection for a request with a smaller
// context. It never resends messages or executes tools itself.
type ContextWindowController interface{ WithContextWindow(int64) (Provider, error) }

func (p *baseProvider) WithContextWindow(window int64) (Provider, error) {
	if window < 4096 {
		return nil, fmt.Errorf("adaptive context minimum is 4096 tokens")
	}
	opts := p.options
	opts.model.ContextWindow = window
	opts.model.ContextSource = "learned after provider context/memory failure"
	opts.maxTokens = min(opts.maxTokens, window/4)
	if opts.maxTokens <= 0 {
		opts.maxTokens = min(2048, window/4)
	}
	return p.cloneRequest(opts)
}

// SystemPromptController creates a request-scoped connection, preserving auth
// and protocol settings without mutating a concurrent connection.
type SystemPromptController interface {
	WithSystemPrompt(string) (Provider, error)
	WithoutThinking() (Provider, error)
	WithMaxTokens(int64) (Provider, error)
}

// WithMaxTokens gives one kind of request its own output limit: a model that
// thinks spends it before answering.
func (p *baseProvider) WithMaxTokens(limit int64) (Provider, error) {
	opts := p.options
	opts.maxTokens = limit
	return p.cloneRequest(opts)
}

// WithoutThinking asks a model that thinks by default to answer at once, on the
// backends where that is a request option.
func (p *baseProvider) WithoutThinking() (Provider, error) {
	opts := p.options
	opts.noThinking = true
	return p.cloneRequest(opts)
}

func (p *baseProvider) WithSystemPrompt(system string) (Provider, error) {
	opts := p.options
	opts.systemMessage = system
	return p.cloneRequest(opts)
}
