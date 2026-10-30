package provider

import (
	"fmt"
	"slices"
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
}

func (p *baseProvider) WithSystemPrompt(system string) (Provider, error) {
	opts := p.options
	opts.systemMessage = system
	return p.cloneRequest(opts)
}

// SourceCitationController binds a repair's reply schema to references supplied
// by the agent. It never reads files, verifies claims, or grants authorization.
// The ordinary agent guard still checks the resulting response before completion.
type SourceCitationController interface {
	WithSourceCitations([]string) (Provider, error)
}

func (p *baseProvider) WithSourceCitations(citations []string) (Provider, error) {
	if len(citations) > 8 {
		return nil, fmt.Errorf("too many source citation choices")
	}
	total := 0
	for _, c := range citations {
		total += len(c)
		if len(c) == 0 || total > 2048 {
			return nil, fmt.Errorf("source citation choices exceed limit")
		}
	}
	opts := p.options
	opts.sourceCitations = slices.Clone(citations)
	return p.cloneRequest(opts)
}
