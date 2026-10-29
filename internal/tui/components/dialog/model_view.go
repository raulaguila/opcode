package dialog

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

// modelGeometry is shared by drawing and mouse hit testing, including compact
// padding. List coordinates never depend on translated labels or model names.
type modelGeometry struct{ width, height, padY, contentX, titleY, listY, rows int }

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
	helpRows := 1
	if m.details && height >= 18 {
		helpRows += 2
		if len(m.reasoningModel().ReasoningChoices()) > 0 {
			helpRows++
		}
	}
	overhead := 3 + 1 + helpRows + 2 + pad*2 + len(m.modelDetailLines())
	return modelGeometry{width: width, height: height, padY: pad, contentX: 3, titleY: 1 + pad, listY: 4 + pad, rows: max(1, min(numVisibleModels, height-overhead))}
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
	if name == "" {
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
	if selected.ContextWindow > 0 {
		context = fmt.Sprintf(config.Text("Contexto: %s", "Context: %s"), tokenLabel(selected.ContextWindow))
		if contextNote != "" {
			context += " (" + contextNote + ")"
		}
	}
	tools := config.Text("Ferramentas: não informado", "Tools: not reported")
	if selected.SupportsTools != nil {
		if *selected.SupportsTools {
			tools = config.Text("Ferramentas: informadas pelo provedor", "Tools: reported by provider")
		} else {
			tools = config.Text("Ferramentas: não suportadas", "Tools: not supported")
		}
	}
	if !m.details {
		if m.height > 0 && m.height < 18 {
			return []string{context}
		}
		return []string{context, tools}
	}
	rows := []string{"ID: " + selected.APIModel, context, tools}
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
	g := m.geometry()
	t := theme.CurrentTheme()
	base := styles.BaseStyle().Width(g.width).MaxWidth(g.width)
	line := func(s string) string { return base.Render(ansi.Truncate(interfaceCopy(s), g.width, "…")) }
	rows := []string{base.Foreground(t.Primary()).Bold(true).Render(ansi.Truncate(interfaceCopy(m.modelTitle()), g.width, "…"))}
	label := config.Text("Buscar: ", "Search: ")
	m.search.Width = max(1, g.width-ansi.StringWidth(label))
	m.search.Placeholder = config.Text("Nome ou ID", "Name or ID")
	rows = append(rows, base.Render(label+m.search.View()))
	note := m.note
	if m.loading {
		note = config.Text("Carregando modelos…", "Loading provider models…")
	}
	rows = append(rows, line(note))
	active := m.activeModelID()
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
		prefs := config.Get().Preferences
		if containsModel(prefs.FavoriteModels, model.ID) {
			label = "★ " + label
		} else if containsModel(prefs.RecentModels, model.ID) {
			label = "↺ " + label
		}
		group := ""
		if model.ID == active {
			group = config.Text("Atual", "Current")
		} else if containsModel(prefs.FavoriteModels, model.ID) {
			group = config.Text("Favoritos", "Favorites")
		} else if containsModel(prefs.RecentModels, model.ID) {
			group = config.Text("Recentes", "Recent")
		}
		rendered := fmt.Sprintf("%s %s %s", caret, mark, label)
		if g.width >= 48 && group != "" {
			rendered = ansi.Truncate(rendered, g.width-12, "…")
			rendered += strings.Repeat(" ", max(1, g.width-11-ansi.StringWidth(rendered))) + group
		}
		row := base
		if i == m.selectedIdx {
			row = row.Background(t.Primary()).Foreground(t.Background()).Bold(true)
		}
		rows = append(rows, row.Render(ansi.Truncate(interfaceCopy(rendered), g.width, "…")))
	}
	if len(m.filtered) == 0 {
		empty := config.Text("Nenhum modelo encontrado", "No models found")
		if m.loading {
			empty = config.Text("Aguarde o catálogo…", "Waiting for catalog…")
		}
		rows = append(rows, line(empty))
	}
	for _, detail := range m.modelDetailLines() {
		rows = append(rows, line(detail))
	}
	position := config.Text("0 de 0", "0 of 0")
	if len(m.filtered) > 0 {
		position = fmt.Sprintf(config.Text("%d de %d", "%d of %d"), m.selectedIdx+1, len(m.filtered))
	}
	if m.scrollOffset > 0 {
		position += " · ↑"
	}
	if m.scrollOffset+g.rows < len(m.filtered) {
		position += " · ↓"
	}
	position += config.Text(" · ✓ atual", " · ✓ current")
	rows = append(rows, line(position))
	hint := config.Text("Enter escolhe · Ctrl+F ★ · Alt+←/→ provedor · Ctrl+D · Esc", "Enter choose · Ctrl+F ★ · Alt+←/→ provider · Ctrl+D · Esc")
	if g.width < 48 {
		hint = config.Text("↑↓ · Enter · Esc · Ctrl+D", "↑↓ · Enter · Esc · Ctrl+D")
	}
	if len(m.filtered) == 0 && !m.loading {
		hint = config.Text("Ctrl+R atualiza · Esc volta", "Ctrl+R refresh · Esc back")
	}
	rows = append(rows, line(hint))
	if m.details && g.height >= 18 {
		if len(m.reasoningModel().ReasoningChoices()) > 0 {
			rows = append(rows, line(config.Text("Ctrl+E esforço do modelo ativo", "Ctrl+E active model effort")))
		}
		rows = append(rows, line(config.Text("Alt+←/→ provedor · Ctrl+F ★ · Ctrl+R atualiza", "Alt+←/→ provider · Ctrl+F ★ · Ctrl+R refresh")))
		rows = append(rows, line(config.Text("Ctrl+A função · Ctrl+T testar · Ctrl+P conexão", "Ctrl+A role · Ctrl+T probe · Ctrl+P connection")))
	}
	frame := styles.BaseStyle().Padding(g.padY, 2).Border(lipgloss.RoundedBorder()).BorderForeground(t.BorderNormal()).Render(strings.Join(rows, "\n"))
	return lipgloss.NewStyle().MaxWidth(g.width + 6).MaxHeight(g.height).Render(frame)
}

func containsModel(list []models.ModelID, id models.ModelID) bool {
	for _, candidate := range list {
		if candidate == id {
			return true
		}
	}
	return false
}

func tokenLabel(n int64) string {
	if n >= 1024 && n%1024 == 0 {
		return fmt.Sprintf("%dk (%d)", n/1024, n)
	}
	return fmt.Sprint(n)
}
