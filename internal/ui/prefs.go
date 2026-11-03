package ui

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/fileutil"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/lsp"
	"github.com/raulaguila/opcode/internal/permission"
)

// Settings sections, in the proposal's nav order.
const (
	sectionConnections = iota
	sectionInterface
	sectionWork
	sectionApproval
	sectionShortcuts
	sectionIntegrations
	sectionAdvanced
)

func sectionNames() []string {
	return []string{textFor("Conexões", "Connections"), textFor("Interface", "Interface"), textFor("Trabalho", "Work"), textFor("Aprovação", "Approval"), textFor("Atalhos", "Shortcuts"), textFor("Integrações", "Integrations"), textFor("Avançado", "Advanced")}
}

// inSection says which section lists a preference: run limits are advanced.
func inSection(section int, s config.Setting) bool {
	switch section {
	case sectionInterface:
		return s.Category == "interface" || s.Category == "sessions"
	case sectionWork:
		return s.Category == "execution" && !s.Advanced
	case sectionShortcuts:
		return s.Category == "shortcuts"
	case sectionAdvanced:
		return s.Category == "execution" && s.Advanced
	}
	return false
}

// connection is one configured provider, legacy backend or named profile.
type connection struct {
	id       string
	legacy   bool
	provider models.ModelProvider
	name     string
	raw      config.Provider
}

type probeMsg struct {
	provider models.ModelProvider
	text     string
	ok       bool
}

// settingsPanel is the proposal's settings: sections on the left, the
// section's connections or preferences on the right, saved on change.
type settingsPanel struct {
	section, selected int
	inside            bool // ←→ move between the menu and the section's items
	query             string
	editing           bool
	draft             string
	deleting          bool
	status            string // "✓ salvo" or the last error
	failed            bool
	values            map[string]string
	origins           map[string]string
	probes            map[models.ModelProvider]probeMsg
	active            models.ModelProvider // the connection in use leads the list
	detected          []lsp.Detection      // installed language servers this project could use
	top               int                  // the first preference in view, when they do not all fit
}

func (m *Model) openSettingsAt(section int) tea.Cmd {
	p := &settingsPanel{section: section, inside: true, probes: map[models.ModelProvider]probeMsg{}}
	if m.app != nil && m.app.CoderAgent != nil {
		p.active = m.app.CoderAgent.Model().Provider
	}
	p.reload()
	m.dialog = p
	// Each card shows its catalog size and latency, as the proposal does.
	var probes []tea.Cmd
	for _, c := range connections(p.active) {
		if !c.raw.Disabled && !keyMissing(c) {
			probes = append(probes, p.probe(c))
		}
	}
	return tea.Batch(probes...)
}

// keyMissing is a connection that names a key variable that is not set.
func keyMissing(c connection) bool {
	return config.KeyMissing(c.provider) || (!c.raw.Disabled && c.raw.APIKeyEnv != "" && os.Getenv(c.raw.APIKeyEnv) == "" && c.raw.APIKey == "" && c.raw.APIKeyRef == "")
}

// openSettings starts on the menu; openSettingsAt goes into a section.
func (m *Model) openSettings() tea.Cmd {
	cmd := m.openSettingsAt(sectionConnections)
	m.dialog.(*settingsPanel).inside = false
	return cmd
}

func (p *settingsPanel) reload() {
	p.detected = nil
	if cfg := config.Get(); cfg != nil {
		p.detected = lsp.Detect(cfg.WorkingDir, cfg.LSP)
	}
	p.values, p.origins = map[string]string{}, map[string]string{}
	for _, v := range config.EffectiveConfiguration() {
		p.values[v.Path], p.origins[v.Path] = fmt.Sprint(v.Value), v.Origin
	}
}

func connections(active models.ModelProvider) []connection {
	cfg := config.Get()
	if cfg == nil {
		return nil
	}
	var out []connection
	for id, raw := range cfg.Providers {
		out = append(out, connection{id: string(id), legacy: true, provider: id, name: providerName(id), raw: raw})
	}
	for id, profile := range cfg.ProviderProfiles {
		name := profile.Name
		if name == "" {
			name = id
		}
		out = append(out, connection{id: id, provider: models.ModelProvider("profile:" + id), name: name, raw: profile.Provider})
	}
	slices.SortFunc(out, func(a, b connection) int {
		if (a.provider == active) != (b.provider == active) {
			return boolRank(b.provider == active) - boolRank(a.provider == active)
		}
		if a.raw.Disabled != b.raw.Disabled {
			return boolRank(a.raw.Disabled) - boolRank(b.raw.Disabled)
		}
		return strings.Compare(a.name, b.name)
	})
	return out
}

// settingRows are the section's preferences, filtered by the search.
func (p *settingsPanel) settingRows() []config.Setting {
	var out []config.Setting
	query := strings.ToLower(strings.TrimSpace(p.query))
	for _, s := range config.UserSettings {
		if !inSection(p.section, s) {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(s.Label+" "+s.Path), query) {
			out = append(out, s)
		}
	}
	return out
}

func (p *settingsPanel) count() int {
	switch p.section {
	case sectionConnections:
		return len(connections(p.active))
	case sectionIntegrations:
		return len(p.detected) + 1 // the servers found, then the integrations form
	case sectionApproval:
		return 1
	}
	return len(p.settingRows())
}

// localLabel picks the language half of "Idioma / Language".
func localLabel(label string) string {
	pt, en, ok := strings.Cut(label, " / ")
	if !ok {
		return label
	}
	return textFor(pt, en)
}

// settingOptions reads "a, b, c" choices from a setting's help; numeric and
// free-text settings have none.
func settingOptions(s config.Setting) []string {
	help, _, _ := strings.Cut(s.Help, " (")
	options := strings.Split(help, ", ")
	for _, option := range options {
		if option == "" || strings.ContainsAny(option, " –+") {
			return nil
		}
	}
	if len(options) < 2 {
		return nil
	}
	return options
}

// optionLabel is a choice in the reader's words.
func optionLabel(value string) string {
	switch value {
	case "true":
		return textFor("Sim", "Yes")
	case "false":
		return textFor("Não", "No")
	case "pt":
		return "Português"
	case "en":
		return "English"
	case "auto":
		return "Auto"
	case "shown":
		return textFor("Sempre", "Always")
	case "hidden":
		return textFor("Nunca", "Never")
	case "off":
		return textFor("Desligado", "Off")
	case "bell":
		return textFor("sino", "bell")
	case "ask", "plan", "execute":
		return modeLabel(value)
	}
	return value
}

// settingHelp is the line under a preference.
func settingHelp(s config.Setting) string {
	switch s.Path {
	case "tui.language":
		return textFor("Textos da interface e respostas", "Interface copy and replies")
	case "tui.proseWidth":
		return textFor("0 usa a largura toda do terminal", "0 uses the full terminal width")
	case "tui.sidebar":
		return textFor("Auto aparece a partir de 140 colunas", "Auto appears from 140 columns")
	case "tui.notifications":
		return textFor("Ao terminar uma tarefa longa ou pedir aprovação", "When a long task ends or asks for approval")
	case "preferences.showProcess":
		return textFor("Abre as ferramentas enquanto o modelo trabalha", "Opens the tools while the model works")
	case "preferences.resumeLast":
		return textFor("Abre a última conversa ao iniciar", "Opens the last conversation at start")
	case "runtime.mode":
		return textFor("Modo das novas conversas", "Mode for new conversations")
	case "runtime.maxSteps":
		return textFor("Etapas por resposta · 1–200", "Steps per reply · 1–200")
	case "runtime.maxSeconds":
		return textFor("Segundos por resposta · 1–7200", "Seconds per reply · 1–7200")
	case "runtime.toolBudget":
		return textFor("Bytes de saída por ferramenta · 4096–1048576", "Output bytes per tool · 4096–1048576")
	case "runtime.firstTokenTimeout":
		return textFor("Segundos até o primeiro token · 1–1800", "Seconds to the first token · 1–1800")
	case "runtime.idleTimeout":
		return textFor("Segundos sem resposta do modelo · 1–600", "Seconds without model output · 1–600")
	case "runtime.embeddingModel":
		return textFor("Modelo de embeddings no Ollama local, ex.: nomic-embed-text · vazio desliga", "Embedding model in the local Ollama, e.g. nomic-embed-text · empty turns it off")
	case "runtime.webSearch":
		return textFor("Busca externa; cada destino ainda pede aprovação de rede", "External search; each destination still asks for network approval")
	}
	if s.Category == "shortcuts" {
		return textFor("Ctrl ou Alt com uma letra · Enter edita", "Ctrl or Alt with a letter · Enter edits")
	}
	return s.Help
}

func (p *settingsPanel) view(m *Model) string {
	width := min(112, m.width-4)
	global, _ := config.Paths(config.WorkingDirectory())
	box := Box{Title: textFor("Configurações", "Settings"), Meta: fileutil.HomeRelative(global), Width: width, PadX: 0, PadY: 1}
	inner := box.Inner()
	navWidth := 22
	content := inner - navWidth - 5 // two columns of air before the frame
	nav := p.navRows(navWidth)
	var body []string
	switch p.section {
	case sectionConnections:
		body = p.connectionRows(m, content)
	case sectionIntegrations:
		body = p.integrationRows(content)
	case sectionApproval:
		body = p.approvalRows(m, content)
	default:
		body = p.preferenceRows(content, max(8, m.height-8))
	}
	height := max(len(nav), len(body), min(18, max(8, m.height-12)))
	rows := make([]string, 0, height+3)
	rule := on(P.Surface, P.Rule).Render("│")
	for i := range height {
		left, right := "", ""
		if i < len(nav) {
			left = nav[i]
		}
		if i < len(body) {
			right = body[i]
		}
		rows = append(rows, Line(left, navWidth, P.Surface)+rule+on(P.Surface, P.Surface).Render("  ")+Line(right, content, P.Surface)+on(P.Surface, P.Surface).Render("  "))
	}
	rows = append(rows, "")
	for _, row := range KeyRow(p.legend(), inner-4, P.Surface) {
		rows = append(rows, on(P.Surface, P.Surface).Render("  ")+row)
	}
	return box.Render(strings.Join(rows, "\n"))
}

func (p *settingsPanel) navRows(width int) []string {
	counts := map[int]string{}
	if n := len(connections(p.active)); n > 0 {
		counts[sectionConnections] = fmt.Sprint(n)
	}
	var rows []string
	for i, name := range sectionNames() {
		bg, fg, bar := P.Surface, P.Text, on(P.Surface, P.Surface).Render("  ")
		if i == p.section {
			bg, fg, bar = P.Selected, P.Strong, on(P.Selected, P.Ask).Render("▌ ")
			if p.inside { // the menu waits while you are in the section
				fg, bar = P.Muted, on(P.Selected, P.Muted).Render("▌ ")
			}
		}
		count := on(bg, P.Muted).Render(counts[i] + " ")
		if n := integrationWarnings(); i == sectionIntegrations && n > 0 {
			count = on(bg, P.Plan).Render(fmt.Sprintf(textFor("%d aviso", "%d warning"), n))
			if n > 1 {
				count = on(bg, P.Plan).Render(fmt.Sprintf(textFor("%d avisos", "%d warnings"), n))
			}
			count += on(bg, bg).Render(" ")
		}
		rows = append(rows, Spread(bar+on(bg, fg).Bold(i == p.section).Render(name), count, width, bg))
	}
	return rows
}

func (p *settingsPanel) legend() string {
	switch {
	case p.deleting:
		return textFor("Enter excluir · Esc cancelar", "Enter delete · Esc cancel")
	case p.editing:
		return textFor("Enter salvar · Esc cancelar", "Enter save · Esc cancel")
	case !p.inside:
		return textFor("↑↓ seção · → entrar · Esc fechar", "↑↓ section · → enter · Esc close")
	case p.section == sectionConnections:
		return textFor("N nova · Enter editar · T testar · D excluir · ← menu · Esc fechar", "N new · Enter edit · T test · D delete · ← menu · Esc close")
	case p.section == sectionApproval:
		return textFor("→ política · ← menu · Esc fechar", "→ policy · ← menu · Esc close")
	case p.section == sectionIntegrations:
		if len(p.detected) > 0 {
			return textFor("↑↓ escolher · Enter ativar · ← menu · Esc fechar", "↑↓ choose · Enter turn on · ← menu · Esc close")
		}
		return textFor("Enter abrir integrações · ← menu · Esc fechar", "Enter open integrations · ← menu · Esc close")
	}
	saved := ""
	if p.status != "" && !p.failed {
		saved = " · " + p.status
	}
	return textFor("↑↓ preferência · → valor · ← menu"+saved+" · Esc fechar", "↑↓ preference · → value · ← menu"+saved+" · Esc close")
}

// connectionRows draws each connection as a framed card: state, name and
// status on top, address and authentication, then the actions that fit its
// state. A disabled connection is a one-line dashed card.
func (p *settingsPanel) connectionRows(m *Model, width int) []string {
	active := p.active
	var rows []string
	list := connections(p.active)
	if len(list) == 0 {
		rows = append(rows, on(P.Surface, P.Muted).Render(textFor("Nenhuma conexão ainda.", "No connections yet.")), "")
	}
	for i, c := range list {
		edge, bg := P.CardEdge, P.Surface
		if i == p.selected {
			edge, bg = P.CardSelectedEdge, P.CardSelected
		}
		inner := width - 4                      // thin edges and a cell of air on each side
		off := c.raw.Disabled && !keyMissing(c) // a missing key loads off but keeps its full card
		icon, iconColor, state, stateColor := "●", P.Success, "", P.Muted
		switch {
		case keyMissing(c):
			icon, iconColor, state, stateColor = "▲", P.Plan, textFor("chave ausente", "key missing"), P.Plan
		case c.raw.Disabled:
			icon, iconColor, state = "○", P.Faint, textFor("desativada", "disabled")
		case c.provider == active:
			state = textFor("em uso", "in use")
		}
		name := on(bg, P.Strong).Bold(true).Render(c.name)
		if off {
			name = on(bg, P.Muted).Render(c.name)
		}
		pad := on(bg, bg).Render(" ")
		indent := on(bg, bg).Render("    ")
		body := []string{pad + Spread(on(bg, iconColor).Render(icon)+on(bg, bg).Render("   ")+name, on(bg, stateColor).Render(state), inner, bg)}
		deleting := i == p.selected && p.deleting
		if !off {
			detail := on(bg, P.Muted).Render(connectionDetail(c))
			if probe, ok := p.probes[c.provider]; ok {
				color := P.Muted
				if !probe.ok {
					color = P.Danger
				}
				detail += on(bg, P.Muted).Render(" · ") + on(bg, color).Render(probe.text)
			}
			body = append(body, pad+indent+detail)
			labels := []string{textFor("Editar", "Edit"), textFor("Testar", "Test"), textFor("Modelos", "Models")}
			if keyMissing(c) {
				labels = []string{textFor("Editar", "Edit"), textFor("Desativar", "Disable")}
			}
			if deleting {
				body = append(body, pad+indent+on(bg, P.Danger).Render(textFor("Excluir esta conexão? Enter confirma · Esc cancela", "Delete this connection? Enter confirms · Esc cancels")))
			} else {
				for _, row := range OutlineButtons(labels, P.Border, P.Strong, bg) {
					body = append(body, pad+on(bg, bg).Render("   ")+row)
				}
			}
		} else if deleting {
			body = append(body, pad+indent+on(bg, P.Danger).Render(textFor("Excluir esta conexão? Enter confirma · Esc cancela", "Delete this connection? Enter confirms · Esc cancels")))
		}
		card := Box{Width: width, Accent: edge, Background: bg, Outer: P.Surface, Dashed: off}
		rows = append(rows, strings.Split(card.Render(strings.Join(body, "\n")), "\n")...)
	}
	if p.status != "" && p.failed {
		rows = append(rows, on(P.Surface, P.Danger).Render(ansi.Truncate(p.status, width, "…")))
	}
	offer := on(P.Surface, P.Ask).Render(" " + textFor("+ Nova conexão · Ollama, OpenAI, Anthropic, Gemini, OpenRouter…", "+ New connection · Ollama, OpenAI, Anthropic, Gemini, OpenRouter…"))
	add := Box{Width: width, Accent: P.CardSelectedEdge, Background: P.Surface, Outer: P.Surface, Dashed: true}
	return append(rows, strings.Split(add.Render(offer), "\n")...)
}

// integrationWarnings counts integrations that fail validation or were
// turned off for lacking a command.
func integrationWarnings() int {
	cfg := config.Get()
	if cfg == nil {
		return 0
	}
	n := 0
	for name, lsp := range cfg.LSP {
		if config.ValidateIntegration(config.Integration{Kind: "lsp", Name: name, LSP: lsp}) != nil || lsp.Command == "" {
			n++
		}
	}
	for name, mcp := range cfg.MCPServers {
		if config.ValidateIntegration(config.Integration{Kind: "mcp", Name: name, MCP: mcp}) != nil {
			n++
		}
	}
	return n
}

// approvalRows offer the policy of this conversation; it is not saved.
func (p *settingsPanel) approvalRows(m *Model, width int) []string {
	options := []struct {
		label  string
		policy permission.ApprovalPolicy
	}{{textFor("Pedir", "Ask"), permission.ApprovalPrompt}, {textFor("Editar no projeto", "Project edits"), permission.ApprovalProjectEdits}}
	var control string
	for _, option := range options {
		if option.policy == m.policy {
			control += on(P.Segment, P.Strong).Bold(true).Render(" " + option.label + " ")
		} else {
			control += on(P.Raised, P.Muted).Render(" " + option.label + " ")
		}
	}
	bg := P.Selected
	return []string{
		on(P.Surface, P.Ask).Render("▌") + Spread(on(bg, P.Strong).Render(" "+textFor("Política desta conversa", "This conversation's policy")), control+on(bg, bg).Render(" "), width-1, bg),
		on(P.Surface, P.Ask).Render("▌") + Line(on(bg, P.Muted).Render(" "+textFor("Editar no projeto aprova edições de arquivos do projeto sem perguntar", "Project edits approves edits to project files without asking")), width-1, bg),
		"",
		on(P.Surface, P.Muted).Render(ansi.Truncate(textFor("Comandos, rede, exclusões e caminhos fora do projeto continuam pedindo aprovação.", "Commands, network, deletions and outside paths still ask for approval."), width, "…")),
	}
}

// connectionDetail is a card's second line: address and authentication.
func connectionDetail(c connection) string {
	address := c.raw.BaseURL
	if address == "" {
		address = textFor("endereço padrão do provedor", "provider default address")
	}
	auth := textFor("sem autenticação", "no authentication")
	switch {
	case c.raw.APIKeyRef != "":
		auth = textFor("chave no cofre do sistema", "key in system vault")
	case c.raw.APIKeyEnv != "":
		auth = c.raw.APIKeyEnv
		if os.Getenv(c.raw.APIKeyEnv) == "" {
			auth += textFor(" não definida", " not set")
		}
	case c.raw.APIKey != "":
		auth = textFor("chave no arquivo de configuração", "key in config file")
	}
	return address + " · " + auth
}

func (p *settingsPanel) integrationRows(width int) []string {
	cfg := config.Get()
	rows := []string{on(P.Surface, P.Strong).Bold(true).Render(textFor("Servidores de linguagem e MCP", "Language servers and MCP")), ""}
	if cfg != nil {
		for name := range cfg.LSP {
			rows = append(rows, on(P.Surface, P.Success).Render("● ")+on(P.Surface, P.Text).Render(name)+on(P.Surface, P.Muted).Render("  LSP"))
		}
		for name := range cfg.MCPServers {
			rows = append(rows, on(P.Surface, P.Success).Render("● ")+on(P.Surface, P.Text).Render(name)+on(P.Surface, P.Muted).Render("  MCP"))
		}
	}
	if len(rows) == 2 {
		rows = append(rows, on(P.Surface, P.Muted).Render(textFor("Nenhuma integração configurada.", "No integrations configured.")))
	}
	if len(p.detected) > 0 {
		rows = append(rows, "", on(P.Surface, P.Strong).Bold(true).Render(textFor("Encontrados neste computador", "Found on this computer")))
		for i, d := range p.detected {
			cursor, name := "  ", on(P.Surface, P.Text).Render(d.Name)
			if p.inside && i == p.selected {
				cursor, name = on(P.Surface, P.Ask).Render("› "), on(P.Surface, P.Strong).Bold(true).Render(d.Name)
			}
			rows = append(rows, cursor+on(P.Surface, P.Faint).Render("○ ")+name+on(P.Surface, P.Muted).Render("  "+strings.Join(d.Languages, ", ")))
		}
		rows = append(rows, on(P.Surface, P.Muted).Render(textFor("Enter ativa; o servidor liga ao abrir um arquivo da linguagem.", "Enter turns it on; it starts when a file of its language is opened.")))
	}
	if p.status != "" {
		color := P.Success
		if p.failed {
			color = P.Danger
		}
		rows = append(rows, "", on(P.Surface, color).Render(p.status))
	}
	return append(rows, "", Button(textFor("Abrir integrações · Enter", "Open integrations · Enter"), !p.inside || p.selected == len(p.detected), P.Ask))
}

// enableDetected saves a found server as an LSP integration, as the form
// would with its preset command and arguments.
func (p *settingsPanel) enableDetected(d lsp.Detection) {
	err := config.SaveIntegration(config.Integration{Kind: "lsp", Name: d.Name, LSP: config.LSPConfig{Command: d.Command, Args: d.Args}})
	if err != nil {
		p.status, p.failed = err.Error(), true
		return
	}
	p.reload()
	p.selected = min(p.selected, len(p.detected))
	p.status, p.failed = fmt.Sprintf(textFor("✓ %s ativo", "✓ %s on"), d.Name), false
}

// preferenceRows are the search field and two lines per preference: the
// name with its value, then what it does; a rule separates them, and the
// list scrolls to keep the selected one within limit rows.
func (p *settingsPanel) preferenceRows(width, limit int) []string {
	field := on(P.Field, P.Ask).Render(" ⌕ ") + on(P.Field, P.Strong).Render(p.query) + on(P.Field, P.Ask).Render("▏")
	if p.query == "" {
		field = on(P.Field, P.Ask).Render(" ⌕ ") + on(P.Field, P.Faint).Render(textFor("Buscar preferência", "Search preferences"))
	}
	rows := []string{Line(field, width, P.Field), ""}
	settings := p.settingRows()
	if len(settings) == 0 {
		return append(rows, on(P.Surface, P.Muted).Render(empty()))
	}
	fit := max(1, (limit-1)/3)
	p.top = max(0, min(max(p.top, p.selected-fit+1), p.selected, len(settings)-fit))
	for i, s := range settings[p.top:min(len(settings), p.top+fit)] {
		i += p.top
		if i > p.top {
			rows = append(rows, on(P.Surface, P.Rule).Render(strings.Repeat("─", width)))
		}
		bg := P.Surface
		bar := on(P.Surface, P.Surface).Render(" ")
		if i == p.selected {
			bg, bar = P.Selected, on(P.Surface, P.Ask).Render("▌")
		}
		value := p.valueView(s, i == p.selected, bg)
		rows = append(rows,
			bar+Spread(on(bg, P.Strong).Render(" "+localLabel(s.Label)), value+on(bg, bg).Render(" "), width-1, bg),
			bar+Line(on(bg, P.Muted).Render(" "+settingHelp(s)), width-1, bg))
	}
	if p.failed {
		rows = append(rows, "", on(P.Surface, P.Danger).Render(ansi.Truncate(p.status, width, "…")))
	}
	return rows
}

// valueView is a segmented control for two or three choices, "value ▾" for
// more, and the number or text otherwise.
func (p *settingsPanel) valueView(s config.Setting, selected bool, bg color.Color) string {
	value := p.values[s.Path]
	if selected && p.editing {
		return on(P.Field, P.Strong).Render(" "+p.draft) + on(P.Field, P.Ask).Render("▏")
	}
	if p.origins[s.Path] == "environment" {
		return on(bg, P.Faint).Render(value + textFor(" · variável de ambiente", " · environment variable"))
	}
	options := settingOptions(s)
	if len(options) == 0 || len(options) > 3 {
		label := optionLabel(value)
		if label == "" {
			label = "—"
		}
		if len(options) > 3 {
			label += " ▾"
		}
		return on(bg, P.Strong).Render(label)
	}
	var out string
	for _, option := range options {
		if option == value {
			out += on(P.Segment, P.Strong).Bold(true).Render(" " + optionLabel(option) + " ")
		} else {
			out += on(P.Raised, P.Muted).Render(" " + optionLabel(option) + " ")
		}
	}
	return out
}

func (p *settingsPanel) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	switch {
	case p.editing:
		return p.editKey(m, key, msg.Text), true
	case p.deleting:
		return p.deleteKey(m, key), true
	}
	switch key {
	case "esc":
		if p.query != "" {
			p.query, p.selected = "", 0
			return nil, true
		}
		m.dialog = nil
		return nil, true
	case "tab", "shift+tab":
		step := 1
		if key == "shift+tab" {
			step = -1
		}
		p.section = (p.section + step + len(sectionNames())) % len(sectionNames())
		p.selected, p.query, p.status, p.failed = 0, "", "", false
		return nil, true
	}
	if !p.inside {
		switch key {
		case "up", "down":
			step := 1
			if key == "up" {
				step = -1
			}
			p.section = (p.section + step + len(sectionNames())) % len(sectionNames())
			p.selected, p.query, p.status, p.failed = 0, "", "", false
		case "right", "enter", "space":
			p.inside = true
		}
		return nil, true
	}
	switch key {
	case "left":
		p.inside = false
		return nil, true
	case "up", "down":
		if n := p.count(); n > 0 {
			step := 1
			if key == "up" {
				step = -1
			}
			p.selected = (p.selected + step + n) % n
		}
		return nil, true
	}
	switch p.section {
	case sectionConnections:
		return p.connectionKey(m, key), true
	case sectionIntegrations:
		if key == "enter" || key == "space" {
			if p.selected < len(p.detected) {
				p.enableDetected(p.detected[p.selected])
				return nil, true
			}
			return m.openIntegrations(), true
		}
		return nil, true
	case sectionApproval:
		if key == "right" || key == "space" || key == "enter" {
			if m.policy == permission.ApprovalProjectEdits {
				m.setApproval(permission.ApprovalPrompt)
			} else {
				m.setApproval(permission.ApprovalProjectEdits)
			}
		}
		return nil, true
	}
	return p.preferenceKey(m, key, msg.Text), true
}

func (p *settingsPanel) paste(text string) {
	if p.editing {
		p.draft += text
	} else if p.section != sectionConnections && p.section != sectionIntegrations {
		p.query += text
		p.selected = 0
	}
}

func (p *settingsPanel) current() (connection, bool) {
	list := connections(p.active)
	if p.selected < 0 || p.selected >= len(list) {
		return connection{}, false
	}
	return list[p.selected], true
}

func (p *settingsPanel) connectionKey(m *Model, key string) tea.Cmd {
	c, ok := p.current()
	switch key {
	case "n":
		return m.openProviderForm("")
	case "enter", "e":
		if !ok {
			return m.openProviderForm("")
		}
		return m.openProviderForm(c.provider)
	case "t":
		if ok {
			return p.probe(c)
		}
	case "a":
		if ok {
			p.setDisabled(m, c, !c.raw.Disabled)
		}
	case "m":
		if ok && !c.raw.Disabled {
			cmd := m.openModels()
			if picker, isPicker := m.dialog.(*modelPicker); isPicker && slices.Contains(picker.providers, c.provider) {
				picker.provider = c.provider
				return picker.load()
			}
			return cmd
		}
	case "d", "delete":
		if ok {
			p.deleting = true
		}
	}
	return nil
}

func (p *settingsPanel) deleteKey(m *Model, key string) tea.Cmd {
	switch key {
	case "esc":
		p.deleting = false
	case "enter":
		p.deleting = false
		c, ok := p.current()
		if !ok {
			return nil
		}
		if err := m.saveGuard(); err != nil {
			p.status, p.failed = err.Error(), true
			return nil
		}
		if err := config.DeleteProvider(c.id, c.legacy); err != nil {
			p.status, p.failed = err.Error(), true
			return nil
		}
		p.status, p.failed = "", false
		p.selected = max(0, min(p.selected, len(connections(p.active))-1))
		m.info = textFor("Conexão excluída", "Connection deleted")
	}
	return nil
}

// setDisabled turns a connection off or back on. The stored key is written
// back as it was in the file, never a key resolved from the environment.
func (p *settingsPanel) setDisabled(m *Model, c connection, disabled bool) {
	if m.app != nil && m.app.CoderAgent != nil && disabled && m.app.CoderAgent.Model().Provider == c.provider {
		p.status, p.failed = textFor("Troque o modelo antes de desativar a conexão em uso", "Switch models before disabling the connection in use"), true
		return
	}
	if err := m.saveGuard(); err != nil {
		p.status, p.failed = err.Error(), true
		return
	}
	var err error
	if c.legacy {
		raw := config.Get().Providers[c.provider]
		raw.Disabled, raw.APIKey = disabled, config.StoredProviderKey(c.id, true)
		err = config.SaveProvider(c.provider, raw)
	} else {
		profile := config.Get().ProviderProfiles[c.id]
		profile.Disabled, profile.APIKey = disabled, config.StoredProviderKey(c.id, false)
		err = config.SaveProviderProfile(c.id, profile)
	}
	if err != nil {
		p.status, p.failed = err.Error(), true
		return
	}
	p.status, p.failed = "", false
	m.info = textFor("Conexão desativada", "Connection disabled")
	if !disabled {
		m.info = textFor("Conexão ativada", "Connection enabled")
	}
}

// probe asks the connection's catalog, as the proposal's "Testar" does.
func (p *settingsPanel) probe(c connection) tea.Cmd {
	fetch := config.ModelCatalog(c.provider)
	if !c.legacy {
		fetch = config.ProfileModelCatalog(c.id)
	}
	p.probes[c.provider] = probeMsg{provider: c.provider, text: textFor("testando…", "testing…"), ok: true}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		start := time.Now()
		found, err := fetch(models.WithoutCatalogMetadata(ctx))
		if err != nil {
			// The card has room for a word; the full reason shows when it is tested by hand in its form.
			return probeMsg{provider: c.provider, text: textFor("sem resposta", "no response")}
		}
		return probeMsg{provider: c.provider, text: fmt.Sprintf(textFor("%d modelos · %d ms", "%d models · %d ms"), len(found), time.Since(start).Milliseconds()), ok: true}
	}
}

func (p *settingsPanel) preferenceKey(m *Model, key, text string) tea.Cmd {
	settings := p.settingRows()
	if p.selected >= len(settings) {
		if len(text) > 0 && key != "space" {
			p.query += text
			p.selected = 0
		} else if key == "backspace" && p.query != "" {
			p.query = p.query[:len(p.query)-1]
		}
		return nil
	}
	s := settings[p.selected]
	options := settingOptions(s)
	switch key {
	case "right", "space":
		if len(options) == 0 {
			return nil
		}
		i := slices.Index(options, p.values[s.Path])
		p.save(m, s, options[(i+1)%len(options)])
	case "enter":
		if len(options) > 0 {
			i := slices.Index(options, p.values[s.Path])
			p.save(m, s, options[(i+1)%len(options)])
			return nil
		}
		p.editing, p.draft = true, p.values[s.Path]
	case "backspace":
		if r := []rune(p.query); len(r) > 0 {
			p.query, p.selected = string(r[:len(r)-1]), 0
		}
	default:
		if text != "" {
			p.query += text
			p.selected = 0
		}
	}
	return nil
}

func (p *settingsPanel) editKey(m *Model, key, text string) tea.Cmd {
	switch key {
	case "esc":
		p.editing = false
	case "enter":
		if s := p.settingRows(); p.selected < len(s) {
			p.save(m, s[p.selected], strings.TrimSpace(p.draft))
		}
		p.editing = false
	case "backspace":
		if r := []rune(p.draft); len(r) > 0 {
			p.draft = string(r[:len(r)-1])
		}
	default:
		p.draft += text
	}
	return nil
}

// save writes one preference to its owning file and reads them all back.
func (p *settingsPanel) save(m *Model, s config.Setting, value string) {
	if p.origins[s.Path] == "environment" {
		p.status, p.failed = textFor("Definida por variável de ambiente", "Set by an environment variable"), true
		return
	}
	if err := config.SaveUserSetting(s.Path, value, false); err != nil {
		p.status, p.failed = err.Error(), true
		return
	}
	p.reload()
	p.status, p.failed = textFor("✓ salvo", "✓ saved"), false
	if s.Path == "runtime.mode" && m.session.ID == "" {
		m.mode = value
	}
	m.layout()
}
