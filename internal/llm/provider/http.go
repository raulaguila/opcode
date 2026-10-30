package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

// wireProtocol owns only a provider's JSON and stream format. Authentication,
// HTTP lifecycle and retries belong to the shared transport below.
type wireProtocol interface {
	request([]message.Message, []tools.BaseTool, bool) (string, any, error)
	decodeResponse(json.RawMessage) (*ProviderResponse, error)
	readStream(context.Context, io.Reader, chan<- ProviderEvent) error
}

type httpProvider struct {
	transport *apiTransport
	protocol  wireProtocol
}

type apiTransport struct {
	baseURL   string
	client    *http.Client
	headers   http.Header
	authorize func(context.Context, *http.Request, []byte) error
	refresh   func(context.Context) error
}

func newAPITransport(baseURL string) *apiTransport {
	transport := http.DefaultTransport
	if standard, ok := transport.(*http.Transport); ok {
		copy := standard.Clone()
		first := (config.RuntimeConfig{}).Defaults().FirstTokenTimeout
		if cfg := config.Get(); cfg != nil {
			first = cfg.Runtime.Defaults().FirstTokenTimeout
		}
		copy.ResponseHeaderTimeout = time.Duration(first) * time.Second
		transport = copy
	}
	return &apiTransport{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{
		Transport: transport,
		// Never forward provider credentials to a redirected host.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, headers: make(http.Header)}
}

func (t *apiTransport) post(ctx context.Context, path string, payload any, stream bool) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode provider request: %w", err)
	}
	return t.postBytes(ctx, path, body, stream)
}

func (t *apiTransport) postBytes(ctx context.Context, path string, body []byte, stream bool) (*http.Response, error) {
	endpoint, err := providerRequestURL(t.baseURL + path)
	if err != nil {
		return nil, err
	}
	refreshed := false
	for attempt := 0; ; attempt++ {
		resp, err := t.postOnce(ctx, endpoint, body, stream)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		if err := t.retryProviderFailure(ctx, resp, attempt, &refreshed); err != nil {
			return nil, err
		}
	}
}

// HTTPError deliberately omits request URLs, headers and request bodies, which
// may contain API keys, prompts or signed URLs.
type HTTPError struct {
	StatusCode      int
	RequestID, Body string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("provider HTTP %d (request %s): %s", e.StatusCode, e.RequestID, e.Body)
}
func requestID(h http.Header) string {
	for _, key := range []string{"x-request-id", "request-id", "x-amzn-requestid"} {
		if s := h.Get(key); s != "" {
			return s
		}
	}
	return "unknown"
}
func retryable(status int) bool {
	return status == 408 || status == 409 || status == 429 || status >= 500 && status <= 599
}
func retryDelay(h http.Header, attempt int) time.Duration {
	delay := time.Second << min(attempt, 5)
	if ms, err := strconv.ParseFloat(h.Get("retry-after-ms"), 64); err == nil && ms >= 0 {
		delay = time.Duration(min(ms, 30000)) * time.Millisecond
	} else if seconds, err := strconv.ParseFloat(h.Get("Retry-After"), 64); err == nil && seconds >= 0 {
		delay = time.Duration(min(seconds, 30) * float64(time.Second))
	} else if date, err := http.ParseTime(h.Get("Retry-After")); err == nil {
		delay = max(0, time.Until(date))
	}
	return min(delay, 30*time.Second)
}
func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *httpProvider) send(ctx context.Context, messages []message.Message, definitions []tools.BaseTool) (*ProviderResponse, error) {
	prepared, err := p.prepare(messages, definitions, false)
	if err != nil {
		return nil, err
	}
	return prepared.SendMessages(ctx)
}

func (p *httpProvider) stream(ctx context.Context, messages []message.Message, definitions []tools.BaseTool) <-chan ProviderEvent {
	prepared, err := p.prepare(messages, definitions, true)
	if err == nil {
		return prepared.StreamResponse(ctx)
	}
	events := make(chan ProviderEvent)
	go func() {
		defer close(events)
		if !errors.Is(err, context.Canceled) {
			publishEvent(ctx, events, ProviderEvent{Type: EventError, Error: err})
		}
	}()
	return events
}

const maxProviderBytes = 32 << 20

// SSE supports comments, CRLF, multi-line data, large tool arguments and an
// unterminated last frame. Provider adapters decide which event ends a turn.
func readSSE(r io.Reader, consume func(string, json.RawMessage) (bool, error)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), maxProviderBytes)
	var kind string
	var data []string
	size := 0
	flush := func() (bool, error) {
		if len(data) == 0 {
			kind = ""
			return false, nil
		}
		raw := strings.Join(data, "\n")
		data = nil
		size = 0
		name := kind
		kind = ""
		return consume(name, json.RawMessage(raw))
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			stop, err := flush()
			if stop || err != nil {
				return err
			}
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			kind = value
		case "data":
			size += len(value) + 1
			if size > maxProviderBytes {
				return fmt.Errorf("provider stream event exceeds size limit")
			}
			data = append(data, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	_, err := flush()
	return err
}

type object = map[string]any

func decodeObject(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func toolSchema(tool tools.BaseTool) object {
	info := tool.Info()
	required := info.Required
	if required == nil {
		required = []string{}
	}
	properties := info.Parameters
	if properties == nil {
		properties = object{}
	}
	return object{"type": "object", "properties": properties, "required": required}
}

func providerRequestURL(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", fmt.Errorf("invalid provider endpoint")
	}
	return parsed.String(), nil
}
func (t *apiTransport) postOnce(ctx context.Context, endpoint string, body []byte, stream bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = t.headers.Clone()
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if t.authorize != nil {
		if err := t.authorize(ctx, req, body); err != nil {
			return nil, fmt.Errorf("authenticate provider request: %w", err)
		}
	}
	// A transport error can follow server acceptance: never replay it.
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider request: %w", err)
	}
	return resp, nil
}
func (t *apiTransport) retryProviderFailure(ctx context.Context, resp *http.Response, attempt int, refreshed *bool) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("read provider error: %w", err)
	}
	failure := &HTTPError{StatusCode: resp.StatusCode, RequestID: requestID(resp.Header), Body: string(raw)}
	if resp.StatusCode == http.StatusUnauthorized && t.refresh != nil && !*refreshed {
		*refreshed = true
		if err := t.refresh(ctx); err != nil {
			return fmt.Errorf("refresh provider credential: %w", err)
		}
		return nil
	}
	if attempt >= maxRetries || !retryable(resp.StatusCode) {
		return failure
	}
	return waitRetry(ctx, retryDelay(resp.Header, attempt))
}
