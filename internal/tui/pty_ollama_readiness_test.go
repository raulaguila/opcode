//go:build integration

package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Readiness has its own deadline, before starting the PTY assertion clock.
// A cold/unavailable model fails here; it never skips or retries acceptance.
func prepareLiveOllamaPTY(t *testing.T, endpoint, model string, contextWindow int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	started := time.Now()
	load, err := warmLiveOllama(ctx, endpoint, model, contextWindow)
	if err != nil {
		t.Fatalf("local model readiness failed: %v", err)
	}
	t.Logf("local model ready: elapsed=%s load=%s context=%d", time.Since(started).Round(time.Millisecond), load.Round(time.Millisecond), contextWindow)
}

func warmLiveOllama(ctx context.Context, endpoint, model string, contextWindow int) (time.Duration, error) {
	address, err := liveOllamaChatURL(endpoint)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(model) == "" || contextWindow <= 0 {
		return 0, errors.New("invalid local model readiness parameters")
	}
	body, err := json.Marshal(map[string]any{
		"model": model, "stream": false, "truncate": false, "keep_alive": "5m",
		"messages": []map[string]string{{"role": "user", "content": "Reply OK"}},
		"options":  map[string]any{"num_ctx": contextWindow, "num_predict": 4, "temperature": 0},
	})
	if err != nil {
		return 0, errors.New("cannot encode local model readiness request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return 0, errors.New("cannot create local model readiness request")
	}
	req.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, errors.New("local model readiness request failed")
	}
	defer response.Body.Close()
	return readLiveOllamaReadiness(response, model)
}

func liveOllamaChatURL(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("readiness requires an HTTP loopback endpoint without credentials or query")
	}
	host, port := u.Hostname(), u.Port()
	if host == "localhost" {
		host = "127.0.0.1"
	} // Never resolve readiness through DNS or a proxy.
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return "", errors.New("readiness requires an HTTP loopback endpoint")
	}
	u.Host = host
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/v1") + "/api/chat"
	u.RawPath = ""
	return u.String(), nil
}

func readLiveOllamaReadiness(response *http.Response, model string) (time.Duration, error) {
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("local model readiness returned HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return 0, errors.New("invalid or oversized local model readiness response")
	}
	var result struct {
		Model        string          `json:"model"`
		Done         bool            `json:"done"`
		LoadDuration time.Duration   `json:"load_duration"`
		Error        json.RawMessage `json:"error"`
		Message      struct {
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &result) != nil || (len(result.Error) > 0 && string(result.Error) != "null") || !result.Done {
		return 0, errors.New("local model readiness did not complete successfully")
	}
	if !liveOllamaModelMatches(model, result.Model) {
		return 0, errors.New("local model readiness returned another model")
	}
	if len(result.Message.ToolCalls) > 0 {
		return 0, errors.New("local model readiness returned unexpected tool calls")
	}
	if result.LoadDuration < 0 {
		return 0, errors.New("invalid local model readiness load duration")
	}
	return result.LoadDuration, nil
}

func liveOllamaModelMatches(requested, actual string) bool {
	if requested == actual {
		return true
	}
	explicitTag := strings.LastIndex(requested, ":") > strings.LastIndex(requested, "/")
	return !explicitTag && actual == requested+":latest"
}

func TestLiveOllamaReadinessRequestContract(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat" {
			t.Errorf("unexpected readiness route: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("readiness must not send credentials")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		expected := `{"model":"llama3.1:8b","stream":false,"truncate":false,"keep_alive":"5m","messages":[{"role":"user","content":"Reply OK"}],"options":{"num_ctx":16384,"num_predict":4,"temperature":0}}`
		var want map[string]any
		_ = json.Unmarshal([]byte(expected), &want)
		encoded, _ := json.Marshal(body)
		canonical, _ := json.Marshal(want)
		if string(encoded) != string(canonical) {
			t.Errorf("unexpected readiness request: %s", encoded)
		}
		_, _ = io.WriteString(w, `{"model":"llama3.1:8b","done":true,"load_duration":1000000,"message":{"content":"OK"}}`)
	}))
	defer server.Close()
	load, err := warmLiveOllama(t.Context(), server.URL+"/v1/", "llama3.1:8b", 16384)
	if err != nil || load != time.Millisecond || requests != 1 {
		t.Fatalf("readiness load=%s requests=%d err=%v", load, requests, err)
	}
}

func TestLiveOllamaReadinessRejectsBadResponses(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"malformed", `{"private":"secret-fixture"`, 200},
		{"status", `secret-fixture`, 503},
		{"no done", `{"model":"llama3.1:8b","done":false}`, 200},
		{"wrong model", `{"model":"another:8b","done":true}`, 200},
		{"explicit alias", `{"model":"llama3.1:latest","done":true}`, 200},
		{"tool call", `{"model":"llama3.1:8b","done":true,"message":{"tool_calls":[{"function":{"name":"bash"}}]}}`, 200},
		{"provider error", `{"model":"llama3.1:8b","done":true,"error":"secret-fixture"}`, 200},
		{"oversized", strings.Repeat("x", (64<<10)+1), 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			_, err := warmLiveOllama(t.Context(), server.URL, "llama3.1:8b", 16384)
			if err == nil || strings.Contains(err.Error(), "secret-fixture") || requests != 1 {
				t.Fatalf("bad response accepted, leaked or retried: requests=%d err=%v", requests, err)
			}
		})
	}
}

func TestLiveOllamaReadinessCancellationAndRedirectStayBounded(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := warmLiveOllama(ctx, server.URL, "llama3.1:8b", 16384); done <- err }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://192.0.2.1/private", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	if _, err := warmLiveOllama(t.Context(), redirect.URL, "llama3.1:8b", 16384); err == nil {
		t.Fatal("redirect accepted")
	}
}

func TestLiveOllamaReadinessRejectsTruncatedHTTPBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = io.WriteString(w, `{"model":"llama3.1:8b","done":true}`)
	}))
	defer server.Close()
	if _, err := warmLiveOllama(t.Context(), server.URL, "llama3.1:8b", 16384); err == nil {
		t.Fatal("truncated HTTP body accepted despite valid JSON prefix")
	}
}

func TestLiveOllamaReadinessEndpointAndAliasValidation(t *testing.T) {
	for _, endpoint := range []string{"https://127.0.0.1/v1", "http://192.0.2.1/v1", "http://example.com/v1", "http://key:secret@127.0.0.1/v1", "http://127.0.0.1/v1?key=secret", "http://127.0.0.1/v1#secret"} {
		if _, err := liveOllamaChatURL(endpoint); err == nil {
			t.Errorf("unsafe endpoint accepted")
		}
	}
	for _, test := range []struct{ endpoint, want string }{{"http://localhost:11434/v1", "http://127.0.0.1:11434/api/chat"}, {"http://[::1]:11434/v1/", "http://[::1]:11434/api/chat"}} {
		actual, err := liveOllamaChatURL(test.endpoint)
		if err != nil || actual != test.want {
			t.Errorf("wrong native endpoint: got %s err=%v", actual, err)
		}
	}
	if !liveOllamaModelMatches("llama3.1", "llama3.1:latest") || liveOllamaModelMatches("llama3.1:8b", "llama3.1:latest") {
		t.Fatal("incorrect alias matching")
	}
}
