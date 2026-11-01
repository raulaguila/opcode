package dialog

import (
	"fmt"
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
type modelGeometry struct{ width, height, padY, contentX, titleY, listY, rows, listWidth int }

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
	listWidth, details := width, len(m.modelDetailLines())
	if m.details && width >= 80 {
		listWidth, details = width*9/20, 0
	}
	overhead := 2 + helpRows + 2 + pad*2 + details
	return modelGeometry{width: width, height: height, padY: pad, contentX: 3, titleY: 0, listY: 3 + pad, rows: max(1, min(numVisibleModels, height-overhead)), listWidth: listWidth}
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

func (m *modelDialogCmp) providerLabel() string {
	if id, ok := strings.CutPrefix(string(m.provider), "profile:"); ok {
		p := config.Get().ProviderProfiles[id]
		if p.Name != "" {
			return p.Name
		}
		return id
	}
	switch m.provider {
	case "":
		return config.Text("Sem provedor", "No provider")
	case models.ProviderLocal:
		if config.Get().Providers[m.provider].API == "ollama" {
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
		return string(m.provider)
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
	title += " · " + m.providerLabel()
	if m.setup {
		title = config.Text("Configuração 2/3 · ", "Setup 2/3 · ") + title
	}
	if len(m.availableProviders) > 1 {
		title += fmt.Sprintf(" [%d/%d]", slices.Index(m.availableProviders, m.provider)+1, len(m.availableProviders))
		title = "‹ " + title + " ›"
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
	label := "⌕ "
	m.search.SetWidth(max(1, g.width-ansi.StringWidth(label)))
	m.search.Placeholder = config.Text("Nome ou ID", "Name or ID")
	rows = append(rows, base.Render(label+m.search.View()))
	note := m.note
	if m.loading {
		note = config.Text("Carregando modelos…", "Loading provider models…")
	}
	rows = append(rows, dialogHint(note, g.width))
	active := m.activeModelID()
	catalogViewScrollOffset(m, &g, t, &base, &rows, active)
	catalogViewFiltered(m, line, &rows)
	if g.width == full.width {
		for _, detail := range m.modelDetailLines() {
			rows = append(rows, dialogHint(detail, g.width))
		}
	} else {
		rows = m.besideDetails(rows, g.width, full.width-g.width-3)
	}
	g.width = full.width
	// An empty unfiltered list has no position; the title keeps the room.
	position := ""
	if m.query != "" {
		position = config.Text("0 de 0", "0 of 0")
	}
	if len(m.filtered) > 0 {
		position = fmt.Sprintf(config.Text("%d de %d", "%d of %d"), m.selectedIdx+1, len(m.filtered))
	}
	if m.query != "" {
		position += config.Text(" resultados", " results")
	}
	if m.scrollOffset > 0 {
		position += " · ↑"
	}
	if m.scrollOffset+g.rows < len(m.filtered) {
		position += " · ↓"
	}
	hint := config.Text("Enter escolhe · Tab provedor · Ctrl+P conexão · Ctrl+D detalhes · Esc", "Enter choose · Tab provider · Ctrl+P connection · Ctrl+D details · Esc")
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
	chat := 0
	for _, model := range m.models {
		if !model.EmbeddingOnly() {
			chat++
		}
	}
	return fmt.Sprintf(config.Text("%d catálogo · %d conversa", "%d catalog · %d chat"), len(m.models), chat)
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
	for i := m.scrollOffset; i < min(len(m.filtered), m.scrollOffset+g.rows); i++ {
		model := m.filtered[i]
		caret, mark := " ", " "
		if i == m.selectedIdx {
			caret = "›"
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
		right = append(right, styles.PanelTitle().Render(ansi.Truncate(m.modelLabel(m.filtered[m.selectedIdx]), rightWidth, "…")), "")
	}
	for _, detail := range m.modelDetailLines() {
		for _, wrapped := range strings.Split(ansi.Wrap(interfaceCopy(detail), max(1, rightWidth), ""), "\n") {
			right = append(right, dialogBody().Render(wrapped))
		}
	}
	fill := dialogBody()
	rule := dialogBody().Foreground(t.BorderNormal()).Render(" │ ")
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
