// Package desktop serves the proposal's interface as a local web app: the
// page runs in an app window, the agent and its services run here. It owns
// no agent, permission or storage logic; it calls the same services the
// terminal interface does.
//
// The page is reachable only from this machine and only by the window that
// was launched with the per-run key: every API call carries the key in a
// header, the Host must be the loopback address that was bound (no DNS
// rebinding), and a request from any other origin is refused.
package desktop

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/yuin/goldmark"
)

//go:embed web
var web embed.FS

const maxBody = 64 << 10

// Server is one app window's backend.
type Server struct {
	app      *app.App
	key      string
	host     string // "127.0.0.1:port", fixed once listening
	mu       sync.Mutex
	feeds    map[chan string]struct{}
	failures map[string]string // a session's last run error, until its next prompt
}

// New creates the backend and its per-run key.
func New(application *app.App) (*Server, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	// The window can answer the agent's questions, so the agent may ask them.
	application.Questions.SetInteractive(true)
	return &Server{app: application, key: hex.EncodeToString(raw), feeds: map[chan string]struct{}{}, failures: map[string]string{}}, nil
}

// Listen binds a random loopback port and returns the URL that opens the app;
// the key travels in the fragment, which browsers never send to a server.
func (s *Server) Listen() (net.Listener, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	s.host = listener.Addr().String()
	return listener, "http://" + s.host + "/#k=" + s.key, nil
}

// Serve runs until ctx ends, relaying service events to open windows.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	go s.relay(ctx)
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	stop := context.AfterFunc(ctx, func() { srv.Close() })
	defer stop()
	if err := srv.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler serves the page and the API.
func (s *Server) Handler() http.Handler {
	static, _ := fs.Sub(web, "web")
	files := http.FileServerFS(static)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("POST /api/sessions", s.create)
	mux.HandleFunc("GET /api/sessions/{id}", s.conversation)
	mux.HandleFunc("POST /api/sessions/{id}/prompt", s.prompt)
	mux.HandleFunc("POST /api/sessions/{id}/cancel", s.cancel)
	mux.HandleFunc("POST /api/sessions/{id}/permissions/{request}", s.decide)
	mux.HandleFunc("GET /api/events", s.events)
	s.routes(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.host {
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+s.host {
			http.Error(w, "foreign origin", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			// The page loads only its own files: no network, no frames.
			w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
			files.ServeHTTP(w, r)
			return
		}
		key := r.Header.Get("X-Opcode-Key")
		if subtle.ConstantTimeCompare([]byte(key), []byte(s.key)) != 1 {
			http.Error(w, "missing key", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		mux.ServeHTTP(w, r)
	})
}

func reply(w http.ResponseWriter, status int, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "cannot encode", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(tools.RedactCredentials(string(raw))))
}

func fail(w http.ResponseWriter, status int, err error) {
	reply(w, status, map[string]string{"error": err.Error()})
}

func decode(r *http.Request, value any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(value)
}

// State is what the frame shows before a conversation: where, with what.
type State struct {
	Workspace string         `json:"workspace"`
	Branch    string         `json:"branch"`
	Model     string         `json:"model"`
	Context   int64          `json:"context"`
	Mode      string         `json:"mode"`
	Language  string         `json:"language"`
	Sessions  []SessionBrief `json:"sessions"`
}

type SessionBrief struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	UpdatedAt    int64  `json:"updated_at"`
	MessageCount int64  `json:"message_count"`
	PromptTokens int64  `json:"prompt_tokens"`
}

func brief(s session.Session) SessionBrief {
	return SessionBrief{s.ID, s.Title, s.UpdatedAt, s.MessageCount, s.PromptTokens}
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	model := s.app.CoderAgent.Model()
	name := model.APIModel
	if name == "" {
		name = string(model.ID)
	}
	state := State{Workspace: homeRelative(config.WorkingDirectory()), Branch: gitBranch(config.WorkingDirectory()), Model: name, Context: model.ContextWindow, Mode: config.Get().Runtime.Defaults().Mode, Language: config.Get().TUI.Language, Sessions: []SessionBrief{}}
	list, err := s.app.Sessions.List(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	slices.SortFunc(list, func(a, b session.Session) int { return int(b.UpdatedAt - a.UpdatedAt) })
	for _, item := range list {
		if item.ParentSessionID == "" {
			state.Sessions = append(state.Sessions, brief(item))
		}
	}
	reply(w, http.StatusOK, state)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Mode   string `json:"mode"`
		Policy string `json:"policy"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	created, err := s.app.Sessions.Create(r.Context(), config.Text("Nova conversa", "New conversation"))
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.configure(r.Context(), created.ID, request.Mode, request.Policy); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusCreated, brief(created))
}

// configure applies the composer's mode and approval policy to a session.
func (s *Server) configure(ctx context.Context, id, mode, policy string) error {
	switch mode {
	case "ask", "plan", "execute":
		if err := s.app.CoderAgent.SetMode(ctx, id, workflow.Mode(mode)); err != nil {
			return err
		}
	case "":
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
	if policy == "" {
		return nil
	}
	policies, ok := s.app.Permissions.(permission.ApprovalPolicies)
	if !ok {
		return nil
	}
	return policies.SetApprovalPolicy(id, permission.ApprovalPolicy(policy), config.WorkingDirectory())
}

// Step is one tool call as the transcript shows it.
type Step struct {
	Name    string `json:"name"`
	Target  string `json:"target"`
	State   string `json:"state"`             // ok, failed, running
	Added   int    `json:"added,omitempty"`   // lines an edit added
	Removed int    `json:"removed,omitempty"` // and removed
}

// Message is one transcript entry; assistant text arrives as rendered HTML.
type Message struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Text     string `json:"text,omitempty"`
	HTML     string `json:"html,omitempty"`
	Steps    []Step `json:"steps,omitempty"`
	Model    string `json:"model,omitempty"`
	Seconds  int64  `json:"seconds,omitempty"`
	Cites    string `json:"cites,omitempty"` // the first source the reply cites, "store.go:45"
	Finished bool   `json:"finished"`
}

// Conversation is one session's screen.
type Conversation struct {
	Session     SessionBrief  `json:"session"`
	Busy        bool          `json:"busy"`
	Messages    []Message     `json:"messages"`
	Permissions []Approval    `json:"permissions"`
	Task        workflow.Task `json:"task"`
	Failure     string        `json:"failure,omitempty"`
	Question    any           `json:"question,omitempty"`
	Number      int           `json:"number"`          // the session's place among this workspace's conversations
	Speed       float64       `json:"speed,omitempty"` // tokens per second of the last reply
}

// Approval is a pending permission with its diff or command, if any.
type Approval struct {
	ID          string `json:"id"`
	Tool        string `json:"tool"`
	Description string `json:"description"`
	File        string `json:"file,omitempty"`
	Diff        string `json:"diff,omitempty"`
	Command     string `json:"command,omitempty"`
}

func (s *Server) conversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	current, err := s.app.Sessions.Get(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	messages, err := s.app.Messages.List(r.Context(), id)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	view := Conversation{Session: brief(current), Busy: s.app.CoderAgent.IsSessionBusy(id), Messages: project(messages), Permissions: []Approval{}}
	view.Task, _ = s.app.Workflows.Get(r.Context(), id)
	if q, ok := s.app.Questions.Pending(id); ok {
		view.Question = q
	}
	s.mu.Lock()
	view.Failure = s.failures[id]
	s.mu.Unlock()
	for _, request := range s.pending(id) {
		view.Permissions = append(view.Permissions, approval(request))
	}
	view.Number = s.number(r.Context(), current)
	if usage, ok := s.app.CoderAgent.(interface {
		Usage(context.Context, string) (agent.UsageMetrics, error)
	}); ok {
		if metrics, err := usage.Usage(r.Context(), id); err == nil && metrics.LastOutputTokens > 0 && metrics.LastOutputDurationNS > 0 {
			view.Speed = float64(metrics.LastOutputTokens) / (float64(metrics.LastOutputDurationNS) / float64(time.Second))
		}
	}
	reply(w, http.StatusOK, view)
}

func (s *Server) pending(id string) []permission.PermissionRequest {
	if pending, ok := s.app.Permissions.(interface {
		PendingRequests(string) []permission.PermissionRequest
	}); ok {
		return pending.PendingRequests(id)
	}
	return nil
}

func approval(request permission.PermissionRequest) Approval {
	a := Approval{ID: request.ID, Tool: request.ToolName, Description: request.Description}
	switch params := request.Params.(type) {
	case tools.EditPermissionsParams:
		a.File, a.Diff = relative(params.FilePath), params.Diff
	case tools.WritePermissionsParams:
		a.File, a.Diff = relative(params.FilePath), params.Diff
	case tools.BashPermissionsParams:
		a.Command = params.Command
	}
	if a.File != "" {
		a.Description = strings.TrimSpace(strings.ReplaceAll(a.Description, filepath.Join(config.WorkingDirectory(), a.File), ""))
	}
	return a
}

// project turns stored messages into transcript entries: your prompts, the
// steps between them and the model's replies, rendered.
func project(messages []message.Message) []Message {
	results := map[string]message.ToolResult{}
	for _, msg := range messages {
		for _, result := range msg.ToolResults() {
			results[result.ToolCallID] = result
		}
	}
	renderer := goldmark.New()
	out := []Message{}
	for _, msg := range messages {
		if msg.IsApplication() || msg.Role == message.Tool {
			continue
		}
		text := strings.TrimSpace(msg.Content().String())
		entry := Message{ID: msg.ID, Role: string(msg.Role), Finished: msg.IsFinished()}
		switch msg.Role {
		case message.User:
			entry.Text = text
		case message.Assistant:
			if text != "" {
				var html bytes.Buffer
				// Goldmark leaves raw HTML out unless asked, so model text cannot inject markup.
				if renderer.Convert([]byte(text), &html) == nil {
					entry.HTML = html.String()
				}
			}
			entry.Model = string(msg.Model)
			if finish := msg.FinishPart(); finish != nil && msg.IsFinished() && finish.Time > msg.CreatedAt {
				entry.Seconds = finish.Time - msg.CreatedAt
			}
			if cites := agent.SourceCitations(text); len(cites) > 0 && cites[0].StartLine > 0 {
				entry.Cites = fmt.Sprintf("%s:%d", filepath.Base(cites[0].Path), cites[0].StartLine)
			}
			for _, call := range msg.ToolCalls() {
				step := Step{Name: call.Name, Target: target(call.Input), State: "ok"}
				if result, done := results[call.ID]; !done {
					step.State = "running"
				} else if result.IsError {
					step.State = "failed"
				} else {
					// Edits report their line counts in the result's metadata.
					var lines struct {
						Additions int `json:"additions"`
						Removals  int `json:"removals"`
					}
					if json.Unmarshal([]byte(result.Metadata), &lines) == nil {
						step.Added, step.Removed = lines.Additions, lines.Removals
					}
				}
				entry.Steps = append(entry.Steps, step)
			}
		}
		if entry.Text != "" || entry.HTML != "" || len(entry.Steps) > 0 {
			out = append(out, entry)
		}
	}
	return out
}

// target is a call's main argument: a file name, a command, a pattern.
func target(input string) string {
	var fields map[string]any
	_ = json.Unmarshal([]byte(input), &fields)
	for _, name := range []string{"file_path", "path", "command", "pattern", "query", "url"} {
		if value, ok := fields[name].(string); ok && strings.TrimSpace(value) != "" {
			if name == "file_path" || name == "path" {
				return filepath.Base(value)
			}
			return strings.Join(strings.Fields(value), " ")
		}
	}
	return ""
}

func (s *Server) prompt(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Text   string `json:"text"`
		Mode   string `json:"mode"`
		Policy string `json:"policy"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	text := strings.TrimSpace(request.Text)
	if text == "" || len(text) > 32000 {
		fail(w, http.StatusBadRequest, errors.New("message must have 1–32000 bytes"))
		return
	}
	id := r.PathValue("id")
	if err := s.configure(r.Context(), id, request.Mode, request.Policy); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	delete(s.failures, id)
	s.mu.Unlock()
	// The run outlives this request; its progress reaches the window as events.
	if _, _, err := s.app.CoderAgent.Submit(context.WithoutCancel(r.Context()), id, text); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	reply(w, http.StatusAccepted, map[string]bool{"queued": true})
}

func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	s.app.CoderAgent.Cancel(r.PathValue("id"))
	reply(w, http.StatusOK, map[string]bool{"cancelled": true})
}

// decide answers a pending approval: once, for this scope in the session,
// or deny. Only a request still pending for this session can be answered.
func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Decision string `json:"decision"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	id, requestID := r.PathValue("id"), r.PathValue("request")
	index := slices.IndexFunc(s.pending(id), func(p permission.PermissionRequest) bool { return p.ID == requestID })
	if index < 0 {
		fail(w, http.StatusNotFound, errors.New("no such pending approval"))
		return
	}
	pending := s.pending(id)[index]
	switch request.Decision {
	case "once":
		s.app.Permissions.Grant(pending)
	case "session":
		s.app.Permissions.GrantPersistant(pending)
	case "deny":
		s.app.Permissions.Deny(pending)
	default:
		fail(w, http.StatusBadRequest, errors.New("decision must be once, session or deny"))
		return
	}
	reply(w, http.StatusOK, map[string]string{"decision": request.Decision})
}

// events streams the IDs of sessions that changed; the window reloads them.
// A fetch stream, not EventSource, so the key can travel in a header.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	feed := make(chan string, 64)
	s.mu.Lock()
	s.feeds[feed] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.feeds, feed)
		s.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	beat := time.NewTicker(15 * time.Second)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case id := <-feed:
			fmt.Fprintf(w, "data: %s\n\n", id)
		case <-beat.C:
			fmt.Fprint(w, ": beat\n\n")
		}
		flusher.Flush()
	}
}

// relay forwards service events to every open stream as a session ID.
func (s *Server) relay(ctx context.Context) {
	messages := s.app.Messages.Subscribe(ctx)
	sessions := s.app.Sessions.Subscribe(ctx)
	permissions := s.app.Permissions.Subscribe(ctx)
	agents := s.app.CoderAgent.Subscribe(ctx)
	for {
		var id string
		select {
		case <-ctx.Done():
			return
		case event, ok := <-messages:
			if !ok {
				return
			}
			id = event.Payload.SessionID
		case event, ok := <-sessions:
			if !ok {
				return
			}
			id = event.Payload.ID
		case event, ok := <-permissions:
			if !ok {
				return
			}
			id = event.Payload.SessionID
		case event, ok := <-agents:
			if !ok {
				return
			}
			id = event.Payload.SessionID
			if id == "" {
				id = event.Payload.Message.SessionID
			}
			if err := event.Payload.Error; err != nil && !errors.Is(err, agent.ErrRequestCancelled) && id != "" {
				s.mu.Lock()
				s.failures[id] = tools.RedactCredentials(strings.Join(strings.Fields(err.Error()), " "))
				s.mu.Unlock()
			}
		}
		s.mu.Lock()
		for feed := range s.feeds {
			select {
			case feed <- id:
			default: // a slow window catches up on its next reload
			}
		}
		s.mu.Unlock()
	}
}

func relative(path string) string {
	if rel, err := filepath.Rel(config.WorkingDirectory(), path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

func homeRelative(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// gitBranch reads .git/HEAD, following a worktree's gitdir file.
func gitBranch(dir string) string {
	git := filepath.Join(dir, ".git")
	if raw, err := os.ReadFile(git); err == nil {
		target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
		if !ok {
			return ""
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		git = target
	}
	head, err := os.ReadFile(filepath.Join(git, "HEAD"))
	if err != nil {
		return ""
	}
	branch, _ := strings.CutPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/")
	return branch
}

// number is a conversation's place, oldest first, among the workspace's
// conversations: "sessão 3".
func (s *Server) number(ctx context.Context, current session.Session) int {
	list, err := s.app.Sessions.List(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, item := range list {
		if item.ParentSessionID == "" && (item.CreatedAt < current.CreatedAt || item.ID == current.ID) {
			n++
		}
	}
	return n
}
