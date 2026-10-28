package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }
func TestHTTPRetryReplaysPayloadAndClosesResponses(t *testing.T) {
	transport := newAPITransport("https://provider.test/v1")
	transport.headers.Set("Authorization", "Bearer unit-test-secret")
	var bodies []*trackedBody
	var requests int
	transport.client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		raw, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"model":"test"}`, string(raw))
		assert.Equal(t, "Bearer unit-test-secret", req.Header.Get("Authorization"))
		status := http.StatusTooManyRequests
		body := `{"error":"busy"}`
		if requests == 3 {
			status = 200
			body = `{"ok":true}`
		}
		reader := &trackedBody{Reader: strings.NewReader(body)}
		bodies = append(bodies, reader)
		return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"0"}}, Body: reader, Request: req}, nil
	})
	resp, err := transport.post(context.Background(), "/test", object{"model": "test"}, false)
	require.NoError(t, err)
	require.Equal(t, 3, requests)
	require.True(t, bodies[0].closed)
	require.True(t, bodies[1].closed)
	require.False(t, bodies[2].closed)
	require.NoError(t, resp.Body.Close())
	require.True(t, bodies[2].closed)
}
func TestHTTPRetriesAreBoundedAndRespectCancellation(t *testing.T) {
	t.Run("bounded", func(t *testing.T) {
		transport := newAPITransport("https://provider.test")
		count := 0
		transport.client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
			count++
			return &http.Response{StatusCode: 503, Header: http.Header{"Retry-After": {"0"}}, Body: io.NopCloser(strings.NewReader("unavailable")), Request: req}, nil
		})
		_, err := transport.post(context.Background(), "/test", object{}, true)
		var failure *HTTPError
		require.ErrorAs(t, err, &failure)
		require.Equal(t, 503, failure.StatusCode)
		require.Equal(t, maxRetries+1, count)
	})
	t.Run("cancel during backoff", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		transport := newAPITransport("https://provider.test")
		count := 0
		transport.client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
			count++
			cancel()
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"30"}}, Body: io.NopCloser(strings.NewReader("busy")), Request: req}, nil
		})
		_, err := transport.post(ctx, "/test", object{}, false)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 1, count)
	})
	t.Run("ambiguous network failure", func(t *testing.T) {
		transport := newAPITransport("https://provider.test")
		count := 0
		transport.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { count++; return nil, io.ErrUnexpectedEOF })
		_, err := transport.post(context.Background(), "/test", object{}, false)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.Equal(t, 1, count)
	})
}
func TestProviderCredentialsDoNotFollowRedirects(t *testing.T) {
	transport := newAPITransport("https://provider.test")
	transport.headers.Set("x-api-key", "secret")
	count := 0
	transport.client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		count++
		assert.Equal(t, "provider.test", req.URL.Host)
		return &http.Response{StatusCode: 307, Header: http.Header{"Location": {"https://other.test/collect"}}, Body: io.NopCloser(strings.NewReader("redirect")), Request: req}, nil
	})
	_, err := transport.post(context.Background(), "/test", object{}, false)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
	require.Equal(t, 1, count)
}
func TestSSEFrames(t *testing.T) {
	var seen []string
	input := ": keep alive\r\nevent: update\r\ndata: {\r\ndata: \"key\": 1}\r\n\r\nid: 2\ndata: {\"end\":true}"
	err := readSSE(strings.NewReader(input), func(kind string, data json.RawMessage) (bool, error) {
		seen = append(seen, kind+":"+string(data))
		return false, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"update:{\n\"key\": 1}", `:{"end":true}`}, seen)
}
func TestRetryAfterFormats(t *testing.T) {
	require.Equal(t, 1500*time.Millisecond, retryDelay(http.Header{"Retry-After": {"1.5"}}, 0))
	require.Equal(t, 250*time.Millisecond, retryDelay(http.Header{"Retry-After-Ms": {"250"}}, 0))
	date := time.Now().Add(15 * time.Second).UTC().Format(http.TimeFormat)
	delay := retryDelay(http.Header{"Retry-After": {date}}, 0)
	require.InDelta(t, 15, delay.Seconds(), 1.1)
	require.LessOrEqual(t, retryDelay(http.Header{"Retry-After": {"99999"}}, 0), 30*time.Second)
}
func TestStreamFailuresNeverCompleteOrRetry(t *testing.T) {
	fixtures := []struct {
		name, body string
		client     func(string) *httpProvider
	}{
		{"chat", `data: {"choices":[{"index":0,"delta":{"content":"partial"}}]}` + "\n\ndata: invalid\n\n", func(body string) *httpProvider {
			return chatClient(t, testOptions(models.ProviderOpenAI), 200, "text/event-stream", body, nil).httpProvider
		}},
		{"responses", `data: {"type":"response.output_text.delta","delta":"partial"}` + "\n\ndata: invalid\n\n", func(body string) *httpProvider {
			opts := testOptions(models.ProviderOpenAI)
			opts.model.API = "responses"
			return chatClient(t, opts, 200, "text/event-stream", body, nil).httpProvider
		}},
		{"anthropic", `data: {"type":"message_start","message":{"content":[],"usage":{}}}` + "\n\ndata: invalid\n\n", func(body string) *httpProvider {
			return anthropicTestClient(t, 200, "text/event-stream", body, nil).httpProvider
		}},
		{"gemini", `data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}` + "\n\ndata: invalid\n\n", func(body string) *httpProvider {
			return geminiTestClient(t, 200, "text/event-stream", body, nil).httpProvider
		}},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			client := fixture.client(fixture.body)
			original := client.transport.client.Transport
			count := 0
			client.transport.client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) { count++; return original.RoundTrip(req) })
			events := collectEvents(t, client.stream(context.Background(), userMessages(), nil))
			require.Equal(t, 1, count)
			require.NotEmpty(t, events)
			for _, event := range events {
				require.NotEqual(t, EventComplete, event.Type)
			}
			require.Equal(t, EventError, events[len(events)-1].Type)
		})
	}
}
func TestRefreshIsAttemptedOnlyOnce(t *testing.T) {
	transport := newAPITransport("https://provider.test")
	refreshes, requests := 0, 0
	transport.refresh = func(context.Context) error { refreshes++; return nil }
	transport.client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unauthorized")), Request: req}, nil
	})
	_, err := transport.post(context.Background(), "/test", object{}, false)
	require.Error(t, err)
	require.Equal(t, 1, refreshes)
	require.Equal(t, 2, requests)
	transport.refresh = func(context.Context) error { return errors.New("refresh failed") }
	_, err = transport.post(context.Background(), "/test", object{}, false)
	require.ErrorContains(t, err, "refresh failed")
}
