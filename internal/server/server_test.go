package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

const testToken = "opcode-test-server-token-0123456789abcdef"

type serverAgent struct {
	agent.Service
	cancelled atomic.Int64
	busy      atomic.Bool
	requests  atomic.Int64
	result    chan agent.AgentEvent
	submitted context.Context
}

func (a *serverAgent) IsSessionBusy(string) bool { return a.busy.Load() }
func (a *serverAgent) Cancel(string)             { a.cancelled.Add(1); a.busy.Store(false) }
func (a *serverAgent) Submit(ctx context.Context, id, text string, _ ...message.Attachment) (<-chan agent.AgentEvent, bool, error) {
	a.requests.Add(1)
	a.submitted = ctx
	a.busy.Store(true)
	return a.result, false, nil
}

func serverFixture(t *testing.T) (*Server, *serverAgent) {
	t.Helper()
	if config.Get() == nil {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("LOCAL_ENDPOINT", "")
		for _, name := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "GROQ_API_KEY", "OPENROUTER_API_KEY", "XAI_API_KEY"} {
			t.Setenv(name, "")
		}
		viper.Reset()
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte(`{}`), 0600))
		viper.SetConfigFile(path)
		_, err := config.Load(t.TempDir(), false)
		require.NoError(t, err)
	}
	cfg := config.Get()
	previous := *cfg
	t.Cleanup(func() { *cfg = previous })
	cfg.WorkingDir = t.TempDir()
	cfg.Data.Directory = filepath.Join(cfg.WorkingDir, ".opcode")
	cfg.Runtime.Mode = "execute"
	conn, err := db.Connect()
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	q := db.New(conn)
	fake := &serverAgent{result: make(chan agent.AgentEvent, 1)}
	application := &app.App{Sessions: session.NewService(q), Messages: message.NewService(q), CoderAgent: fake, Permissions: permission.NewPermissionService(), Questions: interaction.NewQuestions(), Processes: tools.NewProcessManager(), Workflows: workflow.New(conn, cfg.WorkingDir, workflow.Execute)}
	s, err := New(context.Background(), application, conn, testToken)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close(); application.Processes.Close() })
	return s, fake
}
func api(s *Server, method, path string, body any) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func createSession(t *testing.T, s *Server) string {
	t.Helper()
	w := api(s, "POST", "/v1/sessions", map[string]string{"title": "Test"})
	require.Equal(t, 201, w.Code, w.Body.String())
	var response map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	return response["id"]
}

func TestServerRejectsAuthOriginsOversizeAndForeignSessions(t *testing.T) {
	s, _ := serverFixture(t)
	for _, authorization := range []string{"", "wrong", testToken, "Bearer wrong"} {
		r := httptest.NewRequest("GET", "/v1/health", nil)
		r.Header.Set("Authorization", authorization)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		require.Equal(t, 401, w.Code)
		require.NotContains(t, w.Body.String(), testToken)
	}
	r := httptest.NewRequest("GET", "/v1/health", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Origin", "https://evil.invalid")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	require.Equal(t, 403, w.Code)
	w = api(s, "POST", "/v1/sessions", map[string]string{"title": strings.Repeat("x", MaxBody+1)})
	require.Equal(t, 400, w.Code)
	w = api(s, "GET", "/v1/sessions/foreign", nil)
	require.Equal(t, 404, w.Code)
	foreign, err := s.app.Sessions.Create(context.Background(), "foreign")
	require.NoError(t, err)
	other := workflow.New(s.db, t.TempDir(), workflow.Execute)
	_, err = other.SetMode(context.Background(), foreign.ID, workflow.Execute)
	require.NoError(t, err)
	w = api(s, "POST", "/v1/sessions", map[string]string{"resume": foreign.ID})
	require.Equal(t, 404, w.Code)
	w = api(s, "GET", "/v1/sessions?limit=999999", nil)
	require.Equal(t, 400, w.Code)
	for _, address := range []string{"0.0.0.0:0", "localhost:0", "192.168.1.1:0", ":4096"} {
		_, err := ListenLocal(address)
		require.Error(t, err)
	}
	_, err = NewClient("https://remote.invalid", testToken)
	require.Error(t, err)
}

func TestServerReconnectDoesNotReplayAndSnapshotIsBoundedRedacted(t *testing.T) {
	s, fake := serverFixture(t)
	id := createSession(t, s)
	w := api(s, "POST", "/v1/sessions/"+id+"/prompt", map[string]string{"content": "do work", "request_id": "one-request"})
	require.Equal(t, 202, w.Code)
	w = api(s, "POST", "/v1/sessions/"+id+"/prompt", map[string]string{"content": "do work", "request_id": "one-request"})
	require.Equal(t, 202, w.Code)
	require.Contains(t, w.Body.String(), `"replayed":true`)
	w = api(s, "POST", "/v1/sessions/"+id+"/prompt", map[string]string{"content": "different work", "request_id": "one-request"})
	require.Equal(t, 409, w.Code)
	w = api(s, "POST", "/v1/sessions", map[string]string{"resume": id})
	require.Equal(t, 201, w.Code)
	require.EqualValues(t, 1, fake.requests.Load())
	require.NoError(t, fake.submitted.Err(), "disconnect/reconnect must not cancel the worker")
	_, err := s.app.Messages.Create(context.Background(), id, message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "secret " + testToken}, message.Finish{ProviderState: &message.ProviderState{Provider: "test", Data: json.RawMessage(`{"opaque":"never-export"}`)}}}})
	require.NoError(t, err)
	_, err = s.app.Messages.Create(context.Background(), id, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("x", 140000)}}})
	require.NoError(t, err)
	w = api(s, "GET", "/v1/sessions/"+id, nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), testToken)
	require.NotContains(t, w.Body.String(), "never-export")
	require.Contains(t, w.Body.String(), `"truncated":true`)
	var snapshot Snapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snapshot))
	require.True(t, snapshot.Busy)
	w = api(s, "POST", "/v1/sessions/"+id+"/cancel", map[string]any{})
	require.Equal(t, 200, w.Code)
	require.Greater(t, fake.cancelled.Load(), int64(0))
}

func TestServerPermissionOnceAndStaleReplyNeverGrantOtherSession(t *testing.T) {
	s, _ := serverFixture(t)
	id := createSession(t, s)
	other := createSession(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	decision := make(chan bool, 1)
	go func() {
		decision <- s.app.Permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: id, ToolName: "bash", Action: "execute", Path: config.WorkingDirectory(), Params: map[string]string{"command": "echo test"}})
	}()
	var snapshot Snapshot
	require.Eventually(t, func() bool {
		w := api(s, "GET", "/v1/sessions/"+id, nil)
		return json.Unmarshal(w.Body.Bytes(), &snapshot) == nil && len(snapshot.Permissions) == 1
	}, time.Second, time.Millisecond)
	pid := snapshot.Permissions[0].ID
	w := api(s, "POST", "/v1/sessions/"+other+"/permissions/"+pid, map[string]string{"decision": "once"})
	require.Equal(t, 404, w.Code)
	w = api(s, "POST", "/v1/sessions/"+id+"/permissions/"+pid, map[string]string{"decision": "once"})
	require.Equal(t, 200, w.Code)
	require.True(t, <-decision)
	w = api(s, "POST", "/v1/sessions/"+id+"/permissions/"+pid, map[string]string{"decision": "session"})
	require.Equal(t, 404, w.Code)
	go func() {
		decision <- s.app.Permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: id, ToolName: "bash", Action: "execute", Path: config.WorkingDirectory(), Params: map[string]string{"command": "echo test"}})
	}()
	require.Eventually(t, func() bool {
		w := api(s, "GET", "/v1/sessions/"+id, nil)
		return json.Unmarshal(w.Body.Bytes(), &snapshot) == nil && len(snapshot.Permissions) == 1
	}, time.Second, time.Millisecond)
	cancel()
	require.False(t, <-decision)
	w = api(s, "POST", "/v1/sessions/"+id+"/permissions/"+snapshot.Permissions[0].ID, map[string]string{"decision": "session"})
	require.Equal(t, 404, w.Code)
}

func TestServerPTYRequiresApprovalSupportsInputAndRejectsOtherOwner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY is unavailable on Windows")
	}
	s, _ := serverFixture(t)
	id := createSession(t, s)
	other := createSession(t, s)
	config.Get().Security.Sandbox = "none" // Explicit test-only policy; production keeps Bubblewrap.
	path := "/v1/sessions/" + id
	_, err := s.app.Workflows.SetMode(context.Background(), id, workflow.Ask)
	require.NoError(t, err)
	w := api(s, "POST", path+"/processes", tools.ProcessParams{Command: "echo blocked", PTY: true})
	require.Equal(t, 403, w.Code)
	require.Empty(t, s.app.Processes.List(id))
	_, err = s.app.Workflows.SetMode(context.Background(), id, workflow.Execute)
	require.NoError(t, err)
	started := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		started <- api(s, "POST", path+"/processes", tools.ProcessParams{Command: `printf 'ready:'; IFS= read -r value; printf 'done:%s\n' "$value"`, PTY: true, Timeout: 10000})
	}()
	var snapshot Snapshot
	require.Eventually(t, func() bool {
		w := api(s, "GET", path, nil)
		return json.Unmarshal(w.Body.Bytes(), &snapshot) == nil && len(snapshot.Permissions) == 1
	}, time.Second, time.Millisecond)
	require.Empty(t, s.app.Processes.List(id), "no command starts before permission")
	w = api(s, "POST", path+"/permissions/"+snapshot.Permissions[0].ID, map[string]string{"decision": "once"})
	require.Equal(t, 200, w.Code)
	w = <-started
	require.Equal(t, 201, w.Code, w.Body.String())
	var info tools.ProcessInfo
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &info))
	w = api(s, "GET", "/v1/sessions/"+other+"/processes/"+info.ID, nil)
	require.Equal(t, 404, w.Code)
	w = api(s, "POST", path+"/processes/"+info.ID+"/input", map[string]string{"data": "hello\n"})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Eventually(t, func() bool {
		w := api(s, "GET", path+"/processes/"+info.ID, nil)
		var p tools.ProcessSnapshot
		return json.Unmarshal(w.Body.Bytes(), &p) == nil && strings.Contains(p.Stdout, "done:hello") && !p.Running
	}, 3*time.Second, 10*time.Millisecond)
	// Another HTTP client attaches to the same process without starting it again.
	w = api(s, "POST", "/v1/sessions", map[string]string{"resume": id})
	require.Equal(t, 201, w.Code)
	require.Len(t, s.app.Processes.List(id), 1)
}

type clientTransport func(*http.Request) (*http.Response, error)

func (f clientTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestClientUsesHeaderBoundsAndCancellationWithoutExposingCredential(t *testing.T) {
	c, err := NewClient("http://127.0.0.1:4096", testToken)
	require.NoError(t, err)
	c.http.Transport = clientTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer "+testToken, r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.String(), testToken)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"ok"}`)), Header: http.Header{}}, nil
	})
	var response map[string]string
	require.NoError(t, c.Do(context.Background(), "GET", "/v1/health", nil, &response))
	require.Equal(t, "ok", response["status"])
	require.Error(t, c.Do(context.Background(), "POST", "/v1/sessions", map[string]string{"title": strings.Repeat("x", MaxBody+1)}, nil))
}
