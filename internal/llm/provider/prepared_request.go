package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

// PreparedRequest owns immutable serialized bytes and the exact connection and
// protocol that produced them. It cannot expose prompts or credentials. Build a
// new request whenever history, tools or request-scoped options change; only
// send the instance whose footprint passed the context budget.
type PreparedRequest interface {
	RequestFootprint() (RequestFootprint, error)
	StreamResponse(context.Context) <-chan ProviderEvent
	SendMessages(context.Context) (*ProviderResponse, error)
}

type RequestPreparer interface {
	PrepareRequest([]message.Message, []tools.BaseTool, bool) (PreparedRequest, error)
}

type preparedHTTP struct {
	transport *apiTransport
	protocol  wireProtocol
	path      string
	body      []byte
	stream    bool
	footprint RequestFootprint
}

func (p *baseProvider) PrepareRequest(history []message.Message, definitions []tools.BaseTool, stream bool) (PreparedRequest, error) {
	history, err := normalizeImageURLs(p.cleanMessages(history))
	if err != nil {
		return nil, err
	}
	owner, ok := p.client.(interface{ requestHTTP() *httpProvider })
	if !ok {
		return nil, fmt.Errorf("provider does not support prepared requests")
	}
	prepared, payload, err := owner.requestHTTP().prepareEnvelope(history, definitions, stream)
	if err != nil {
		return nil, err
	}
	text, visual, err := imageBudgetProjection(p.options.model, history, payload, prepared.body)
	if err != nil {
		return nil, err
	}
	prepared.footprint = p.serializedFootprint(text)
	prepared.footprint.WireBytes = int64(len(prepared.body))
	prepared.footprint.VisualTokens = visual
	return prepared, nil
}

func (p *httpProvider) requestHTTP() *httpProvider { return p }

func (p *httpProvider) prepare(history []message.Message, definitions []tools.BaseTool, stream bool) (*preparedHTTP, error) {
	prepared, _, err := p.prepareEnvelope(history, definitions, stream)
	return prepared, err
}

func (p *httpProvider) prepareEnvelope(history []message.Message, definitions []tools.BaseTool, stream bool) (*preparedHTTP, any, error) {
	history, err := normalizeImageURLs(history)
	if err != nil {
		return nil, nil, err
	}
	path, payload, err := p.protocol.request(history, definitions, stream)
	if err != nil {
		return nil, nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("encode provider request: %w", err)
	}
	if len(body) > maxProviderBytes {
		return nil, nil, fmt.Errorf("provider request exceeds 32 MiB")
	}
	return &preparedHTTP{transport: p.transport, protocol: p.protocol, path: path, body: body, stream: stream}, payload, nil
}

func (p *preparedHTTP) RequestFootprint() (RequestFootprint, error) { return p.footprint, nil }

func (p *preparedHTTP) SendMessages(ctx context.Context) (*ProviderResponse, error) {
	if p.stream {
		return nil, fmt.Errorf("prepared request requires streaming")
	}
	response, err := p.transport.postBytes(ctx, p.path, p.body, false)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxProviderBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxProviderBytes {
		return nil, fmt.Errorf("provider response exceeds %d bytes", maxProviderBytes)
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("provider returned invalid JSON")
	}
	return p.protocol.decodeResponse(raw)
}

func (p *preparedHTTP) StreamResponse(ctx context.Context) <-chan ProviderEvent {
	events := make(chan ProviderEvent)
	go func() {
		defer close(events)
		err := p.readStream(ctx, events)
		if err != nil && !errors.Is(err, context.Canceled) {
			publishEvent(ctx, events, ProviderEvent{Type: EventError, Error: err})
		}
	}()
	return events
}

func (p *preparedHTTP) readStream(ctx context.Context, events chan<- ProviderEvent) error {
	if !p.stream {
		return fmt.Errorf("prepared request does not support streaming")
	}
	response, err := p.transport.postBytes(ctx, p.path, p.body, true)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	// Once streaming starts, no layer retries the request.
	return p.protocol.readStream(ctx, response.Body, events)
}
