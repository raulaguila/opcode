package ui

import (
	"context"
	"fmt"
	"image/color"
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
}

// modelPicker holds the provider tabs and the catalog of the selected one.
type modelPicker struct {
	*listDialog
	providers []models.ModelProvider
	provider  models.ModelProvider
	catalog   []models.Model
	loading   bool
	details   bool
	err       error
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
	picker := &modelPicker{providers: enabledProviders()}
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
		width:        96,
		list:         List{Grouped: true},
		meta:         func(*listDialog) string { return picker.meta() },
		header:       picker.tabs,
		choose:       picker.choose,
		keys:         picker.keys,
	}
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
		found, err := fetch(models.WithoutCatalogMetadata(ctx))
		return catalogMsg{provider, found, err}
	}
}

// accept fills the list: local models first, then hosted ones; the active
// model carries a check and its details.
func (p *modelPicker) accept(m *Model, msg catalogMsg) {
	if msg.provider != p.provider {
		return
	}
	p.loading, p.err, p.catalog = false, msg.err, msg.models
	active := models.ModelID("")
	if m.app != nil && m.app.CoderAgent != nil {
		active = m.app.CoderAgent.Model().ID
	}
	var items []Item
	for _, model := range msg.models {
		if model.EmbeddingOnly() {
			continue
		}
		item := Item{ID: string(model.ID), Title: modelLabel(model), Right: modelFacts(model, model.ID == active), Value: model, Group: textFor("Locais", "Local")}
		if isCloud(model) {
			item.Group = textFor("Nuvem", "Cloud")
		}
		if model.ID == active {
			item.Mark = "✓"
		}
		items = append(items, item)
	}
	slices.SortStableFunc(items, func(a, b Item) int {
		if a.Mark != b.Mark {
			return strings.Compare(b.Mark, a.Mark)
		}
		if a.Group != b.Group {
			return strings.Compare(b.Group, a.Group)
		}
		return strings.Compare(a.Title, b.Title)
	})
	// Local stays above Cloud even after the active model is lifted.
	slices.SortStableFunc(items, func(a, b Item) int { return boolRank(isCloudGroup(a)) - boolRank(isCloudGroup(b)) })
	p.list = List{Items: items, Grouped: true}
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

func modelLabel(model models.Model) string {
	// Catalog names drop the size tag ("Gemma4" for gemma4:12b); the ID keeps it.
	squash := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, " ", "")) }
	if model.APIModel != "" && (model.Name == "" || model.Name == string(model.ID) || strings.HasPrefix(squash(model.APIModel), squash(model.Name))) {
		return model.APIModel
	}
	if model.Name != "" {
		return model.Name
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
	case p.loading:
		return name + textFor(" · carregando…", " · loading…")
	case p.list.Query != "":
		return name + " · " + results(len(p.list.Visible()))
	}
	return fmt.Sprintf(textFor("%s · %d de %d", "%s · %d of %d"), name, chat, len(p.catalog))
}

// tabs are the enabled providers; the selected one is filled.
func (p *modelPicker) tabs(_ *Model, width int) []string {
	row := ""
	for _, provider := range p.providers {
		label := " " + providerName(provider) + " "
		if provider == p.provider {
			row += on(P.Selected, P.Strong).Bold(true).Render(label)
		} else {
			row += on(P.Surface, P.Muted).Render(label)
		}
		row += on(P.Surface, P.Surface).Render(" ")
	}
	rows := []string{Line(ansi.Truncate(row, width, "…"), width, P.Surface)}
	switch {
	case p.provider == "":
		rows = append(rows, on(P.Surface, P.Muted).Render(textFor("Nenhuma conexão ativa · Ctrl+P configura uma", "No active connection · Ctrl+P sets one up")))
	case p.err != nil:
		rows = append(rows, on(P.Surface, P.Danger).Render(ansi.Truncate(textFor("Catálogo indisponível: ", "Catalog unavailable: ")+p.err.Error(), width, "…")))
	}
	return append(rows, Line("", width, P.Surface))
}

func (p *modelPicker) keys(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
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
		p.details = !p.details
		if p.details {
			p.side = p.detailColumn
		} else {
			p.side = nil
		}
		return nil, true
	case "ctrl+p":
		m.dialog = nil
		return m.openSettings(), true
	}
	return nil, false
}

func (p *modelPicker) choose(m *Model, item Item) tea.Cmd {
	model := item.Value.(models.Model)
	selected, err := m.app.CoderAgent.SelectModel(config.AgentCoder, model)
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	m.dialog = nil
	m.notice = ""
	m.info = fmt.Sprintf(textFor("Modelo: %s", "Model: %s"), modelLabel(selected))
	return nil
}

// detailColumn is the proposal's model sheet: name, capability tags and a
// key-value list.
func (p *modelPicker) detailColumn(_ *Model, item Item, width int) []string {
	model := item.Value.(models.Model)
	tag := func(text string, c color.Color) string { return on(P.Surface, c).Render("[" + text + "]") }
	tags := []string{}
	if model.SupportsTools != nil && *model.SupportsTools {
		tags = append(tags, tag(textFor("ferramentas", "tools"), P.Success))
	}
	if model.CanReason {
		tags = append(tags, tag(textFor("raciocínio", "reasoning"), P.Info))
	}
	if model.SupportsAttachments {
		tags = append(tags, tag(textFor("imagens", "images"), P.User))
	} else {
		tags = append(tags, tag(textFor("sem imagens", "no images"), P.Faint))
	}
	if isCloud(model) {
		tags = append(tags, tag(textFor("nuvem", "cloud"), P.Muted))
	}
	rows := []string{
		on(P.Surface, P.Strong).Bold(true).Render(ansi.Truncate(modelLabel(model), width, "…")),
		strings.Join(tags, on(P.Surface, P.Surface).Render(" ")),
		"",
	}
	pair := func(key, value string) {
		rows = append(rows, on(P.Surface, P.Muted).Render(fmt.Sprintf("%-12s ", key))+on(P.Surface, P.Strong).Render(ansi.Truncate(value, max(1, width-13), "…")))
	}
	pair("ID", model.APIModel)
	context := textFor("não informado", "not reported")
	if model.ContextWindow > 0 {
		context = compactTokens(model.ContextWindow)
		if model.ContextEstimated {
			context += textFor(" (estimado)", " (estimated)")
		}
	}
	pair(textFor("Contexto", "Context"), context)
	pair(textFor("Saída", "Output"), fmt.Sprintf(textFor("%d tokens por resposta", "%d tokens per reply"), model.DefaultMaxTokens))
	api := model.API
	if api == "" {
		api = textFor("padrão do provedor", "provider default")
	}
	pair("API", api)
	return append(rows, "", Button(textFor("Usar · Enter", "Use · Enter"), true, P.Ask))
}
