package dialog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

const numVisibleModels = 10

type ModelSelectedMsg struct {
	Model models.Model
	Agent config.AgentName
}
type CloseModelDialogMsg struct{}
type ModelCatalogLoadedMsg struct {
	request uint64
	models  []models.Model
	err     error
}

type ModelDialog interface {
	tea.Model
	layout.Bindings
	Open() tea.Cmd
	Close()
}

type modelDialogCmp struct {
	models, filtered                         []models.Model
	provider                                 models.ModelProvider
	availableProviders                       []models.ModelProvider
	selectedIdx, width, height, scrollOffset int
	query, note                              string
	loading                                  bool
	request                                  uint64
	cancel                                   context.CancelFunc
	target                                   config.AgentName
	probeConfirm, probeRunning               bool
	setup                                    bool
}

var modelKeys = struct {
	Up, Down, Left, Right, Enter, Escape, Refresh key.Binding
}{
	key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "previous model")),
	key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "next model")),
	key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "previous provider")),
	key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "next provider")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select model")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh models")),
}

func (m *modelDialogCmp) Init() tea.Cmd { return nil }
func (m *modelDialogCmp) Open() tea.Cmd {
	m.Close()
	cfg := config.Get()
	m.availableProviders = getEnabledProviders(cfg)
	selected := GetSelectedModel(cfg)
	m.provider = selected.Provider
	if selected.ProviderProfile != "" {
		m.provider = models.ModelProvider("profile:" + selected.ProviderProfile)
	}
	m.target = config.AgentCoder
	if !slices.Contains(m.availableProviders, m.provider) {
		m.provider = ""
		if len(m.availableProviders) > 0 {
			m.provider = m.availableProviders[0]
		}
	}
	m.query = ""
	return m.load()
}
func (m *modelDialogCmp) Close() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.request++ // Discard late results after closing, refreshing or switching providers.
	m.loading = false
	m.probeConfirm = false
	m.probeRunning = false
}
func (m *modelDialogCmp) load() tea.Cmd {
	m.Close()
	m.models = getModelsForProvider(m.provider)
	m.note = "Configured catalog"
	m.filter()
	if m.provider == "" {
		m.note = "Configure a provider in ~/.opcode/config.json"
		return nil
	}
	fetch := config.ModelCatalog(m.provider)
	if id, ok := strings.CutPrefix(string(m.provider), "profile:"); ok {
		fetch = config.ProfileModelCatalog(id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.loading = true
	request := m.request
	return func() tea.Msg {
		found, err := fetch(ctx)
		cancel()
		return ModelCatalogLoadedMsg{request, found, err}
	}
}
func (m *modelDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, handled := m.modelProbeUpdate(msg); handled {
		return m, cmd
	}
	switch msg := msg.(type) {
	case ModelCatalogLoadedMsg:
		if msg.request != m.request {
			return m, nil
		}
		m.loading = false
		m.cancel = nil
		if msg.err == nil {
			m.models = msg.models
			m.note = fmt.Sprintf(config.Text("Catálogo do provedor · %d modelos", "Provider catalog · %d models"), len(m.models))
			m.filter()
		} else if errors.Is(msg.err, models.ErrCatalogUnsupported) {
			m.note = "Configured catalog · live listing unavailable"
		} else {
			m.note = "Could not load models; showing configured catalog. Ctrl+R retries."
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.keepVisible()
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, modelKeys.Up):
			m.move(-1)
		case key.Matches(msg, modelKeys.Down):
			m.move(1)
		case key.Matches(msg, modelKeys.Left):
			return m, m.switchProvider(-1)
		case key.Matches(msg, modelKeys.Right):
			return m, m.switchProvider(1)
		case msg.String() == "ctrl+p":
			m.Close()
			return m, util.CmdHandler(OpenProviderDialogMsg{})
		case msg.String() == "ctrl+f":
			if len(m.filtered) > 0 {
				if err := config.ToggleFavoriteModel(m.filtered[m.selectedIdx].ID); err != nil {
					return m, util.ReportError(err)
				}
				m.filter()
			}
			return m, nil
		case msg.String() == "ctrl+a":
			roles := []config.AgentName{config.AgentCoder, config.AgentTask, config.AgentTitle, config.AgentSummarizer}
			m.target = roles[(slices.Index(roles, m.target)+1)%len(roles)]
			m.filter()
			return m, nil
		case key.Matches(msg, modelKeys.Refresh):
			return m, m.load()
		case key.Matches(msg, modelKeys.Enter):
			return m, m.choose()
		case key.Matches(msg, modelKeys.Escape):
			m.Close()
			return m, util.CmdHandler(CloseModelDialogMsg{})
		case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyCtrlH:
			_, size := utf8.DecodeLastRuneInString(m.query)
			if size > 0 {
				m.query = m.query[:len(m.query)-size]
				m.filter()
			}
		case msg.Type == tea.KeyRunes && len(m.query) < 256:
			for _, r := range msg.Runes {
				if !unicode.IsControl(r) && len(m.query)+utf8.RuneLen(r) <= 256 {
					m.query += string(r)
				}
			}
			m.filter()
		}
	case tea.MouseMsg:
		// The app translates mouse coordinates into this centered dialog.
		if msg.Button == tea.MouseButtonWheelUp {
			m.move(-1)
		}
		if msg.Button == tea.MouseButtonWheelDown {
			m.move(1)
		}
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.X >= 3 && msg.X < 3+m.contentWidth() {
			if msg.Y == 2 {
				if msg.X < 3+m.contentWidth()/2 {
					return m, m.switchProvider(-1)
				}
				return m, m.switchProvider(1)
			}
			index := msg.Y - 5 + m.scrollOffset
			if msg.Y >= 5 && msg.Y < 5+m.visibleRows() && index < len(m.filtered) {
				m.selectedIdx = index
				return m, m.choose()
			}
		}
	}
	return m, nil
}
func (m *modelDialogCmp) choose() tea.Cmd {
	if len(m.filtered) == 0 || m.selectedIdx < 0 || m.selectedIdx >= len(m.filtered) {
		return nil
	}
	selected := m.filtered[m.selectedIdx]
	m.Close()
	return util.CmdHandler(ModelSelectedMsg{Model: selected, Agent: m.target})
}
func (m *modelDialogCmp) switchProvider(offset int) tea.Cmd {
	if len(m.availableProviders) < 2 {
		return nil
	}
	index := slices.Index(m.availableProviders, m.provider)
	index = (index + offset + len(m.availableProviders)) % len(m.availableProviders)
	m.provider = m.availableProviders[index]
	m.query = ""
	return m.load()
}
func (m *modelDialogCmp) filter() {
	m.filtered = nil
	for _, model := range m.models {
		if strings.Contains(strings.ToLower(model.Name+" "+model.APIModel), strings.ToLower(m.query)) {
			m.filtered = append(m.filtered, model)
		}
	}
	prefs := config.Get().Preferences
	slices.SortStableFunc(m.filtered, func(a, b models.Model) int {
		fa, fb := slices.Contains(prefs.FavoriteModels, a.ID), slices.Contains(prefs.FavoriteModels, b.ID)
		if fa != fb {
			if fa {
				return -1
			}
			return 1
		}
		ra, rb := slices.Index(prefs.RecentModels, a.ID), slices.Index(prefs.RecentModels, b.ID)
		if ra < 0 {
			ra = 999
		}
		if rb < 0 {
			rb = 999
		}
		if ra != rb {
			return ra - rb
		}
		return strings.Compare(a.APIModel, b.APIModel)
	})
	m.selectedIdx, m.scrollOffset = 0, 0
	target := m.target
	if target == "" {
		target = config.AgentCoder
	}
	selected := config.Get().Agents[target].Model
	for i, model := range m.filtered {
		if model.ID == selected {
			m.selectedIdx = i
			break
		}
	}
	m.keepVisible()
}
func (m *modelDialogCmp) move(delta int) {
	if len(m.filtered) == 0 {
		return
	}
	m.selectedIdx = (m.selectedIdx + delta + len(m.filtered)) % len(m.filtered)
	m.keepVisible()
}
func (m *modelDialogCmp) keepVisible() {
	if m.selectedIdx < m.scrollOffset {
		m.scrollOffset = m.selectedIdx
	}
	if m.selectedIdx >= m.scrollOffset+m.visibleRows() {
		m.scrollOffset = m.selectedIdx - m.visibleRows() + 1
	}
}
func (m *modelDialogCmp) visibleRows() int {
	if m.height == 0 {
		return numVisibleModels
	}
	return max(1, min(numVisibleModels, m.height-12))
}
func (m *modelDialogCmp) contentWidth() int {
	if m.width == 0 {
		return 68
	}
	return max(12, min(68, m.width-8))
}
func (m *modelDialogCmp) View() string {
	if m.probeConfirm || m.probeRunning {
		return m.probeView()
	}
	t := theme.CurrentTheme()
	width := m.contentWidth()
	base := styles.BaseStyle().Width(width)
	line := func(s string) string { return base.Render(ansi.Truncate(interfaceCopy(s), width, "…")) }
	provider := string(m.provider)
	if provider == "" {
		provider = "No provider"
	}
	if id, ok := strings.CutPrefix(provider, "profile:"); ok {
		p := config.Get().ProviderProfiles[id]
		provider = p.Name + " [" + id + "]"
	}
	role := "main"
	if m.target != "" && m.target != config.AgentCoder {
		role = string(m.target)
	}
	title := config.Text("Escolher modelo ", "Select ") + role + config.Text(" · ", " model · ") + provider
	if m.setup {
		title = config.Text("Configuração · 2/3 · ", "Setup · 2/3 · ") + title
	}
	if m.provider == "" {
		title = "No provider · Ctrl+P settings"
	}
	if len(m.availableProviders) > 1 {
		title = "‹ " + title + " ›"
	}
	lines := []string{base.Foreground(t.Primary()).Bold(true).Render(ansi.Truncate(interfaceCopy(title), width, "…")), line(config.Text("Buscar: ", "Search: ") + m.query)}
	note := m.note
	if m.loading {
		note = "Loading provider models…"
	}
	lines = append(lines, line(note))
	if len(m.filtered) == 0 {
		lines = append(lines, line("No models found"))
	}
	for i := m.scrollOffset; i < min(len(m.filtered), m.scrollOffset+m.visibleRows()); i++ {
		model := m.filtered[i]
		label := model.Name
		prefs := config.Get().Preferences
		if slices.Contains(prefs.FavoriteModels, model.ID) {
			label = "★ " + label
		} else if slices.Contains(prefs.RecentModels, model.ID) {
			label = "↺ " + label
		}
		if model.APIModel != model.Name {
			label += " · " + model.APIModel
		}
		row := base
		if i == m.selectedIdx {
			row = row.Background(t.Primary()).Foreground(t.Background()).Bold(true)
		}
		lines = append(lines, row.Render(ansi.Truncate(label, width, "…")))
	}
	if len(m.filtered) > 0 {
		selected := m.filtered[m.selectedIdx]
		contextNote := ""
		if selected.ContextEstimated {
			contextNote = config.Text(" (estimativa alternativa)", " (fallback estimate)")
		} else {
			switch selected.ContextSource {
			case "ollama:loaded", "lmstudio:loaded":
				contextNote = config.Text(" (alocado no servidor)", " (server allocation)")
			case "ollama:model", "lmstudio:model":
				contextNote = config.Text(" (máximo do modelo)", " (model maximum)")
			case "ollama:parameter":
				contextNote = config.Text(" (parâmetro do servidor)", " (server parameter)")
			case "configuration":
				contextNote = config.Text(" (configurado)", " (configured)")
			case "cache":
				contextNote = config.Text(" (última consulta)", " (cached metadata)")
			case "catalog":
				contextNote = config.Text(" (catálogo interno)", " (built-in catalog)")
			case "provider":
				contextNote = config.Text(" (informado pelo provedor)", " (provider metadata)")
			}
		}
		lines = append(lines, line(fmt.Sprintf(config.Text("Contexto: %d%s · saída: %d", "Context: %d%s · output: %d"), selected.ContextWindow, contextNote, selected.DefaultMaxTokens)))
		caps := []string{}
		if selected.CanReason {
			caps = append(caps, config.Text("raciocínio", "reasoning"))
		}
		if selected.SupportsAttachments {
			caps = append(caps, config.Text("anexos", "attachments"))
		}
		if len(caps) == 0 {
			caps = append(caps, config.Text("nenhuma", "none"))
		}
		protocol := selected.API
		if protocol == "" {
			protocol = config.Text("padrão do provedor", "provider default")
		}
		lines = append(lines, line("API: "+protocol+config.Text(" · capacidades declaradas: ", " · capabilities declared: ")+strings.Join(caps, ", ")))
	}
	lines = append(lines, line("↑↓ select · ←→ provider · Enter choose · Ctrl+F favorite"), line("Ctrl+P settings · Ctrl+A role · Ctrl+R catalog · Ctrl+T probe · Esc"))
	return styles.BaseStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(t.TextMuted()).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
func (m *modelDialogCmp) BindingKeys() []key.Binding { return layout.KeyMapToSlice(modelKeys) }
func GetSelectedModel(cfg *config.Config) models.Model {
	if cfg == nil {
		return models.Model{}
	}
	return models.SupportedModels[cfg.Agents[config.AgentCoder].Model]
}
func getEnabledProviders(cfg *config.Config) []models.ModelProvider {
	var providers []models.ModelProvider
	if cfg == nil {
		return nil
	}
	for id, p := range cfg.ProviderProfiles {
		if !p.Disabled {
			providers = append(providers, models.ModelProvider("profile:"+id))
		}
	}
	for id, p := range cfg.Providers {
		if !p.Disabled {
			providers = append(providers, id)
		}
	}
	slices.SortFunc(providers, func(a, b models.ModelProvider) int {
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
	return providers
}
func getModelsForProvider(provider models.ModelProvider) []models.Model {
	if id, ok := strings.CutPrefix(string(provider), "profile:"); ok {
		return config.ConfiguredProfileModels(id)
	}
	var found []models.Model
	for _, model := range models.SupportedModels {
		if model.Provider == provider && model.ProviderProfile == "" {
			found = append(found, model)
		}
	}
	slices.SortFunc(found, func(a, b models.Model) int { return strings.Compare(a.APIModel, b.APIModel) })
	return found
}
func NewModelDialogCmp() ModelDialog { return &modelDialogCmp{} }

func (m *modelDialogCmp) SetSetup(enabled bool) { m.setup = enabled }
