package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/fileutil"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/transcript"
)

// routes adds the endpoints behind the dialogs: models, conversations,
// answers, preferences and connections.
func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/models", s.models)
	mux.HandleFunc("POST /api/model", s.selectModel)
	mux.HandleFunc("POST /api/model/context", s.setContext)
	mux.HandleFunc("POST /api/model/reasoning", s.setReasoning)
	mux.HandleFunc("GET /api/files", s.files)
	mux.HandleFunc("GET /api/conversations", s.conversations)
	mux.HandleFunc("GET /api/sessions/{id}/preview", s.preview)
	mux.HandleFunc("POST /api/sessions/{id}/rename", s.rename)
	mux.HandleFunc("POST /api/sessions/{id}/archive", s.archive)
	mux.HandleFunc("POST /api/sessions/{id}/export", s.export)
	mux.HandleFunc("POST /api/sessions/{id}/answer", s.answer)
	mux.HandleFunc("GET /api/settings", s.settings)
	mux.HandleFunc("POST /api/settings", s.saveSetting)
	mux.HandleFunc("GET /api/connections", s.connections)
	mux.HandleFunc("POST /api/connections/test", s.testConnection)
	mux.HandleFunc("POST /api/connections/save", s.saveConnection)
	mux.HandleFunc("POST /api/connections/delete", s.deleteConnection)
	mux.HandleFunc("POST /api/connections/enabled", s.enableConnection)
}

// guard refuses configuration changes while a run is going.
func (s *Server) guard() error {
	if s.app.CoderAgent.IsBusy() {
		return errors.New(config.Text("aguarde ou cancele a execução antes de alterar configurações", "wait for or cancel the run before changing settings"))
	}
	return nil
}

// ---- models

type provider struct {
	ID   models.ModelProvider `json:"id"`
	Name string               `json:"name"`
	Host string               `json:"host"`
}

// enabledProviders lists the connections that can offer models, most
// popular backends first.
func enabledProviders() []provider {
	cfg := config.Get()
	var out []provider
	for id, p := range cfg.ProviderProfiles {
		if !p.Disabled {
			name := p.Name
			if name == "" {
				name = id
			}
			out = append(out, provider{models.ModelProvider("profile:" + id), name, host(p.BaseURL)})
		}
	}
	for id, p := range cfg.Providers {
		if !p.Disabled {
			out = append(out, provider{id, backendName(id), host(p.BaseURL)})
		}
	}
	slices.SortFunc(out, func(a, b provider) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func backendName(id models.ModelProvider) string {
	switch id {
	case models.ProviderLocal:
		if config.Get().Providers[id].API == "ollama" {
			return "Ollama"
		}
		return config.Text("Servidor local", "Local server")
	case models.ProviderOpenAI:
		return "OpenAI"
	case models.ProviderAnthropic:
		return "Anthropic"
	case models.ProviderGemini:
		return "Gemini"
	case models.ProviderOpenRouter:
		return "OpenRouter"
	}
	return string(id)
}

func host(base string) string {
	if u, err := url.Parse(base); err == nil {
		return u.Host
	}
	return ""
}

// ModelInfo is one catalog entry as the picker and its details show it.
type ModelInfo struct {
	ID        models.ModelID       `json:"id"`
	Provider  models.ModelProvider `json:"provider"`
	Label     string               `json:"label"`
	Context   int64                `json:"context"`
	Estimated bool                 `json:"estimated"`
	Output    int64                `json:"output"`
	Tools     bool                 `json:"tools"`
	Images    bool                 `json:"images"`
	Reasoning bool                 `json:"reasoning"`
	Embedding bool                 `json:"embedding"`
	Cloud     bool                 `json:"cloud"`
	API       string               `json:"api"`
}

func info(m models.Model) ModelInfo {
	label := m.APIModel
	if label == "" {
		label = string(m.ID)
	}
	name := strings.ToLower(m.APIModel)
	return ModelInfo{m.ID, m.Provider, label, m.ContextWindow, m.ContextEstimated, m.DefaultMaxTokens,
		m.SupportsTools != nil && *m.SupportsTools, m.SupportsAttachments, m.CanReason, m.EmbeddingOnly(),
		m.Hosting == "cloud" || strings.HasSuffix(name, ":cloud") || strings.HasSuffix(name, "-cloud"), m.API}
}

func catalog(ctx context.Context, p models.ModelProvider) ([]models.Model, error) {
	return fetchCatalog(ctx, p, false)
}

// fetchCatalog lists a connection's models; with metadata, each local model
// also reports its tools, images and context.
func fetchCatalog(ctx context.Context, p models.ModelProvider, metadata bool) ([]models.Model, error) {
	fetch := config.ModelCatalog(p)
	if id, ok := strings.CutPrefix(string(p), "profile:"); ok {
		fetch = config.ProfileModelCatalog(id)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if !metadata {
		ctx = models.WithoutCatalogMetadata(ctx)
	}
	return fetch(ctx)
}

// Loaded is where a local Ollama holds a model now.
type Loaded struct {
	Placement string `json:"placement"` // GPU, CPU, CPU/GPU
	Bytes     int64  `json:"bytes"`
	Context   int64  `json:"context"`
}

// loaded asks a local Ollama connection which models it holds in memory.
func loaded(ctx context.Context, p models.ModelProvider) map[string]Loaded {
	base, key, ok := config.OllamaConnection(p)
	if !ok {
		return nil
	}
	running, err := models.InspectOllama(ctx, base, key)
	if err != nil {
		return nil
	}
	out := map[string]Loaded{}
	for _, m := range running {
		out[m.Name] = Loaded{m.Placement(), m.Size, m.Context}
	}
	return out
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	providers := enabledProviders()
	active := s.app.CoderAgent.Model()
	chosen := models.ModelProvider(r.URL.Query().Get("provider"))
	if chosen == "" {
		chosen = active.Provider
	}
	if !slices.ContainsFunc(providers, func(p provider) bool { return p.ID == chosen }) && len(providers) > 0 {
		chosen = providers[0].ID
	}
	result := map[string]any{"providers": providers, "provider": chosen, "active": active.ID, "models": []ModelInfo{}}
	if chosen == "" {
		reply(w, http.StatusOK, result)
		return
	}
	found, err := fetchCatalog(r.Context(), chosen, true)
	if err != nil {
		// Metadata is a nicety; the plain list is enough to choose.
		found, err = catalog(r.Context(), chosen)
	}
	if err != nil {
		result["error"] = tools.RedactCredentials(err.Error())
	}
	result["loaded"] = loaded(r.Context(), chosen)
	// The active model's knobs: its reasoning effort and the context it asks for.
	current := config.ModelWithProviderDefaults(active)
	effort := ""
	if agentConfig, ok := config.Get().Agents[config.AgentCoder]; ok {
		effort = agentConfig.ReasoningEffort
	}
	result["reasoning"] = map[string]any{"choices": current.ReasoningChoices(), "current": effort}
	result["context"] = map[string]any{"window": current.ContextWindow, "auto": current.UsesAutomaticContext()}
	list := []ModelInfo{}
	for _, m := range found {
		list = append(list, info(m))
	}
	result["models"] = list
	reply(w, http.StatusOK, result)
}

func (s *Server) selectModel(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Provider models.ModelProvider `json:"provider"`
		ID       models.ModelID       `json:"id"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	found, err := catalog(r.Context(), request.Provider)
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	index := slices.IndexFunc(found, func(m models.Model) bool { return m.ID == request.ID })
	if index < 0 || found[index].EmbeddingOnly() {
		fail(w, http.StatusNotFound, errors.New(config.Text("modelo indisponível para conversa", "model not available for chat")))
		return
	}
	selected, err := s.app.CoderAgent.SelectModel(config.AgentCoder, found[index])
	if err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	reply(w, http.StatusOK, info(selected))
}

// ---- conversations

type ConversationItem struct {
	SessionBrief
	Archived bool    `json:"archived"`
	Busy     bool    `json:"busy"`
	Failed   bool    `json:"failed"`   // its last run stopped on an error
	Planning bool    `json:"planning"` // its plan has steps left
	Cost     float64 `json:"cost"`
}

func catalogOf() *session.Catalog {
	cfg := config.Get()
	return session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory)
}

func (s *Server) conversations(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.Sessions.List(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	archived := map[string]bool{}
	if snapshot, _, err := catalogOf().Snapshot(); err == nil {
		for id, state := range snapshot {
			archived[id] = state.Archived
		}
	}
	slices.SortFunc(list, func(a, b session.Session) int { return int(b.UpdatedAt - a.UpdatedAt) })
	out := []ConversationItem{}
	for _, item := range list {
		if item.ParentSessionID != "" {
			continue
		}
		entry := ConversationItem{SessionBrief: brief(item), Archived: archived[item.ID], Busy: s.app.CoderAgent.IsSessionBusy(item.ID), Cost: item.Cost}
		s.mu.Lock()
		entry.Failed = s.failures[item.ID] != ""
		s.mu.Unlock()
		if task, err := s.app.Workflows.Get(r.Context(), item.ID); err == nil {
			for _, step := range task.Steps {
				entry.Planning = entry.Planning || step.Status != "completed"
			}
		}
		out = append(out, entry)
	}
	reply(w, http.StatusOK, out)
}

// preview is a conversation's last prompt from you and the reply to it.
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	messages, err := s.app.Messages.List(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	var prompt, answer, model string
	turn := []message.Message{}
	for i := len(messages) - 1; i >= 0 && prompt == ""; i-- {
		text := strings.TrimSpace(messages[i].Content().String())
		switch {
		case messages[i].Role == message.User && !messages[i].IsApplication():
			prompt = text
		case messages[i].Role == message.Assistant:
			if answer == "" && text != "" {
				answer = text
			}
			if model == "" && messages[i].Model != "" {
				model = string(messages[i].Model)
			}
		}
		turn = append([]message.Message{messages[i]}, turn...)
	}
	var steps []Step
	for _, entry := range project(turn) {
		steps = append(steps, entry.Steps...)
	}
	result := map[string]any{"prompt": prompt, "reply": answer, "steps": steps}
	if _, label, ok := strings.Cut(model, "."); ok {
		model = label
	}
	result["model"] = model
	if task, err := s.app.Workflows.Get(r.Context(), r.PathValue("id")); err == nil {
		done := 0
		for _, step := range task.Steps {
			if step.Status == "completed" {
				done++
			}
		}
		result["mode"], result["plan_done"], result["plan_total"] = task.Mode, done, len(task.Steps)
	}
	reply(w, http.StatusOK, result)
}

// manageable refuses changes to a conversation that is still running.
func (s *Server) manageable(w http.ResponseWriter, r *http.Request) (session.Session, bool) {
	current, err := s.app.Sessions.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return current, false
	}
	if s.app.CoderAgent.IsSessionBusy(current.ID) {
		fail(w, http.StatusConflict, errors.New(config.Text("aguarde ou cancele esta conversa antes de gerenciá-la", "wait for or cancel this conversation before managing it")))
		return current, false
	}
	return current, true
}

func (s *Server) rename(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Title string `json:"title"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	current, ok := s.manageable(w, r)
	if !ok {
		return
	}
	current.Title = strings.TrimSpace(request.Title)
	if current.Title == "" || len(current.Title) > 200 {
		fail(w, http.StatusBadRequest, errors.New(config.Text("use um título entre 1 e 200 caracteres", "use a title between 1 and 200 characters")))
		return
	}
	saved, err := s.app.Sessions.Save(r.Context(), current)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, http.StatusOK, brief(saved))
}

func (s *Server) archive(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Archived bool `json:"archived"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	current, ok := s.manageable(w, r)
	if !ok {
		return
	}
	if err := catalogOf().Archive(current.ID, request.Archived); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, http.StatusOK, map[string]bool{"archived": request.Archived})
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	current, ok := s.manageable(w, r)
	if !ok {
		return
	}
	messages, err := s.app.Messages.List(r.Context(), current.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	cfg := config.Get()
	path, err := transcript.Save(cfg.Data.Directory, cfg.WorkingDir, current, messages, tools.RedactCredentials)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"path": fileutil.HomeRelative(path)})
}

// answer replies to the agent's pending question: one text, or one item
// per question of a batch.
func (s *Server) answer(w http.ResponseWriter, r *http.Request) {
	var request struct {
		QuestionID string                   `json:"question_id"`
		Text       string                   `json:"text"`
		Answers    []interaction.AnswerItem `json:"answers"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	id := r.PathValue("id")
	var err error
	if len(request.Answers) > 0 {
		err = s.app.Questions.AnswerBatch(id, request.QuestionID, request.Answers)
	} else {
		err = s.app.Questions.Answer(id, request.QuestionID, request.Text)
	}
	if err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	reply(w, http.StatusOK, map[string]bool{"answered": true})
}

// ---- preferences

type Setting struct {
	Path     string `json:"path"`
	Label    string `json:"label"`
	Category string `json:"category"`
	Advanced bool   `json:"advanced"`
	Help     string `json:"help"`
	Value    string `json:"value"`
	Origin   string `json:"origin"`
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	values, origins := map[string]string{}, map[string]string{}
	for _, v := range config.EffectiveConfiguration() {
		values[v.Path], origins[v.Path] = fmt.Sprint(v.Value), v.Origin
	}
	out := []Setting{}
	for _, item := range config.UserSettings {
		label := item.Label
		if pt, en, ok := strings.Cut(label, " / "); ok {
			label = config.Text(pt, en)
		}
		out = append(out, Setting{item.Path, label, item.Category, item.Advanced, item.Help, values[item.Path], origins[item.Path]})
	}
	global, _ := config.Paths(config.WorkingDirectory())
	reply(w, http.StatusOK, map[string]any{"path": fileutil.HomeRelative(global), "settings": out})
}

func (s *Server) saveSetting(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path  string `json:"path"`
		Value string `json:"value"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := config.SaveUserSetting(request.Path, request.Value, false); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusOK, map[string]bool{"saved": true})
}

// ---- connections

// Connection is one configured provider as its card shows it. Keys never
// leave the backend: only how the key is stored is reported.
type Connection struct {
	Provider   models.ModelProvider `json:"provider"`
	ID         string               `json:"id"`
	Legacy     bool                 `json:"legacy"`
	Name       string               `json:"name"`
	Backend    models.ModelProvider `json:"backend"`
	URL        string               `json:"url"`
	API        string               `json:"api"`
	Auth       string               `json:"auth"` // none, api_key, environment, vault
	Env        string               `json:"env"`
	Disabled   bool                 `json:"disabled"`
	KeyMissing bool                 `json:"key_missing"`
	Context    int64                `json:"context"`
	Output     int64                `json:"output"`
	TLS        bool                 `json:"tls_insecure"`
}

func connection(id string, legacy bool, name string, backend models.ModelProvider, raw config.Provider) Connection {
	c := Connection{ID: id, Legacy: legacy, Name: name, Backend: backend, URL: raw.BaseURL, API: raw.API, Env: raw.APIKeyEnv, Disabled: raw.Disabled, Context: raw.ContextWindow, Output: raw.DefaultMaxTokens, TLS: raw.TLSInsecure, Auth: "none"}
	c.Provider = models.ModelProvider("profile:" + id)
	if legacy {
		c.Provider = backend
	}
	switch {
	case raw.APIKeyRef != "":
		c.Auth = "vault"
	case raw.APIKeyEnv != "":
		c.Auth = "environment"
		// Loaded off because its variable is unset, or on with the variable since removed.
		c.KeyMissing = config.KeyMissing(c.Provider) || (!raw.Disabled && os.Getenv(raw.APIKeyEnv) == "" && raw.APIKey == "")
		c.Disabled = raw.Disabled && !c.KeyMissing
	case config.StoredProviderKey(id, legacy) != "":
		c.Auth = "api_key"
	}
	return c
}

func (s *Server) connections(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	out := []Connection{}
	for id, raw := range cfg.Providers {
		out = append(out, connection(string(id), true, backendName(id), id, raw))
	}
	for id, profile := range cfg.ProviderProfiles {
		name := profile.Name
		if name == "" {
			name = id
		}
		out = append(out, connection(id, false, name, profile.Backend, profile.Provider))
	}
	active := s.app.CoderAgent.Model().Provider
	slices.SortFunc(out, func(a, b Connection) int {
		if (a.Provider == active) != (b.Provider == active) {
			if a.Provider == active {
				return -1
			}
			return 1
		}
		if a.Disabled != b.Disabled {
			if a.Disabled {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Name, b.Name)
	})
	presets := []map[string]any{}
	for _, p := range config.ProviderPresets {
		presets = append(presets, map[string]any{"id": p.ID, "name": p.Profile.Name, "backend": p.Profile.Backend, "url": p.Profile.BaseURL, "api": p.Profile.API, "env": p.Profile.APIKeyEnv, "output": p.Profile.DefaultMaxTokens})
	}
	integrations := []string{}
	for name := range cfg.LSP {
		integrations = append(integrations, name+" · LSP")
	}
	for name := range cfg.MCPServers {
		integrations = append(integrations, name+" · MCP")
	}
	slices.Sort(integrations)
	reply(w, http.StatusOK, map[string]any{"active": active, "items": out, "presets": presets, "integrations": integrations})
}

// form is the connection editor's content.
type form struct {
	OriginalID string               `json:"original_id"`
	Legacy     bool                 `json:"legacy"`
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Backend    models.ModelProvider `json:"backend"`
	URL        string               `json:"url"`
	API        string               `json:"api"`
	Auth       string               `json:"auth"`
	Key        string               `json:"key"`
	Env        string               `json:"env"`
	Enabled    bool                 `json:"enabled"`
	Context    string               `json:"context"`
	Output     string               `json:"output"`
	TLS        bool                 `json:"tls_insecure"`
}

// profile reads the form; an untouched key keeps the one already stored.
func (f form) profile() (string, config.ProviderProfile, error) {
	id := strings.TrimSpace(f.ID)
	if f.OriginalID != "" && id != f.OriginalID {
		return "", config.ProviderProfile{}, errors.New(config.Text("o ID da conexão não pode ser alterado; adicione outra conexão", "a connection's ID cannot change; add another connection"))
	}
	if id == "" {
		return "", config.ProviderProfile{}, errors.New(config.Text("informe um ID para a conexão", "enter an ID for the connection"))
	}
	number := func(raw, what string) (int64, error) {
		if strings.TrimSpace(raw) == "" {
			return 0, nil
		}
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || n < 0 {
			return 0, fmt.Errorf(config.Text("%s deve ser um inteiro não negativo", "%s must be a nonnegative integer"), what)
		}
		return n, nil
	}
	context, err := number(f.Context, config.Text("a janela de contexto", "the context window"))
	if err != nil {
		return "", config.ProviderProfile{}, err
	}
	output, err := number(f.Output, config.Text("os tokens de saída", "the output tokens"))
	if err != nil {
		return "", config.ProviderProfile{}, err
	}
	p := config.ProviderProfile{Name: strings.TrimSpace(f.Name), Backend: f.Backend, Provider: config.Provider{BaseURL: strings.TrimSpace(f.URL), API: f.API, Disabled: !f.Enabled, ContextWindow: context, DefaultMaxTokens: output, TLSInsecure: f.TLS}}
	switch f.Auth {
	case "none":
	case "api_key":
		p.APIKey = strings.TrimSpace(f.Key)
		if p.APIKey == "" && f.OriginalID != "" {
			p.APIKey = config.StoredProviderKey(id, f.Legacy)
		}
		if p.APIKey == "" && f.Enabled {
			return "", p, errors.New(config.Text("informe a chave da API ou escolha outra autenticação", "enter the API key or choose another authentication"))
		}
	case "environment":
		p.APIKeyEnv = strings.TrimSpace(f.Env)
		if p.APIKeyEnv == "" && f.Enabled {
			return "", p, errors.New(config.Text("informe o nome da variável de ambiente", "enter the environment variable name"))
		}
	case "vault":
		if f.OriginalID != "" {
			if f.Legacy {
				p.APIKeyRef = config.Get().Providers[f.Backend].APIKeyRef
			} else {
				p.APIKeyRef = config.Get().ProviderProfiles[id].APIKeyRef
			}
		}
		if p.APIKeyRef == "" && strings.TrimSpace(f.Key) == "" {
			return "", p, errors.New(config.Text("informe a chave para o cofre do sistema", "enter the key for the system vault"))
		}
	default:
		return "", p, errors.New(config.Text("autenticação inválida", "invalid authentication"))
	}
	return id, p, nil
}

func (s *Server) testConnection(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Provider models.ModelProvider `json:"provider"`
		Form     *form                `json:"form"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	start := time.Now()
	var found []models.Model
	var err error
	if request.Form != nil {
		id, profile, ferr := request.Form.profile()
		if ferr != nil {
			fail(w, http.StatusBadRequest, ferr)
			return
		}
		if request.Form.Auth == "vault" && strings.TrimSpace(request.Form.Key) != "" {
			profile.APIKeyRef, profile.APIKey = "", strings.TrimSpace(request.Form.Key)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		found, err = config.TestProviderProfile(id, profile)(models.WithoutCatalogMetadata(ctx))
	} else {
		found, err = catalog(r.Context(), request.Provider)
	}
	if err != nil {
		reply(w, http.StatusOK, map[string]any{"ok": false, "error": tools.RedactCredentials(err.Error())})
		return
	}
	chat := 0
	for _, m := range found {
		if !m.EmbeddingOnly() {
			chat++
		}
	}
	reply(w, http.StatusOK, map[string]any{"ok": true, "models": len(found), "chat": chat, "ms": time.Since(start).Milliseconds()})
}

func (s *Server) saveConnection(w http.ResponseWriter, r *http.Request) {
	var f form
	if err := decode(r, &f); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.guard(); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	id, profile, err := f.profile()
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	secret := strings.TrimSpace(f.Key)
	switch {
	case f.Legacy && f.Auth == "vault" && secret != "":
		err = errors.New(config.Text("para guardar no cofre, crie uma conexão nomeada", "to use the vault, create a named connection"))
	case f.Legacy:
		err = config.SaveProvider(f.Backend, profile.Provider)
	case f.Auth == "vault" && secret != "":
		err = config.SaveProviderProfileInVault(id, profile, secret)
	default:
		err = config.SaveProviderProfile(id, profile)
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.reconfigure()
	reply(w, http.StatusOK, map[string]string{"saved": id})
}

// reconfigure reloads the agent so a changed connection takes effect.
func (s *Server) reconfigure() {
	if s.app.CoderAgent.Model().ID != "" {
		_ = s.app.ConfigureCoderAgent()
	}
}

func (s *Server) deleteConnection(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID     string `json:"id"`
		Legacy bool   `json:"legacy"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.guard(); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	if err := config.DeleteProvider(request.ID, request.Legacy); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusOK, map[string]bool{"deleted": true})
}

// enableConnection turns a connection off or on. The stored key is written
// back as it was in the file, never one resolved from the environment.
func (s *Server) enableConnection(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID      string `json:"id"`
		Legacy  bool   `json:"legacy"`
		Enabled bool   `json:"enabled"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.guard(); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	cfg := config.Get()
	var err error
	if request.Legacy {
		backend := models.ModelProvider(request.ID)
		if !request.Enabled && s.app.CoderAgent.Model().Provider == backend {
			err = errors.New(config.Text("troque o modelo antes de desativar a conexão em uso", "switch models before disabling the connection in use"))
		} else {
			raw := cfg.Providers[backend]
			raw.Disabled, raw.APIKey = !request.Enabled, config.StoredProviderKey(request.ID, true)
			err = config.SaveProvider(backend, raw)
		}
	} else {
		if !request.Enabled && s.app.CoderAgent.Model().Provider == models.ModelProvider("profile:"+request.ID) {
			err = errors.New(config.Text("troque o modelo antes de desativar a conexão em uso", "switch models before disabling the connection in use"))
		} else {
			profile := cfg.ProviderProfiles[request.ID]
			profile.Disabled, profile.APIKey = !request.Enabled, config.StoredProviderKey(request.ID, false)
			err = config.SaveProviderProfile(request.ID, profile)
		}
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusOK, map[string]bool{"enabled": request.Enabled})
}

// setContext asks the active model for a context window, or for discovery
// with 0. The choice is saved with the model, as the terminal saves it.
func (s *Server) setContext(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Window int64 `json:"window"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if request.Window < 0 || request.Window > 2<<20 {
		fail(w, http.StatusBadRequest, errors.New(config.Text("janela de contexto fora do intervalo", "context window out of range")))
		return
	}
	if err := s.guard(); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	model := s.app.CoderAgent.Model()
	model.ContextWindow, model.ContextAuto, model.ContextEstimated = request.Window, request.Window == 0, false
	model.ContextSource = ""
	selected, err := s.app.CoderAgent.SelectModel(config.AgentCoder, model)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusOK, info(selected))
}

// setReasoning sets the active model's reasoning effort, where its provider
// accepts one.
func (s *Server) setReasoning(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Effort string `json:"effort"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	coder, ok := s.app.CoderAgent.(interface {
		SetReasoning(config.AgentName, models.ModelID, string) error
	})
	if !ok {
		fail(w, http.StatusNotImplemented, errors.New(config.Text("este agente não ajusta raciocínio", "this agent does not adjust reasoning")))
		return
	}
	if err := coder.SetReasoning(config.AgentCoder, s.app.CoderAgent.Model().ID, request.Effort); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"effort": request.Effort})
}

// files lists the workspace's files for an @ mention, best matches first.
// It reads names only, through the same listing the terminal's file picker
// uses, which honors .gitignore.
func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	files := s.workspaceFiles()
	if len(files) >= fileutil.WorkspaceLimit && strings.TrimSpace(query) != "" {
		files = s.searchFiles(query) // the listing stopped short in a large workspace
	}
	reply(w, http.StatusOK, fileutil.MatchFiles(files, query, 20))
}

// searchFiles searches the whole workspace for query. Searches run one at a
// time, and the last one's matches answer any longer query that contains
// it, so fast typing does not start a walk per key.
func (s *Server) searchFiles(query string) []string {
	const limit = 2000
	s.searchMu.Lock()
	defer s.searchMu.Unlock()
	lower := strings.ToLower(strings.TrimSpace(query))
	if s.searched != "" && strings.Contains(lower, s.searched) && len(s.found) < limit && time.Since(s.searchedAt) < 10*time.Second {
		return s.found
	}
	s.found, s.searched, s.searchedAt = fileutil.SearchWorkspace(config.WorkingDirectory(), lower, limit), lower, time.Now()
	return s.found
}

// workspaceFiles keeps the listing for a few seconds; @ asks on every key.
func (s *Server) workspaceFiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.filesAt) < 10*time.Second {
		return s.filesCache
	}
	list := fileutil.WorkspaceFiles(config.WorkingDirectory())
	s.filesCache, s.filesAt = list, time.Now()
	return list
}
