package ui

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
)

type catalogMsg struct {
	provider models.ModelProvider
	models   []models.Model
	err      error
	loaded   map[string]models.OllamaRuntime // what a local Ollama holds in memory
}

// modelPicker holds the provider tabs and the catalog of the selected one.
type modelPicker struct {
	*listDialog
	providers []models.ModelProvider
	provider  models.ModelProvider
	catalog   []models.Model
	loaded    map[string]models.OllamaRuntime
	active    models.ModelID
	loading   bool
	details   bool
	err       error
	role      config.AgentName // which job the chosen model does; Ctrl+A cycles it
}

var roles = []config.AgentName{config.AgentCoder, config.AgentTask, config.AgentTitle, config.AgentSummarizer}

func roleName(role config.AgentName) string {
	switch role {
	case config.AgentTask:
		return textFor("tarefas", "tasks")
	case config.AgentTitle:
		return textFor("títulos", "titles")
	case config.AgentSummarizer:
		return textFor("resumos", "summaries")
	}
	return textFor("principal", "main")
}

func enabledProviders() []models.ModelProvider {
	cfg := config.Get()
	if cfg == nil {
		return nil
	}
	var out []models.ModelProvider
	for id, p := range cfg.ProviderProfiles {
		if !p.Disabled {
			out = append(out, models.ModelProvider("profile:"+id))
		}
	}
	for id, p := range cfg.Providers {
		if !p.Disabled {
			out = append(out, id)
		}
	}
	slices.SortFunc(out, func(a, b models.ModelProvider) int {
		ra, rb := models.ProviderPopularity[a], models.ProviderPopularity[b]
		if ra == 0 {
			ra = 999
		}
		if rb == 0 {
			rb = 999
		}
		if ra != rb {
			return ra - rb
		}
		return strings.Compare(string(a), string(b))
	})
	return out
}

func providerName(p models.ModelProvider) string {
	if id, ok := strings.CutPrefix(string(p), "profile:"); ok {
		if profile := config.Get().ProviderProfiles[id]; profile.Name != "" {
			return profile.Name
		}
		return id
	}
	switch p {
	case models.ProviderLocal:
		if config.Get().Providers[p].API == "ollama" {
			return "Ollama"
		}
		return textFor("Servidor local", "Local server")
	case models.ProviderOpenAI:
		return "OpenAI"
	case models.ProviderAnthropic:
		return "Anthropic"
	case models.ProviderGemini:
		return "Gemini"
	case models.ProviderOpenRouter:
		return "OpenRouter"
	}
	return string(p)
}

func (m *Model) openModels() tea.Cmd {
	picker := &modelPicker{providers: enabledProviders(), role: config.AgentCoder}
	if m.app != nil && m.app.CoderAgent != nil {
		picker.provider = m.app.CoderAgent.Model().Provider
	}
	if !slices.Contains(picker.providers, picker.provider) && len(picker.providers) > 0 {
		picker.provider = picker.providers[0]
	}
	picker.listDialog = &listDialog{
		title:        textFor("Escolher modelo", "Choose model"),
		legend:       textFor("Enter usar · Tab provedor · Ctrl+D detalhes · Ctrl+P conexão · Esc fechar", "Enter use · Tab provider · Ctrl+D details · Ctrl+P connection · Esc close"),
		searchPrompt: "⌕",
		placeholder:  textFor("Buscar por nome ou ID", "Search by name or ID"),
		width:        96,
		list:         List{Grouped: true},
		meta:         func(*listDialog) string { return picker.meta() },
		header:       picker.tabs,
		choose:       picker.choose,
		keys:         picker.keys,
	}
	picker.setDetails(false)
	m.dialog = picker
	return picker.load()
}

func (p *modelPicker) load() tea.Cmd {
	if p.provider == "" {
		return nil
	}
	p.loading, p.err = true, nil
	provider := p.provider
	fetch := config.ModelCatalog(provider)
	if id, ok := strings.CutPrefix(string(provider), "profile:"); ok {
		fetch = config.ProfileModelCatalog(id)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		// Metadata says which models take tools and images; the plain list is the fallback.
		found, err := fetch(ctx)
		if err != nil {
			found, err = fetch(models.WithoutCatalogMetadata(ctx))
		}
		msg := catalogMsg{provider: provider, models: found, err: err}
		if base, key, ok := config.OllamaConnection(provider); ok {
			if running, err := models.InspectOllama(ctx, base, key); err == nil {
				msg.loaded = map[string]models.OllamaRuntime{}
				for _, model := range running {
					msg.loaded[model.Name] = model
				}
			}
		}
		return msg
	}
}

// accept fills the list: local models first, then hosted ones; the active
// model carries a check.
func (p *modelPicker) accept(m *Model, msg catalogMsg) {
	if msg.provider != p.provider {
		return
	}
	p.loading, p.err, p.catalog, p.loaded = false, msg.err, msg.models, msg.loaded
	p.markActive(m)
	p.fill()
}

// markActive checks the model doing the picker's role.
func (p *modelPicker) markActive(m *Model) {
	p.active = ""
	if p.role != config.AgentCoder {
		if cfg := config.Get(); cfg != nil {
			p.active = cfg.Agents[p.role].Model
		}
	} else if m.app != nil && m.app.CoderAgent != nil {
		p.active = m.app.CoderAgent.Model().ID
	}
}

// fill lists the catalog. The details view drops groups and facts, and shows
// embedding models greyed out, as the proposal does.
func (p *modelPicker) fill() {
	var items []Item
	for _, model := range p.catalog {
		item := Item{ID: string(model.ID), Title: modelLabel(model), Value: model, Group: textFor("Locais", "Local")}
		switch {
		case model.EmbeddingOnly() && !p.details:
			continue
		case model.EmbeddingOnly():
			item.Faint, item.Right = true, "embeddings"
		case !p.details:
			item.Right = modelFacts(model, model.ID == p.active)
		}
		if isCloud(model) {
			item.Group = textFor("Nuvem", "Cloud")
		}
		if model.ID == p.active {
			item.Mark = "✓"
		}
		items = append(items, item)
	}
	rank := func(item Item) int {
		switch {
		case item.Faint:
			return 3
		case item.Mark != "":
			return 0
		case isCloudGroup(item):
			return 2
		}
		return 1
	}
	slices.SortStableFunc(items, func(a, b Item) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return strings.Compare(a.Title, b.Title)
	})
	// The active cloud model leads the list but stays in its group.
	if len(items) > 0 && items[0].Mark != "" && isCloudGroup(items[0]) {
		slices.SortStableFunc(items, func(a, b Item) int { return boolRank(isCloudGroup(a)) - boolRank(isCloudGroup(b)) })
	}
	p.list = List{Items: items, Grouped: !p.details}
}

func isCloudGroup(item Item) bool { return item.Group == textFor("Nuvem", "Cloud") }

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isCloud(model models.Model) bool {
	name := strings.ToLower(model.APIModel)
	return model.Hosting == "cloud" || strings.HasSuffix(name, ":cloud") || strings.HasSuffix(name, "-cloud")
}

// modelLabel is the model's API ID, as the proposal lists it: catalog names
// drop the size tag ("Gemma4" for gemma4:12b).
func modelLabel(model models.Model) string {
	if model.APIModel != "" {
		return model.APIModel
	}
	return string(model.ID)
}

func compactTokens(n int64) string {
	if n >= 1024 && n%1024 == 0 {
		return fmt.Sprintf("%dk", n/1024)
	}
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprint(n)
}

// modelFacts is the row's right side: "16k · ferramentas · em uso".
func modelFacts(model models.Model, active bool) string {
	var facts []string
	if model.ContextWindow > 0 {
		context := compactTokens(model.ContextWindow)
		if model.ContextEstimated {
			context = "~" + context
		}
		facts = append(facts, context)
	}
	if model.SupportsTools != nil && *model.SupportsTools {
		facts = append(facts, textFor("ferramentas", "tools"))
	}
	if model.SupportsAttachments {
		facts = append(facts, textFor("imagens", "images"))
	}
	if active {
		facts = append(facts, textFor("em uso", "in use"))
	}
	return strings.Join(facts, " · ")
}

func (p *modelPicker) meta() string {
	chat := 0
	for _, model := range p.catalog {
		if !model.EmbeddingOnly() {
			chat++
		}
	}
	name := providerName(p.provider)
	switch {
	case p.provider == "":
		return ""
	case p.details:
		if host := providerHost(p.provider); host != "" {
			return name + " · " + host
		}
		return name
	case p.loading:
		return name + textFor(" · carregando…", " · loading…")
	case p.list.Query != "":
		return name + " · " + results(len(p.list.Visible()))
	}
	return fmt.Sprintf(textFor("%s · %d de %d", "%s · %d of %d"), name, chat, len(p.catalog))
}

// providerHost is the connection's address without scheme and path.
func providerHost(p models.ModelProvider) string {
	cfg := config.Get()
	base := cfg.Providers[p].BaseURL
	if id, ok := strings.CutPrefix(string(p), "profile:"); ok {
		base = cfg.ProviderProfiles[id].BaseURL
	}
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		return u.Host
	}
	return ""
}

// tabs are the enabled providers; the selected one is raised.
func (p *modelPicker) tabs(_ *Model, width int) []string {
	var rows []string
	if !p.details {
		row := ""
		for _, provider := range p.providers {
			label := " " + providerName(provider) + " "
			if provider == p.provider {
				row += on(P.Raised, P.Strong).Bold(true).Render(label)
			} else {
				row += on(P.Surface, P.Muted).Render(label)
			}
			row += on(P.Surface, P.Surface).Render(" ")
		}
		rows = append(rows, Line(ansi.Truncate(row, width, "…"), width, P.Surface), "")
	}
	switch {
	case p.provider == "":
		rows = append(rows, on(P.Surface, P.Muted).Render(textFor("Nenhuma conexão ativa · Ctrl+P configura uma", "No active connection · Ctrl+P sets one up")))
	case p.err != nil:
		rows = append(rows, on(P.Surface, P.Danger).Render(ansi.Truncate(textFor("Catálogo indisponível: ", "Catalog unavailable: ")+p.err.Error(), width, "…")))
	}
	if len(rows) > 0 && rows[len(rows)-1] != "" {
		rows = append(rows, "")
	}
	return rows
}

func (p *modelPicker) keys(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if p.details && p.role == config.AgentCoder && (msg.String() == "c" || msg.String() == "r") && m.app != nil && m.app.CoderAgent != nil {
		return p.cycleKnob(m, msg.String()), true
	}
	switch msg.String() {
	case "tab", "shift+tab":
		if len(p.providers) < 2 {
			return nil, true
		}
		step := 1
		if msg.String() == "shift+tab" {
			step = -1
		}
		i := slices.Index(p.providers, p.provider)
		p.provider = p.providers[(i+step+len(p.providers))%len(p.providers)]
		p.catalog, p.list = nil, List{Grouped: true}
		return p.load(), true
	case "ctrl+d":
		p.setDetails(!p.details)
		return nil, true
	case "ctrl+a":
		p.role = roles[(slices.Index(roles, p.role)+1)%len(roles)]
		p.markActive(m)
		p.setDetails(p.details)
		return nil, true
	case "ctrl+p":
		m.dialog = nil
		return m.openSettings(), true
	}
	return nil, false
}

// setDetails switches to the proposal's two-column sheet and back, keeping
// the selected model.
func (p *modelPicker) setDetails(on bool) {
	current, _ := p.list.Current()
	p.details, p.noSearch, p.side, p.split, p.width = on, on, nil, 0, 96
	p.title = textFor("Escolher modelo", "Choose model")
	p.legend = textFor("Enter usar · Tab provedor · Ctrl+A função · Ctrl+D detalhes · Ctrl+P conexão · Esc fechar", "Enter use · Tab provider · Ctrl+A role · Ctrl+D details · Ctrl+P connection · Esc close")
	if on {
		p.side, p.split, p.width = p.detailColumn, 36, 120
		p.title = textFor("Escolher modelo · detalhes", "Choose model · details")
		p.legend = textFor("Enter usar · C contexto · R raciocínio · Ctrl+D ocultar detalhes · Tab provedor · Esc fechar", "Enter use · C context · R reasoning · Ctrl+D hide details · Tab provider · Esc close")
	}
	if p.role != config.AgentCoder {
		p.title += " · " + roleName(p.role)
	}
	p.fill()
	for i, item := range p.list.Visible() {
		if item.ID == current.ID {
			p.list.Selected = i
			p.list.keepVisible(min(listRows, 20))
		}
	}
}

func (p *modelPicker) choose(m *Model, item Item) tea.Cmd {
	if item.Faint {
		m.info = textFor("Modelo de embeddings: não conversa", "Embedding model: not for chat")
		return nil
	}
	model := item.Value.(models.Model)
	selected, err := m.app.CoderAgent.SelectModel(p.role, model)
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	m.dialog = nil
	m.notice = ""
	m.info = fmt.Sprintf(textFor("Modelo: %s", "Model: %s"), modelLabel(selected))
	if p.role != config.AgentCoder {
		m.info = fmt.Sprintf(textFor("Modelo de %s: %s", "Model for %s: %s"), roleName(p.role), modelLabel(selected))
	}
	return nil
}

// detailColumn is the proposal's model sheet: the ID, capability chips, a
// key-value list and the action.
func (p *modelPicker) detailColumn(m *Model, item Item, width int) []string {
	model := item.Value.(models.Model)
	var tags string
	if model.EmbeddingOnly() {
		tags += Chip("embeddings", P.Faint, P.Surface)
	} else if model.SupportsTools != nil && *model.SupportsTools {
		tags += Chip(textFor("ferramentas", "tools"), P.Success, P.Surface)
	}
	if model.CanReason {
		tags += Chip(textFor("raciocínio", "reasoning"), P.Info, P.Surface)
	}
	if model.SupportsAttachments {
		tags += Chip(textFor("imagens", "images"), P.User, P.Surface)
	} else {
		tags += Chip(textFor("sem imagens", "no images"), P.Faint, P.Surface)
	}
	rows := []string{
		on(P.Surface, P.Strong).Bold(true).Render(ansi.Truncate(modelLabel(model), width, "…")),
		tags,
		"",
	}
	pair := func(key, value string) {
		rows = append(rows, on(P.Surface, P.Muted).Render(fmt.Sprintf("%-14s", key))+on(P.Surface, P.Strong).Render(ansi.Truncate(value, max(1, width-14), "…")))
	}
	held, isLoaded := p.loaded[model.APIModel]
	context := textFor("não informado", "not reported")
	switch {
	case isLoaded && held.Context > 0 && model.ContextWindow > 0 && held.Context != model.ContextWindow:
		context = fmt.Sprintf(textFor("%s em uso · %s suportado", "%s in use · %s supported"), compactTokens(held.Context), compactTokens(model.ContextWindow))
	case model.ContextWindow > 0:
		context = compactTokens(model.ContextWindow)
		if model.ContextEstimated {
			context = "~" + context + textFor(" estimado", " estimated")
		}
		if model.ID == p.active {
			context += textFor(" · em uso", " · in use")
		}
	}
	pair(textFor("Contexto", "Context"), context)
	if model.DefaultMaxTokens > 0 {
		pair(textFor("Saída", "Output"), fmt.Sprintf(textFor("%d tokens por resposta", "%d tokens per reply"), model.DefaultMaxTokens))
	}
	where := textFor("local", "local")
	if isCloud(model) {
		where = textFor("nuvem", "cloud")
	}
	if p.loaded != nil && !isCloud(model) {
		state := textFor("não", "no")
		if isLoaded {
			state = fmt.Sprintf(textFor("● sim · %s · %.1f GB", "● yes · %s · %.1f GB"), held.Placement(), float64(held.Size)/1e9)
		}
		pair(textFor("Carregado", "Loaded"), state)
	}
	pair(textFor("Execução", "Runs"), where)
	if model.ID == p.active {
		if speed, live, _ := m.rate(); speed > 0 && !live {
			pair(textFor("Velocidade", "Speed"), fmt.Sprintf(textFor("≈ %d t/s na última resposta", "≈ %d t/s on the last reply"), int(speed+0.5)))
		}
	}
	if model.API != "" {
		pair("API", model.API)
	}
	rows = append(rows, "")
	if model.EmbeddingOnly() {
		return append(rows, on(P.Surface, P.Muted).Render(textFor("Gera embeddings; não serve para conversar.", "Makes embeddings; not for chat.")))
	}
	rows = append(rows, Button(textFor("Usar este modelo", "Use this model"), true, P.Ask))
	if model.ID == p.active {
		// The knobs of the model in use: C cycles its context, R its reasoning.
		gap := on(P.Surface, P.Surface).Render(" ")
		window := textFor("automático", "automatic")
		if current := config.ModelWithProviderDefaults(model); !current.UsesAutomaticContext() {
			window = compactTokens(current.ContextWindow)
		}
		effort := textFor("não suportado", "not supported")
		if choices := config.ModelWithProviderDefaults(model).ReasoningChoices(); len(choices) > 0 {
			effort = effortName(config.Get().Agents[config.AgentCoder].ReasoningEffort)
		}
		rows = append(rows, "", Button(textFor("Contexto: ", "Context: ")+window+" ▾", false, P.Ask)+on(P.Surface, P.Muted).Render(" C")+gap+gap+
			Button(textFor("Raciocínio: ", "Reasoning: ")+effort+" ▾", false, P.Ask)+on(P.Surface, P.Muted).Render(" R"))
	}
	return rows
}

func effortName(effort string) string {
	switch effort {
	case "low":
		return textFor("baixo", "low")
	case "medium":
		return textFor("médio", "medium")
	case "high":
		return textFor("alto", "high")
	case "minimal":
		return textFor("mínimo", "minimal")
	case "":
		return textFor("padrão", "default")
	}
	return effort
}

// contextWindows are the context sizes C cycles through; 0 is automatic.
var contextWindows = []int64{0, 8192, 16384, 32768, 65536, 131072}

// cycleKnob changes the active model's context (C) or reasoning (R), saved
// with the model as the terminal saves any choice of model.
func (p *modelPicker) cycleKnob(m *Model, knob string) tea.Cmd {
	if err := m.saveGuard(); err != nil {
		m.notice = err.Error()
		return nil
	}
	model := m.app.CoderAgent.Model()
	switch knob {
	case "c":
		current := config.ModelWithProviderDefaults(model)
		at := 0
		if !current.UsesAutomaticContext() {
			at = slices.Index(contextWindows, current.ContextWindow)
		}
		next := contextWindows[(at+1)%len(contextWindows)]
		model.ContextWindow, model.ContextAuto, model.ContextEstimated, model.ContextSource = next, next == 0, false, ""
		if _, err := m.app.CoderAgent.SelectModel(config.AgentCoder, model); err != nil {
			m.notice = err.Error()
			return nil
		}
	case "r":
		choices := config.ModelWithProviderDefaults(model).ReasoningChoices()
		coder, ok := m.app.CoderAgent.(interface {
			SetReasoning(config.AgentName, models.ModelID, string) error
		})
		if len(choices) == 0 || !ok {
			m.info = textFor("Este modelo não ajusta raciocínio", "This model does not adjust reasoning")
			return nil
		}
		at := slices.Index(choices, config.Get().Agents[config.AgentCoder].ReasoningEffort)
		if err := coder.SetReasoning(config.AgentCoder, model.ID, choices[(at+1)%len(choices)]); err != nil {
			m.notice = err.Error()
			return nil
		}
	}
	p.active = m.app.CoderAgent.Model().ID
	return p.load()
}
