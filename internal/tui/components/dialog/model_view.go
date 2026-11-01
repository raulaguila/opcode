package dialog

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

// modelGeometry is shared by drawing and mouse hit testing, including compact
// padding. List coordinates never depend on translated labels or model names.
type modelGeometry struct {
	width, height, padY, contentX, titleY, listY, rows, listWidth int
	grouped                                                       bool
}

func (m *modelDialogCmp) geometry() modelGeometry {
	width := 88
	if m.width > 0 {
		width = max(1, min(width, m.width-8))
	}
	height := 28
	if m.height > 0 {
		height = m.height
	}
	pad := 1
	if height < 18 {
		pad = 0
	}
	// Title, position and the main legend live in the frame's borders.
	helpRows := 0
	if m.details && height >= 18 {
		helpRows++
		if len(m.reasoningModel().ReasoningChoices()) > 0 {
			helpRows++
		}
	}
	// Wide details sit beside the list instead of below it.
	// The compact list carries context and capabilities in its rows; details
	// show on demand (Ctrl+D), beside the list when there is room.
	listWidth, details := width, 0
	if m.details {
		details = len(m.modelDetailLines())
	}
	if m.details && width >= 80 {
		listWidth, details = width*9/20, 0
	}
	// Rows above the list: provider tabs, search and a transient note.
	header, tabsY := 1, -1
	if m.providerTabs() {
		tabsY = 1 + pad
		header++
	}
	if m.showNote() {
		header++
	}
	overhead := header + helpRows + 2 + pad*2 + details
	g := modelGeometry{width: width, height: height, padY: pad, contentX: 3, titleY: tabsY, listY: 1 + pad + header, rows: max(1, min(numVisibleModels, height-overhead)), listWidth: listWidth}
	g.grouped = m.query == "" && m.hasCloudAndLocal() && len(m.filtered)+2 <= g.rows
	return g
}

func (m *modelDialogCmp) activeModelID() models.ModelID {
	target := m.target
	if target == "" {
		target = config.AgentCoder
	}
	if cfg := config.Get(); cfg != nil {
		return cfg.Agents[target].Model
	}
	return ""
}

func (m *modelDialogCmp) providerLabel() string { return m.providerName(m.provider) }

func (m *modelDialogCmp) providerName(provider models.ModelProvider) string {
	if id, ok := strings.CutPrefix(string(provider), "profile:"); ok {
		p := config.Get().ProviderProfiles[id]
		if p.Name != "" {
			return p.Name
		}
		return id
	}
	switch provider {
	case "":
		return config.Text("Sem provedor", "No provider")
	case models.ProviderLocal:
		if config.Get().Providers[provider].API == "ollama" {
			return "Ollama"
		}
		for _, model := range m.models {
			if model.API == "ollama" {
				return "Ollama"
			}
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
	default:
		return string(provider)
	}
}

func (m *modelDialogCmp) modelTitle() string {
	title := config.Text("Escolher modelo", "Choose model")
	switch m.target {
	case config.AgentTask:
		title = config.Text("Modelo de tarefas", "Task model")
	case config.AgentTitle:
		title = config.Text("Modelo de títulos", "Title model")
	case config.AgentSummarizer:
		title = config.Text("Modelo de resumo", "Summary model")
	}
	if !m.providerTabs() {
		title += " · " + m.providerLabel()
	}
	if m.details && m.width >= 88 {
		title += config.Text(" · detalhes", " · details")
	}
	if m.setup {
		title = config.Text("Configuração 2/3 · ", "Setup 2/3 · ") + title
	}
	return title
}

func modelIdentity(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, text)
}

func (m *modelDialogCmp) modelLabel(model models.Model) string {
	name, id := model.Name, model.APIModel
	// A configured model without a name falls back to its internal ID
	// ("local.x"); show the provider's name like every other row.
	if name == "" || (name == string(model.ID) && id != "") {
		return id
	}
	if modelIdentity(name) == modelIdentity(id) {
		return name
	}
	if len(id) <= 36 && strings.HasPrefix(modelIdentity(id), modelIdentity(name)) {
		return id
	}
	duplicate := false
	for _, other := range m.models {
		if other.ID != model.ID && other.Name == model.Name {
			duplicate = true
			break
		}
	}
	if duplicate {
		if slash := strings.LastIndex(id, "/"); slash >= 0 {
			id = id[slash+1:]
		}
		return name + " · " + id
	}
	return name
}

func (m *modelDialogCmp) modelDetailLines() []string {
	if m.selectedIdx < 0 || m.selectedIdx >= len(m.filtered) {
		return nil
	}
	selected := m.filtered[m.selectedIdx]
	contextNote := map[string]string{
		"ollama:loaded": config.Text("alocado no servidor", "server allocation"), "lmstudio:loaded": config.Text("alocado no servidor", "server allocation"),
		"ollama:model": config.Text("máximo do modelo", "model maximum"), "lmstudio:model": config.Text("máximo do modelo", "model maximum"),
		"ollama:parameter": config.Text("parâmetro do servidor", "server parameter"), "configuration": config.Text("configurado", "configured"),
		"cache": config.Text("última consulta", "cached metadata"), "catalog": config.Text("catálogo interno", "built-in catalog"), "provider": config.Text("informado pelo provedor", "provider metadata"),
	}[selected.ContextSource]
	if selected.ContextEstimated {
		contextNote = config.Text("estimativa alternativa", "fallback estimate")
	}
	context := config.Text("Contexto: não informado", "Context: not reported")
	modelDetailLinesSelectedContextWindow(&selected, contextNote, &context)
	effective := config.Text("Efetivo: disponível após ativar", "Effective: available after activation")
	modelDetailLinesActiveContextModelTargetAgentCoder(m, &selected, &effective)
	tools := config.Text("Ferramentas: não informado", "Tools: not reported")
	if selected.SupportsTools != nil {
		if *selected.SupportsTools {
			tools = config.Text("Ferramentas: informadas pelo provedor", "Tools: reported by provider")
		} else {
			tools = config.Text("Ferramentas: não suportadas", "Tools: not supported")
		}
	}
	if result0, handled := modelDetailLinesDetails(m, context, effective, tools); handled {
		return result0
	}
	rows := []string{"ID: " + selected.APIModel, context, effective, tools}
	rows = append(rows, modelHostingDetailLines(selected)...)
	if m.height > 0 && m.height < 18 {
		return rows
	}
	caps := []string{}
	if selected.CanReason {
		caps = append(caps, config.Text("raciocínio", "reasoning"))
	}
	if selected.SupportsAttachments {
		caps = append(caps, config.Text("anexos", "attachments"))
	}
	if len(caps) == 0 {
		caps = append(caps, config.Text("não informadas", "not reported"))
	}
	protocol := selected.API
	if protocol == "" {
		protocol = config.Text("padrão do provedor", "provider default")
	}
	rows = append(rows, fmt.Sprintf(config.Text("Saída: %d · API: %s", "Output: %d · API: %s"), selected.DefaultMaxTokens, protocol))
	rows = append(rows, config.Text("Capacidades: ", "Capabilities: ")+strings.Join(caps, ", "))
	if m.metadataNote != "" {
		rows = append(rows, m.metadataNote)
	}
	return rows
}

func (m *modelDialogCmp) catalogView() string {
	full := m.geometry()
	g := full
	g.width = full.listWidth
	t := theme.CurrentTheme()
	base := dialogBody().Width(g.width).MaxWidth(g.width)
	line := func(s string) string { return base.Render(ansi.Truncate(interfaceCopy(s), g.width, "…")) }
	var rows []string
	if m.providerTabs() {
		rows = append(rows, m.providerTabsRow(g.width))
	}
	label := "⌕ "
	m.search.SetWidth(max(1, g.width-ansi.StringWidth(label)))
	m.search.Placeholder = config.Text("Buscar por nome ou ID", "Search by name or ID")
	// The search reads as a field: a darker band across the dialog.
	field := styles.BaseStyle().Background(t.BackgroundDarker()).Width(g.width)
	rows = append(rows, field.Render(dialogBody().Background(t.BackgroundDarker()).Foreground(t.Info()).Render(label)+m.search.View()))
	if m.showNote() {
		note := m.note
		if m.loading {
			note = config.Text("Carregando modelos…", "Loading provider models…")
		}
		rows = append(rows, dialogHint(note, g.width))
	}
	active := m.activeModelID()
	catalogViewScrollOffset(m, &g, t, &base, &rows, active)
	catalogViewFiltered(m, line, &rows)
	if g.width == full.width && m.details {
		for _, detail := range m.modelDetailLines() {
			rows = append(rows, dialogHint(detail, g.width))
		}
	} else if g.width != full.width {
		rows = m.besideDetails(rows, g.width, full.width-g.width-3)
	}
	g.width = full.width
	// The corner reads like the catalog: provider and chat models of the
	// catalog, or the result count while searching.
	// The provider repeats in the corner on wide dialogs; narrow ones keep
	// the room for the counts (the tabs already name the provider).
	position := m.providerLabel()
	if !m.providerTabs() || g.width < 60 {
		position = ""
	}
	counts := ""
	switch {
	case len(m.filtered) > g.rows:
		// A list that scrolls says where the highlight is.
		counts = fmt.Sprintf(config.Text("%d de %d", "%d of %d"), m.selectedIdx+1, len(m.filtered))
	case m.query != "" && len(m.filtered) == 1:
		counts = config.Text("1 resultado", "1 result")
	case m.query != "":
		counts = fmt.Sprintf(config.Text("%d resultados", "%d results"), len(m.filtered))
	case len(m.models) > 0:
		counts = fmt.Sprintf(config.Text("%d de %d", "%d of %d"), m.chatModelCount(), len(m.models))
	}
	if counts != "" {
		if position != "" {
			position += " · "
		}
		position += counts
	}
	if m.scrollOffset > 0 {
		position += " · ↑"
	}
	if m.scrollOffset+g.rows < len(m.filtered) {
		position += " · ↓"
	}
	hint := config.Text("Enter usar · Tab provedor · Ctrl+D detalhes · Ctrl+P conexão · Esc fechar", "Enter use · Tab provider · Ctrl+D details · Ctrl+P connection · Esc close")
	if g.width < 48 {
		hint = config.Text("Enter · Ctrl+D detalhes · Esc", "Enter · Ctrl+D details · Esc")
	}
	if len(m.filtered) == 0 && !m.loading {
		hint = config.Text("Tab provedor · Ctrl+R tenta · Ctrl+P conexão · Esc", "Tab provider · Ctrl+R retry · Ctrl+P connection · Esc")
	}
	if m.details && g.height >= 18 {
		if len(m.reasoningModel().ReasoningChoices()) > 0 {
			rows = append(rows, dialogHint(config.Text("Ctrl+E esforço do modelo ativo", "Ctrl+E active model effort"), g.width))
		}
		// Details add only the shortcuts the main legend does not show.
		rows = append(rows, dialogHint(config.Text("Shift+Tab/Alt+←→ provedor · Ctrl+R atualiza · Ctrl+A função · Ctrl+T testar", "Shift+Tab/Alt+←→ provider · Ctrl+R refresh · Ctrl+A role · Ctrl+T probe"), g.width))
	}
	frame := styles.Frame(strings.Join(rows, "\n"), g.width, 2, g.padY, interfaceCopy(m.modelTitle()), position, interfaceCopy(hint))
	return lipgloss.NewStyle().MaxWidth(g.width + 6).MaxHeight(g.height).Render(frame)
}

func (m *modelDialogCmp) catalogCounts() string {
	return fmt.Sprintf(config.Text("%d catálogo · %d conversa", "%d catalog · %d chat"), len(m.models), m.chatModelCount())
}

func (m *modelDialogCmp) chatModelCount() int {
	chat := 0
	for _, model := range m.models {
		if !model.EmbeddingOnly() {
			chat++
		}
	}
	return chat
}

func (m *modelDialogCmp) providerTabs() bool { return len(m.availableProviders) > 0 }

// showNote: the catalog summary now lives in the corner, so the note row only
// appears for loading and for messages such as errors.
func (m *modelDialogCmp) showNote() bool {
	return m.loading || m.note != "" && !strings.HasPrefix(m.note, m.catalogCounts())
}

// providerTabsRow lists the enabled providers as tabs; Tab moves between them.
func (m *modelDialogCmp) providerTabsRow(width int) string {
	t := theme.CurrentTheme()
	row := ""
	for _, provider := range m.availableProviders {
		label := " " + m.providerName(provider) + " "
		if provider == m.provider {
			row += styles.SelectedRow().Bold(true).Render(label)
		} else {
			row += dialogBody().Foreground(styles.ReadableForeground(t.TextMuted(), t.BackgroundSecondary())).Render(label)
		}
		row += dialogBody().Render(" ")
	}
	return ansi.Truncate(row, width, "…")
}

// providerTabAt is the provider whose tab covers column x of the tabs row.
func (m *modelDialogCmp) providerTabAt(x int) (models.ModelProvider, bool) {
	start := 0
	for _, provider := range m.availableProviders {
		end := start + ansi.StringWidth(m.providerName(provider)) + 2
		if x >= start && x < end {
			return provider, true
		}
		start = end + 1
	}
	return "", false
}

func isCloudModel(model models.Model) bool {
	return modelHostingLabel(model) == config.Text("Nuvem", "Cloud")
}

func (m *modelDialogCmp) hasCloudAndLocal() bool {
	cloud, local := false, false
	for _, model := range m.filtered {
		if isCloudModel(model) {
			cloud = true
		} else {
			local = true
		}
	}
	return cloud && local
}

// modelRows maps list rows to models; -1 marks a group heading.
func (m *modelDialogCmp) modelRows(g modelGeometry) []int {
	var rows []int
	for i := m.scrollOffset; i < min(len(m.filtered), m.scrollOffset+g.rows) && len(rows) < g.rows; i++ {
		if g.grouped && (i == m.scrollOffset || isCloudModel(m.filtered[i-1]) != isCloudModel(m.filtered[i])) {
			rows = append(rows, -1)
		}
		rows = append(rows, i)
	}
	return rows
}

func containsModel(list []models.ModelID, id models.ModelID) bool {
	return slices.Contains(list, id)
}

func tokenLabel(n int64) string {
	if n >= 1024 && n%1024 == 0 {
		return fmt.Sprintf("%dk (%d)", n/1024, n)
	}
	return fmt.Sprint(n)
}

func modelDetailLinesSelectedContextWindow(selected *models.Model, contextNote string, context *string) {
	if selected.ContextWindow > 0 {
		(*context) = fmt.Sprintf(config.Text("Contexto: %s", "Context: %s"), tokenLabel(selected.ContextWindow))
		if contextNote != "" {
			(*context) += " (" + contextNote + ")"
		}
		if !selected.ContextEstimated && selected.ContextSource != "ollama:loaded" && selected.ContextSource != "lmstudio:loaded" && selected.ContextSource != "ollama:parameter" && selected.ContextSource != "configuration" {
			(*context) += config.Text(" · anunciado", " · advertised")
		}
	}
}

func catalogViewScrollOffset(m *modelDialogCmp, g *modelGeometry, t theme.Theme, base *lipgloss.Style, rows *[]string, active models.ModelID) {
	layout := m.modelRows(*g)
	for k, i := range layout {
		if i < 0 {
			heading := config.Text("LOCAIS", "LOCAL")
			if k+1 < len(layout) && isCloudModel(m.filtered[layout[k+1]]) {
				heading = config.Text("NUVEM", "CLOUD")
			}
			(*rows) = append((*rows), dialogHint(heading, g.width))
			continue
		}
		model := m.filtered[i]
		caret, mark := " ", " "
		if i == m.selectedIdx {
			caret = "▌"
		}
		if model.ID == active {
			mark = "✓"
		}
		label := m.modelLabel(model)
		if hosting := modelHostingLabel(model); hosting != "" {
			label += " · " + hosting
		}
		prefs := config.Get().Preferences
		if containsModel(prefs.FavoriteModels, model.ID) {
			label = "★ " + label
		} else if containsModel(prefs.RecentModels, model.ID) {
			label = "↺ " + label
		}
		group := ""
		catalogViewScrollOffsetModelIDActive(active, &model, &prefs, &group)
		rendered := fmt.Sprintf("%s %s %s", caret, mark, label)
		rendered = modelRowGroup(rendered, group, modelRowContext(model)+modelRowCapabilities(model, g.width), g.width, model.ID == active)
		row := (*base)
		if i == m.selectedIdx {
			row = row.Background(t.BorderDim()).Foreground(t.Text()).Bold(true)
		}
		(*rows) = append((*rows), row.Render(ansi.Truncate(interfaceCopy(rendered), g.width, "…")))
	}
}

func modelRowGroup(row, group, context string, width int, active bool) string {
	if width < 12 || ansi.StringWidth(context)+5 > width {
		return ansi.Truncate(row, width, "…")
	}
	metadata := context
	if group != "" && (width >= 48 || active) && width >= ansi.StringWidth(context)+ansi.StringWidth(group)+16 {
		metadata += " · " + group
	}
	metadataWidth := ansi.StringWidth(metadata)
	row = ansi.Truncate(row, max(1, width-metadataWidth-1), "…")
	return row + strings.Repeat(" ", max(1, width-metadataWidth-ansi.StringWidth(row))) + metadata
}

func modelRowContext(model models.Model) string {
	if model.ContextWindow <= 0 {
		return "—"
	}
	label := compactModelTokens(model.ContextWindow)
	if model.ContextWindow >= 1024*1024 {
		label = fmt.Sprintf("%.1fM", float64(model.ContextWindow)/(1024*1024))
	}
	if model.ContextEstimated {
		label = "~" + label
	}
	return label
}

func modelDetailLinesActiveContextModelTargetAgentCoder(m *modelDialogCmp, selected *models.Model, effective *string) {
	if selected.ID == m.activeContextModel && m.target == config.AgentCoder {
		(*effective) = config.Text("Efetivo: ainda não informado", "Effective: not reported yet")
		if m.activeContextWindow > 0 {
			(*effective) = fmt.Sprintf(config.Text("Efetivo: %s", "Effective: %s"), tokenLabel(m.activeContextWindow))
			if selected.ContextWindow > 0 && m.activeContextWindow != selected.ContextWindow {
				(*effective) += config.Text(" · limite em uso", " · current request limit")
			}
			if m.activeContextSource == "ollama:loaded" || m.activeContextSource == "lmstudio:loaded" {
				(*effective) += config.Text(" · alocado no servidor", " · server allocation")
			}
		}
	}
}

func modelDetailLinesDetails(m *modelDialogCmp, context string, effective string, tools string) (extractedResult0 []string, handled bool) {
	if !m.details {
		selected := m.filtered[m.selectedIdx]
		if selected.ContextWindow > 0 && selected.ID == m.activeContextModel && m.target == config.AgentCoder && selected.ContextWindow == m.activeContextWindow {
			context = fmt.Sprintf(config.Text("Contexto em uso: %s", "Context in use: %s"), compactModelTokens(selected.ContextWindow))
			// Tools and images sit in the row itself; Ctrl+D has the rest.
			return []string{context}, true
		}
		return []string{context, effective}, true
	}
	return extractedResult0, false
}

func compactModelTokens(n int64) string {
	if n >= 1024 && n%1024 == 0 {
		return fmt.Sprintf("%dk", n/1024)
	}
	return fmt.Sprint(n)
}

func modelHostingLabel(model models.Model) string {
	switch model.Hosting {
	case "local":
		return "Local"
	case "cloud":
		return config.Text("Nuvem", "Cloud")
	}
	// Ollama names its hosted models "name:cloud" or "name-cloud".
	if name := strings.ToLower(model.APIModel); strings.HasSuffix(name, ":cloud") || strings.HasSuffix(name, "-cloud") {
		return config.Text("Nuvem", "Cloud")
	}
	return ""
}

func modelHostingDetailLines(model models.Model) []string {
	if hosting := modelHostingLabel(model); hosting != "" {
		return []string{config.Text("Execução: ", "Hosting: ") + hosting + config.Text(" · informada pelo provedor", " · reported by provider")}
	}
	return nil
}

func catalogViewScrollOffsetModelIDActive(active models.ModelID, model *models.Model, prefs *config.Preferences, group *string) {
	if model.ID == active {
		(*group) = config.Text("Em uso", "In use")
	} else if containsModel(prefs.FavoriteModels, model.ID) {
		(*group) = config.Text("Favoritos", "Favorites")
	} else if containsModel(prefs.RecentModels, model.ID) {
		(*group) = config.Text("Recentes", "Recent")
	}
}

func catalogViewFiltered(m *modelDialogCmp, line func(s string) string, rows *[]string) {
	if len(m.filtered) == 0 {
		empty := config.Text("Nenhum modelo encontrado", "No models found")
		if m.loading {
			empty = config.Text("Aguarde o catálogo…", "Waiting for catalog…")
		} else if m.catalogFailed {
			empty = config.Text("Lista vazia: consulta falhou", "Empty list: query failed")
		}
		(*rows) = append((*rows), line(empty))
	}
}

// modelRowCapabilities tags a row with what the provider reports it can do.
func modelRowCapabilities(model models.Model, width int) string {
	if width < 64 {
		return ""
	}
	var tags string
	if model.SupportsTools != nil && *model.SupportsTools {
		tags += config.Text(" · ferramentas", " · tools")
	}
	if model.SupportsAttachments {
		tags += config.Text(" · imagens", " · images")
	}
	return tags
}

// besideDetails joins the list rows with the selected model's details in a
// right-hand column, separated by a rule.
func (m *modelDialogCmp) besideDetails(left []string, leftWidth, rightWidth int) []string {
	t := theme.CurrentTheme()
	var right []string
	if m.selectedIdx >= 0 && m.selectedIdx < len(m.filtered) {
		model := m.filtered[m.selectedIdx]
		right = append(right, styles.PanelTitle().Render(ansi.Truncate(m.modelLabel(model), rightWidth, "…")))
		// Capability tags, as the provider reports them.
		tag := func(text string, c color.Color) string {
			return dialogBody().Foreground(styles.ReadableForeground(c, t.BackgroundSecondary())).Render("[" + text + "]")
		}
		var tags []string
		if model.SupportsTools != nil && *model.SupportsTools {
			tags = append(tags, tag(config.Text("ferramentas", "tools"), t.Success()))
		}
		if model.CanReason {
			tags = append(tags, tag(config.Text("raciocínio", "reasoning"), t.Info()))
		}
		if model.SupportsAttachments {
			tags = append(tags, tag(config.Text("imagens", "images"), t.Accent()))
		} else {
			tags = append(tags, tag(config.Text("sem imagens", "no images"), t.TextMuted()))
		}
		if hosting := modelHostingLabel(model); hosting != "" {
			tags = append(tags, tag(hosting, t.TextMuted()))
		}
		right = append(right, strings.Join(tags, dialogBody().Render(" ")), "")
	}
	// "Label: value" details read as a key-value list.
	keyWidth := 13
	for _, detail := range m.modelDetailLines() {
		key, value, ok := strings.Cut(interfaceCopy(detail), ": ")
		if !ok || ansi.StringWidth(key) > keyWidth {
			key, value = "", interfaceCopy(detail)
		}
		valueWidth := max(1, rightWidth-keyWidth-1)
		for i, wrapped := range strings.Split(ansi.Wrap(value, valueWidth, ""), "\n") {
			name := ""
			if i == 0 {
				name = key
			}
			name += strings.Repeat(" ", max(0, keyWidth-ansi.StringWidth(name)))
			right = append(right, styles.PanelHint().Render(name+" ")+dialogBody().Render(wrapped))
		}
	}
	right = append(right, "", dialogBody().Render(config.Text("[Usar] Enter", "[Use] Enter"))+styles.PanelHint().Render(config.Text(" · Ctrl+E raciocínio · Ctrl+T testar", " · Ctrl+E reasoning · Ctrl+T probe")))
	return besideColumns(left, right, leftWidth)
}

// besideColumns joins two row lists side by side with a vertical rule.
func besideColumns(left, right []string, leftWidth int) []string {
	fill := dialogBody()
	rule := dialogBody().Foreground(theme.CurrentTheme().BorderNormal()).Render(" │ ")
	rows := make([]string, 0, max(len(left), len(right)))
	for i := range max(len(left), len(right)) {
		l, r := "", ""
		if i < len(left) {
			l = ansi.Truncate(left[i], leftWidth, "…")
		}
		if i < len(right) {
			r = right[i]
		}
		gap := fill.Render(strings.Repeat(" ", max(0, leftWidth-ansi.StringWidth(l))))
		rows = append(rows, l+gap+rule+r)
	}
	return rows
}
