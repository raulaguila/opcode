package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/stretchr/testify/require"
)

type routeSessions struct {
	session.Service
	saved []session.Session
}

func (s *routeSessions) Get(_ context.Context, id string) (session.Session, error) {
	return session.Session{ID: id, Title: "old"}, nil
}
func (s *routeSessions) Save(_ context.Context, current session.Session) (session.Session, error) {
	s.saved = append(s.saved, current)
	return current, nil
}

type routeAgent struct {
	agent.Service
	busy      bool
	compacted int
}

func (a *routeAgent) IsSessionBusy(string) bool { return a.busy }
func (a *routeAgent) ContextStatus(context.Context, string) (agent.ContextStatus, error) {
	return agent.ContextStatus{ContextWindow: 1000, EstimatedInputTokens: 900}, nil
}
func (a *routeAgent) CompactContext(context.Context, string) (agent.ContextStatus, error) {
	a.compacted++
	return agent.ContextStatus{ContextWindow: 1000, EstimatedInputTokens: 300, CompactedMessages: 4, Fits: true}, nil
}

func route(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+s.host+path, strings.NewReader(body))
	r.Host = s.host
	r.Header.Set("X-Opcode-Key", s.key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestManagingRoutesRefuseARunningConversation(t *testing.T) {
	sessions, coder := &routeSessions{}, &routeAgent{busy: true}
	s := &Server{app: &app.App{Sessions: sessions, CoderAgent: coder}, key: "k", host: "127.0.0.1:4000", feeds: map[chan string]struct{}{}, failures: map[string]string{}}
	require.Equal(t, http.StatusConflict, route(s, http.MethodPost, "/api/sessions/s/rename", `{"title":"new"}`).Code)
	require.Equal(t, http.StatusConflict, route(s, http.MethodPost, "/api/sessions/s/context", `{}`).Code)
	require.Empty(t, sessions.saved)
	require.Zero(t, coder.compacted)

	coder.busy = false
	require.Equal(t, http.StatusBadRequest, route(s, http.MethodPost, "/api/sessions/s/rename", `{"title":"  "}`).Code, "a blank title is refused")
	require.Equal(t, http.StatusBadRequest, route(s, http.MethodPost, "/api/sessions/s/rename", `{"title":"x","extra":1}`).Code, "unknown fields are refused")
	require.Equal(t, http.StatusOK, route(s, http.MethodPost, "/api/sessions/s/rename", `{"title":" new "}`).Code)
	require.Equal(t, "new", sessions.saved[0].Title)
}

func TestContextRouteMeasuresAndCompacts(t *testing.T) {
	coder := &routeAgent{}
	s := &Server{app: &app.App{Sessions: &routeSessions{}, CoderAgent: coder}, key: "k", host: "127.0.0.1:4000", feeds: map[chan string]struct{}{}, failures: map[string]string{}}
	var status agent.ContextStatus
	require.NoError(t, json.Unmarshal(route(s, http.MethodGet, "/api/sessions/s/context", "").Body.Bytes(), &status))
	require.Equal(t, int64(900), status.EstimatedInputTokens)
	require.Zero(t, coder.compacted, "reading does not compact")
	require.NoError(t, json.Unmarshal(route(s, http.MethodPost, "/api/sessions/s/context", `{}`).Body.Bytes(), &status))
	require.Equal(t, 4, status.CompactedMessages)
	require.Equal(t, 1, coder.compacted)
}

func TestStreamingRepliesAreNotCached(t *testing.T) {
	rendered.Lock()
	clear(rendered.replies)
	rendered.Unlock()
	renderReply("part", false)
	renderReply("whole reply", true)
	rendered.Lock()
	defer rendered.Unlock()
	require.NotContains(t, rendered.replies, "part")
	require.Contains(t, rendered.replies, "whole reply")
}
