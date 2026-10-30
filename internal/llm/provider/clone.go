package provider

import "fmt"

// Request clones share only immutable connection resources (headers, endpoint,
// TLS policy and the concurrency-safe HTTP/auth clients). Protocol options are
// copied; changing a prompt, window or citation schema never rebuilds a pool or
// appends provider defaults to an existing option chain.
type requestCloner interface {
	cloneRequest(providerClientOptions) providerClient
}

func (p *baseProvider) cloneRequest(options providerClientOptions) (Provider, error) {
	cloner, ok := p.client.(requestCloner)
	if !ok {
		return nil, fmt.Errorf("provider does not support request-scoped options")
	}
	return &baseProvider{options: options, client: cloner.cloneRequest(options)}, nil
}

// Constructors may normalize protocol-specific model IDs (e.g. Bedrock). Only
// request-scoped fields are replaced; credentials and normalized IDs stay bound
// to the original connection, including refresh closures and TLS settings.
func scopedRequestOptions(original, request providerClientOptions) providerClientOptions {
	original.systemMessage = request.systemMessage
	original.maxTokens = request.maxTokens
	original.model.ContextWindow = request.model.ContextWindow
	original.model.ContextSource = request.model.ContextSource
	original.sourceCitations = request.sourceCitations
	return original
}

func (c *openaiClient) cloneRequest(options providerClientOptions) providerClient {
	clone := *c
	clone.providerOptions = scopedRequestOptions(c.providerOptions, options)
	clone.httpProvider = &httpProvider{transport: c.transport, protocol: &clone}
	if azure, ok := c.protocol.(*azureProtocol); ok {
		clone.protocol = &azureProtocol{openaiClient: &clone, apiVersion: azure.apiVersion}
	}
	return &clone
}

func (c *anthropicClient) cloneRequest(options providerClientOptions) providerClient {
	clone := *c
	clone.providerOptions = scopedRequestOptions(c.providerOptions, options)
	clone.httpProvider = &httpProvider{transport: c.transport, protocol: &clone}
	return &clone
}

func (c *geminiClient) cloneRequest(options providerClientOptions) providerClient {
	clone := *c
	clone.providerOptions = scopedRequestOptions(c.providerOptions, options)
	clone.httpProvider = &httpProvider{transport: c.transport, protocol: &clone}
	return &clone
}

func (c *ollamaClient) cloneRequest(options providerClientOptions) providerClient {
	clone := *c
	clone.opts = scopedRequestOptions(c.opts, options)
	clone.httpProvider = &httpProvider{transport: c.transport, protocol: &clone}
	return &clone
}
