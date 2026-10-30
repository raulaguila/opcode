// Package server exposes the running local application without transferring
// permission ownership to a browser, a saved session, or an agent instruction.
package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/workflow"
)

const MaxBody = 64 << 10

type Server struct {
	app         *app.App
	db          *sql.DB
	token       string
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	outcomes    map[string]string
	runIDs      map[string]uint64
	submissions map[string]map[string]submission
	allowed     map[string]bool
	workers     sync.WaitGroup
	slots       chan struct{}
	handlers    sync.WaitGroup
	closed      bool
	handler     http.Handler
}
type submission struct {
	digest [32]byte
	queued bool
}

func New(parent context.Context, application *app.App, conn *sql.DB, token string) (*Server, error) {
	if len(token) < 32 || len(token) > 512 || strings.ContainsAny(token, " \r\n\t") {
		return nil, fmt.Errorf("OPCODE_SERVER_TOKEN must contain 32–512 characters without spaces")
	}
	if application == nil || conn == nil {
		return nil, fmt.Errorf("server requires an application and database")
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Server{app: application, db: conn, token: token, ctx: ctx, cancel: cancel, outcomes: map[string]string{}, runIDs: map[string]uint64{}, submissions: map[string]map[string]submission{}, allowed: map[string]bool{}, slots: make(chan struct{}, 16)}
	config.RegisterRuntimeCredentials("local-server", token)
	application.Questions.SetInteractive(true)
	requests := application.Permissions.Subscribe(ctx)
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-requests:
				if !ok {
					return
				}
				// Requests remain owned by permission.Service. Polling uses its live
				// snapshot so cancellation never leaves a replayable approval here.
				_ = event
			}
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.health)
	mux.HandleFunc("GET /v1/sessions", s.sessions)
	mux.HandleFunc("POST /v1/sessions", s.create)
	mux.HandleFunc("GET /v1/sessions/{session}", s.snapshot)
	mux.HandleFunc("POST /v1/sessions/{session}/prompt", s.prompt)
	mux.HandleFunc("POST /v1/sessions/{session}/cancel", s.cancelSession)
	mux.HandleFunc("POST /v1/sessions/{session}/mode", s.mode)
	mux.HandleFunc("POST /v1/sessions/{session}/permissions/{id}", s.approve)
	mux.HandleFunc("POST /v1/sessions/{session}/answer", s.answer)
	mux.HandleFunc("POST /v1/sessions/{session}/processes", s.startProcess)
	mux.HandleFunc("GET /v1/sessions/{session}/processes/{id}", s.processSnapshot)
	mux.HandleFunc("POST /v1/sessions/{session}/processes/{id}/input", s.processInput)
	mux.HandleFunc("POST /v1/sessions/{session}/processes/{id}/resize", s.processResize)
	mux.HandleFunc("POST /v1/sessions/{session}/processes/{id}/stop", s.processStop)
	s.handler = s.secure(mux)
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.handler }
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	ids := make([]string, 0, len(s.allowed))
	for id := range s.allowed {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	s.cancel()
	for _, id := range ids {
		s.app.CoderAgent.Cancel(id)
		s.app.Processes.CancelSession(id)
		s.app.Permissions.RevokeSession(id)
	}
	s.handlers.Wait()
	s.workers.Wait()
	s.app.Questions.SetInteractive(false)
	config.RegisterRuntimeCredentials("local-server")
}

// ListenLocal accepts a numeric loopback address only. It cannot accidentally
// expose the control plane on a LAN interface through a hostname or wildcard.
func ListenLocal(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return nil, fmt.Errorf("server address must be a loopback IP and port (127.0.0.1:4096)")
	}
	return net.Listen("tcp", address)
}
func (s *Server) Serve(listener net.Listener) error {
	if addr, ok := listener.Addr().(*net.TCPAddr); !ok || !addr.IP.IsLoopback() {
		return fmt.Errorf("server listener must be loopback")
	}
	web := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return s.ctx }}
	stop := context.AfterFunc(s.ctx, func() { web.Close() })
	defer stop()
	err := web.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) || s.ctx.Err() != nil {
		return nil
	}
	return err
}
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			s.fail(w, 503, "server is shutting down")
			return
		}
		s.handlers.Add(1)
		s.mu.Unlock()
		defer s.handlers.Done()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", "application/json")
		if s.ctx.Err() != nil {
			s.fail(w, 503, "server is shutting down")
			return
		}
		if r.Header.Get("Origin") != "" {
			s.fail(w, 403, "browser origins are not enabled")
			return
		}
		if r.URL.RawQuery != "" && strings.Contains(strings.ToLower(r.URL.RawQuery), "token") {
			s.fail(w, 400, "credentials must use the Authorization header")
			return
		}
		candidate := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(candidate), []byte(s.token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			s.fail(w, 401, "authentication required")
			return
		}
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		default:
			s.fail(w, 429, "too many concurrent requests")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
		next.ServeHTTP(w, r)
	})
}
func (s *Server) write(w http.ResponseWriter, status int, value any) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 8<<20 {
		s.fail(w, 500, "response exceeds its limit")
		return
	}
	// Redact after JSON encoding too, including escaped forms of known keys.
	raw = []byte(tools.RedactCredentials(string(raw)))
	w.WriteHeader(status)
	w.Write(raw)
	w.Write([]byte("\n"))
}
func (s *Server) fail(w http.ResponseWriter, status int, text string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": tools.RedactCredentials(text)})
}
func (s *Server) decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		s.fail(w, 415, "Content-Type must be application/json")
		return false
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		s.fail(w, 400, "invalid or oversized JSON request")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		s.fail(w, 400, "request must contain one JSON value")
		return false
	}
	return true
}
func (s *Server) owns(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("session")
	s.mu.Lock()
	allowed := s.allowed[id]
	s.mu.Unlock()
	if !allowed {
		s.fail(w, 404, "session is not attached to this server")
		return "", false
	}
	return id, true
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	s.write(w, 200, map[string]any{"status": "ok", "workspace": config.WorkingDirectory(), "version": 1})
}

type Session struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt int64  `json:"updated_at"`
	Attached  bool   `json:"attached"`
}

func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			s.fail(w, 400, "limit must be 1–200")
			return
		}
		limit = n
	}
	// Query is bounded in SQL, and returns only sessions explicitly scoped to
	// this workspace by workflow state. Legacy sessions require local migration.
	rows, err := s.db.QueryContext(r.Context(), `SELECT s.id, substr(s.title,1,1000), s.updated_at FROM sessions s JOIN agent_workflows w ON w.session_id=s.id WHERE s.parent_session_id IS NULL AND w.root=? ORDER BY s.created_at DESC,s.id DESC LIMIT ? OFFSET ?`, config.WorkingDirectory(), limit, pageOffset(r))
	if err != nil {
		s.fail(w, 500, "cannot list workspace sessions")
		return
	}
	defer rows.Close()
	list := []Session{}
	for rows.Next() {
		var item Session
		if err := rows.Scan(&item.ID, &item.Title, &item.UpdatedAt); err != nil {
			s.fail(w, 500, "cannot read sessions")
			return
		}
		s.mu.Lock()
		item.Attached = s.allowed[item.ID]
		s.mu.Unlock()
		list = append(list, item)
	}
	if rows.Err() != nil {
		s.fail(w, 500, "cannot read sessions")
		return
	}
	s.write(w, 200, list)
}
func pageOffset(r *http.Request) int {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return min(max(offset, 0), 100000)
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Title  string `json:"title"`
		Resume string `json:"resume,omitempty"`
	}
	if !s.decode(w, r, &request) {
		return
	}
	if len(request.Title) > 1000 || len(request.Resume) > 128 {
		s.fail(w, 400, "title or session ID too long")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.allowed) >= 64 && !s.allowed[request.Resume] {
		s.fail(w, 409, "server session limit reached")
		return
	}
	id := request.Resume
	if id != "" {
		if !s.resumeSession(w, r, id) {
			return
		}
	} else {
		if strings.TrimSpace(request.Title) == "" {
			request.Title = "Sessão do servidor"
		}
		sess, err := s.app.Sessions.Create(r.Context(), request.Title)
		if err != nil {
			s.fail(w, 500, "cannot create session")
			return
		}
		id = sess.ID
		if _, err := s.app.Workflows.SetMode(r.Context(), id, workflow.Mode(config.Get().Runtime.Defaults().Mode)); err != nil {
			s.app.Sessions.Delete(r.Context(), id)
			s.fail(w, 500, "cannot initialize task")
			return
		}
	}
	s.allowed[id] = true
	s.write(w, 201, map[string]string{"id": id})
}

type Message struct {
	ID        string   `json:"id"`
	Role      string   `json:"role"`
	Text      string   `json:"text"`
	Tools     []string `json:"tools,omitempty"`
	Finished  bool     `json:"finished"`
	Truncated bool     `json:"truncated,omitempty"`
}
type Snapshot struct {
	Session     string                         `json:"session"`
	Busy        bool                           `json:"busy"`
	Outcome     string                         `json:"outcome,omitempty"`
	Messages    []Message                      `json:"messages"`
	Permissions []permission.PermissionRequest `json:"permissions"`
	Question    *interaction.Question          `json:"question,omitempty"`
	Processes   []tools.ProcessInfo            `json:"processes"`
	Task        workflow.Task                  `json:"task"`
	Queue       []agent.QueuedInstruction      `json:"queue,omitempty"`
}

func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owns(w, r)
	if !ok {
		return
	}
	// SQL does not materialize oversized binary attachments/provider state.
	rows, err := s.db.QueryContext(r.Context(), `SELECT id,role,CASE WHEN length(parts)<=131072 THEN parts ELSE '' END,finished_at IS NOT NULL FROM (SELECT id,role,parts,finished_at,created_at,rowid AS ordinal FROM messages WHERE session_id=? ORDER BY created_at DESC,rowid DESC LIMIT 50) ORDER BY created_at,ordinal`, id)
	if err != nil {
		s.fail(w, 500, "cannot read messages")
		return
	}
	result := Snapshot{Session: id, Busy: s.app.CoderAgent.IsSessionBusy(id), Messages: []Message{}, Permissions: []permission.PermissionRequest{}, Processes: s.app.Processes.List(id)}
	for rows.Next() {
		var m Message
		var raw string
		if err := rows.Scan(&m.ID, &m.Role, &raw, &m.Finished); err != nil {
			rows.Close()
			s.fail(w, 500, "cannot read messages")
			return
		}
		projectSnapshotMessage(&m, raw)
		result.Messages = append(result.Messages, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.fail(w, 500, "cannot read messages")
		return
	}
	result.Task, err = s.app.Workflows.Get(r.Context(), id)
	if err != nil {
		s.fail(w, 500, "cannot read task")
		return
	}
	if q, ok := s.app.Questions.Pending(id); ok {
		result.Question = &q
	}
	if queue, ok := s.app.CoderAgent.(agent.QueueControl); ok {
		result.Queue = queue.QueuedInstructions(id)
		for i := range result.Queue {
			result.Queue[i].Attachments = nil
		}
	}
	s.mu.Lock()
	result.Outcome = s.outcomes[id]
	s.mu.Unlock()
	if pending, ok := s.app.Permissions.(interface {
		PendingRequests(string) []permission.PermissionRequest
	}); ok {
		result.Permissions = pending.PendingRequests(id)
	}
	slices.SortFunc(result.Permissions, func(a, b permission.PermissionRequest) int { return strings.Compare(a.ID, b.ID) })
	s.write(w, 200, result)
}
func (s *Server) prompt(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owns(w, r)
	if !ok {
		return
	}
	var request struct {
		Content   string `json:"content"`
		RequestID string `json:"request_id,omitempty"`
	}
	if !s.decode(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.Content) == "" || len(request.Content) > 32000 {
		s.fail(w, 400, "content must contain 1–32000 bytes")
		return
	}
	if len(request.RequestID) > 128 || strings.ContainsAny(request.RequestID, " /\\\r\n\t") {
		s.fail(w, 400, "invalid request_id")
		return
	}
	s.mu.Lock()
	digest := sha256.Sum256([]byte(request.Content))
	if s.replaySubmission(w, id, request.RequestID, digest) {
		return
	}
	if len(s.outcomes) >= 64 && s.outcomes[id] == "" {
		s.mu.Unlock()
		s.fail(w, 409, "task limit reached")
		return
	}
	events, queued, err := s.app.CoderAgent.Submit(s.ctx, id, request.Content)
	if err != nil {
		s.mu.Unlock()
		s.fail(w, 409, err.Error())
		return
	}
	s.outcomes[id] = "running"
	s.recordSubmission(id, request.RequestID, digest, queued)
	if !queued && events != nil {
		s.runIDs[id]++
		s.workers.Add(1)
	}
	runID := s.runIDs[id]
	s.mu.Unlock()
	if queued || events == nil {
		s.write(w, 202, map[string]any{"accepted": true, "queued": queued})
		return
	}
	go s.observeSubmission(id, runID, events)
	s.write(w, 202, map[string]any{"accepted": true, "queued": queued})
}
func (s *Server) cancelSession(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owns(w, r)
	if !ok {
		return
	}
	s.app.CoderAgent.Cancel(id)
	s.app.Processes.CancelSession(id)
	s.app.Permissions.RevokeSession(id)
	s.write(w, 200, map[string]bool{"cancelled": true})
}
func (s *Server) mode(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owns(w, r)
	if !ok {
		return
	}
	var request struct {
		Mode workflow.Mode `json:"mode"`
	}
	if !s.decode(w, r, &request) {
		return
	}
	if err := s.app.CoderAgent.SetMode(r.Context(), id, request.Mode); err != nil {
		s.fail(w, 409, err.Error())
		return
	}
	s.write(w, 200, map[string]any{"mode": request.Mode})
}
func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owns(w, r)
	if !ok {
		return
	}
	var request struct {
		Decision string `json:"decision"`
	}
	if !s.decode(w, r, &request) {
		return
	}
	if request.Decision != "once" && request.Decision != "session" && request.Decision != "deny" {
		s.fail(w, 400, "decision must be once, session or deny")
		return
	}
	s.mu.Lock()
	var p permission.PermissionRequest
	exists := false
	if pending, ok := s.app.Permissions.(interface {
		PendingRequests(string) []permission.PermissionRequest
	}); ok {
		for _, item := range pending.PendingRequests(id) {
			if item.ID == r.PathValue("id") {
				p, exists = item, true
				break
			}
		}
	}
	s.mu.Unlock()
	if !exists || p.SessionID != id {
		s.fail(w, 404, "permission is no longer pending")
		return
	}
	switch request.Decision {
	case "once":
		s.app.Permissions.Grant(p)
	case "session":
		s.app.Permissions.GrantPersistant(p)
	default:
		s.app.Permissions.Deny(p)
	}
	s.write(w, 200, map[string]bool{"resolved": true})
}
func (s *Server) answer(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owns(w, r)
	if !ok {
		return
	}
	var request struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	if !s.decode(w, r, &request) {
		return
	}
	if err := s.app.Questions.Answer(id, request.ID, request.Text); err != nil {
		s.fail(w, 409, err.Error())
		return
	}
	s.write(w, 200, map[string]bool{"answered": true})
}
func (s *Server) startProcess(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owns(w, r)
	if !ok {
		return
	}
	var params tools.ProcessParams
	if !s.decode(w, r, &params) {
		return
	}
	task, err := s.app.Workflows.Get(r.Context(), id)
	if err != nil || task.Mode != workflow.Execute {
		s.fail(w, 403, "terminal execution requires Execute mode")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, id)
	info, err := s.app.Processes.Start(ctx, s.app.Permissions, params)
	if err != nil {
		s.fail(w, 403, err.Error())
		return
	}
	s.write(w, 201, info)
}
func (s *Server) processSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owns(w, r)
	if !ok {
		return
	}
	snapshot, err := s.app.Processes.Snapshot(id, r.PathValue("id"))
	if err != nil {
		s.fail(w, 404, err.Error())
		return
	}
	s.write(w, 200, snapshot)
}
func (s *Server) processInput(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owns(w, r)
	if !ok {
		return
	}
	var request struct {
		Data string `json:"data"`
	}
	if !s.decode(w, r, &request) {
		return
	}
	task, err := s.app.Workflows.Get(r.Context(), id)
	if err != nil || task.Mode != workflow.Execute {
		s.fail(w, 403, "terminal input requires Execute mode")
		return
	}
	if err := s.app.Processes.Input(id, r.PathValue("id"), request.Data); err != nil {
		s.fail(w, 409, err.Error())
		return
	}
	s.write(w, 200, map[string]bool{"sent": true})
}
func (s *Server) processResize(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owns(w, r)
	if !ok {
		return
	}
	var request struct {
		Cols int `json:"cols"`
		Rows int `json:"rows"`
	}
	if !s.decode(w, r, &request) {
		return
	}
	if err := s.app.Processes.Resize(id, r.PathValue("id"), request.Cols, request.Rows); err != nil {
		s.fail(w, 409, err.Error())
		return
	}
	s.write(w, 200, map[string]bool{"resized": true})
}
func (s *Server) processStop(w http.ResponseWriter, r *http.Request) {
	id, ok := s.owns(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.app.Processes.Stop(ctx, id, r.PathValue("id")); err != nil {
		s.fail(w, 409, err.Error())
		return
	}
	s.write(w, 200, map[string]bool{"stopped": true})
}

func projectSnapshotMessage(m *Message, raw string) {
	if raw == "" {
		m.Truncated = true
		m.Text = "[Mensagem extensa; consulte a sessão local]"
		return
	}
	var parts []struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &parts) != nil {
		return
	}
	for _, part := range parts {
		var data struct {
			Text string `json:"text"`
			Name string `json:"name"`
		}
		if json.Unmarshal(part.Data, &data) != nil {
			continue
		}
		switch part.Type {
		case "text":
			m.Text += data.Text
		case "tool_call":
			m.Tools = append(m.Tools, data.Name)
		}
	}
}

func (s *Server) replaySubmission(w http.ResponseWriter, id, requestID string, digest [32]byte) bool {
	if requestID != "" {
		if previous, ok := s.submissions[id][requestID]; ok {
			s.mu.Unlock()
			if previous.digest != digest {
				s.fail(w, 409, "request_id already used for different content")
				return true
			}
			s.write(w, 202, map[string]any{"accepted": true, "queued": previous.queued, "replayed": true})
			return true
		}
		if len(s.submissions[id]) >= 128 {
			s.mu.Unlock()
			s.fail(w, 409, "idempotency limit reached; start another session")
			return true
		}
	}
	return false
}

func (s *Server) observeSubmission(id string, runID uint64, events <-chan agent.AgentEvent) {
	defer s.workers.Done()
	select {
	case <-s.ctx.Done():
		return
	case event, open := <-events:
		if !open {
			return
		}
		outcome := event.Outcome
		if outcome == "" {
			outcome = "completed"
		}
		if event.Error != nil {
			outcome = tools.RedactCredentials(event.Error.Error())
		}
		s.mu.Lock()
		if s.runIDs[id] == runID {
			s.outcomes[id] = outcome
		}
		s.mu.Unlock()
	}
}

func (s *Server) resumeSession(w http.ResponseWriter, r *http.Request, id string) bool {
	var root string
	if err := s.db.QueryRowContext(r.Context(), "SELECT root FROM agent_workflows WHERE session_id=?", id).Scan(&root); err != nil || root != config.WorkingDirectory() {
		s.fail(w, 404, "session does not belong to this workspace")
		return false
	}
	if _, err := s.app.Sessions.Get(r.Context(), id); err != nil {
		s.fail(w, 404, "session not found")
		return false
	}
	return true
}

// recordSubmission runs while the submission lock is held.
func (s *Server) recordSubmission(id, requestID string, digest [32]byte, queued bool) {
	if requestID != "" {
		if s.submissions[id] == nil {
			s.submissions[id] = map[string]submission{}
		}
		s.submissions[id][requestID] = submission{digest, queued}
	}
}
