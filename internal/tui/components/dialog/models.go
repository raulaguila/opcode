package dialog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/layout"
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
	reasoningOpen                            bool
	reasoningIndex                           int
	models, filtered                         []models.Model
	provider                                 models.ModelProvider
	availableProviders                       []models.ModelProvider
	selectedIdx, width, height, scrollOffset int
	query, note                              string
	search                                   textinput.Model
	loading                                  bool
	request                                  uint64
	cancel                                   context.CancelFunc
	target                                   config.AgentName
	probeConfirm, probeRunning               bool
	setup                                    bool
	catalogProvider                          models.ModelProvider
	metadataCancel                           context.CancelFunc
	metadataRequest                          uint64
	metadataFor, metadataNote                string
	selecting                                bool
	details                                  bool
}

var modelKeys = struct {
	Up, Down, Left, Right, Enter, Escape, Refresh key.Binding
}{
	key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "previous model")),
	key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "next model")),
	key.NewBinding(key.WithKeys("alt+left", "shift+tab"), key.WithHelp("Alt+←/Shift+Tab", "previous provider")),
	key.NewBinding(key.WithKeys("alt+right", "tab"), key.WithHelp("Alt+→/Tab", "next provider")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select model")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh models")),
}

func (m *modelDialogCmp) Init() tea.Cmd { return nil }
func (m *modelDialogCmp) Open() tea.Cmd {
	m.Close()
	m.catalogProvider = ""
	m.details = false
	m.reasoningOpen = false
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
	m.search.SetValue("")
	return m.load()
}
func (m *modelDialogCmp) Close() {
	m.search.Blur()
	m.selecting = false
	if m.metadataCancel != nil {
		m.metadataCancel()
		m.metadataCancel = nil
	}
	m.metadataRequest++
	m.metadataFor, m.metadataNote = "", ""
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
	m.search.Focus()
	if m.provider == "" || m.catalogProvider != m.provider {
		m.models = getModelsForProvider(m.provider)
		m.catalogProvider = m.provider
	}
	m.note = config.Text("Catálogo disponível", "Available catalog")
	m.filter()
	if m.provider == "" {
		m.note = config.Text("Configure uma conexão em /settings", "Configure a connection in /settings")
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
		found, err := fetch(models.WithoutCatalogMetadata(ctx))
		cancel()
		return ModelCatalogLoadedMsg{request, found, err}
	}
}
func (m *modelDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, handled := m.reasoningUpdate(msg); handled {
		return m, cmd
	}
	if m.selecting {
		if key, ok := msg.(tea.KeyMsg); ok {
			if key.Type == tea.KeyEsc {
				m.Close()
				return m, util.CmdHandler(CloseModelDialogMsg{})
			}
			return m, nil
		}
		if _, ok := msg.(tea.MouseMsg); ok {
			return m, nil
		}
	}
	if result, ok := msg.(modelMetadataLoadedMsg); ok {
		return m, m.acceptMetadata(result)
	}
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
			m.note = config.Text("Catálogo configurado · consulta indisponível", "Configured catalog · live listing unavailable")
		} else {
			m.note = config.Text("Falha ao atualizar; catálogo anterior preservado. Ctrl+R tenta novamente.", "Refresh failed; previous catalog preserved. Ctrl+R retries.")
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.keepVisible()
	case tea.KeyMsg:
		switch {
		case msg.String() == "ctrl+d":
			m.details = !m.details
			m.keepVisible()
			return m, m.scheduleMetadata()
		case key.Matches(msg, modelKeys.Up):
			m.move(-1)
		case key.Matches(msg, modelKeys.Down):
			m.move(1)
		case msg.Type == tea.KeyPgUp:
			m.selectedIdx = max(0, m.selectedIdx-m.visibleRows())
			m.keepVisible()
		case msg.Type == tea.KeyPgDown:
			m.selectedIdx = max(0, min(len(m.filtered)-1, m.selectedIdx+m.visibleRows()))
			m.keepVisible()
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
			m.selectedIdx = -1
			m.filter()
			return m, nil
		case key.Matches(msg, modelKeys.Refresh):
			return m, m.load()
		case key.Matches(msg, modelKeys.Enter):
			return m, m.choose()
		case key.Matches(msg, modelKeys.Escape):
			m.Close()
			return m, util.CmdHandler(CloseModelDialogMsg{})
		default:
			if cmd := m.updateSearch(msg); cmd != nil {
				return m, cmd
			}

		}
	case tea.MouseMsg:
		// The app translates mouse coordinates into this centered dialog.
		if msg.Button == tea.MouseButtonWheelUp {
			m.move(-1)
		}
		if msg.Button == tea.MouseButtonWheelDown {
			m.move(1)
		}
		geometry := m.geometry()
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.X >= geometry.contentX && msg.X < geometry.contentX+geometry.width {
			if msg.Y == geometry.titleY && len(m.availableProviders) > 1 {
				if msg.X < geometry.contentX+geometry.width/2 {
					return m, m.switchProvider(-1)
				}
				return m, m.switchProvider(1)
			}
			index := msg.Y - geometry.listY + m.scrollOffset
			if msg.Y >= geometry.listY && msg.Y < geometry.listY+geometry.rows && index >= 0 && index < len(m.filtered) {
				m.selectedIdx = index
				return m, m.choose()
			}
		}
	default:
		// textinput also receives its asynchronous clipboard result messages.
		if cmd := m.updateSearch(msg); cmd != nil {
			return m, cmd
		}
	}
	return m, m.scheduleMetadata()
}

func (m *modelDialogCmp) updateSearch(msg tea.Msg) tea.Cmd {
	before := m.search.Value()
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyRunes {
		key.Runes = []rune(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, string(key.Runes)))
		msg = key
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	value := m.search.Value()
	bounded := strings.Builder{}
	for _, r := range value {
		if unicode.IsControl(r) {
			continue
		}
		if bounded.Len()+len(string(r)) > 256 {
			break
		}
		bounded.WriteRune(r)
	}
	m.query = bounded.String()
	if value != m.query {
		m.search.SetValue(m.query)
	}
	if m.query != before {
		m.filter()
	}
	return cmd
}
func (m *modelDialogCmp) choose() tea.Cmd {
	if len(m.filtered) == 0 || m.selectedIdx < 0 || m.selectedIdx >= len(m.filtered) {
		return nil
	}
	selected := m.filtered[m.selectedIdx]
	lookup := config.ModelMetadata(selected)
	agent := m.target
	m.Close()
	if lookup != nil && (selected.ContextEstimated || selected.ContextSource == "cache") {
		m.selecting = true
		m.note = config.Text("Selecionando modelo… Esc cancela", "Selecting model… Esc cancels")
		ctx, cancel := context.WithCancel(context.Background())
		m.metadataCancel = cancel
		return func() tea.Msg {
			defer cancel()
			if found, err := lookup(ctx); err == nil {
				selected = found
			}
			if ctx.Err() != nil {
				return nil
			}
			return ModelSelectedMsg{Model: selected, Agent: agent}
		}
	}
	return util.CmdHandler(ModelSelectedMsg{Model: selected, Agent: agent})
}
func (m *modelDialogCmp) switchProvider(offset int) tea.Cmd {
	if len(m.availableProviders) < 2 {
		m.note = config.Text("Este é o único provedor habilitado · Ctrl+P conexões", "This is the only enabled provider · Ctrl+P connections")
		if len(m.availableProviders) == 0 {
			m.note = config.Text("Nenhum provedor habilitado · Ctrl+P conexões", "No enabled providers · Ctrl+P connections")
		}
		return nil
	}
	index := slices.Index(m.availableProviders, m.provider)
	index = (index + offset + len(m.availableProviders)) % len(m.availableProviders)
	m.provider = m.availableProviders[index]
	m.query = ""
	m.search.SetValue("")
	return m.load()
}
func (m *modelDialogCmp) filter() {
	if m.search.Value() != m.query {
		m.search.SetValue(m.query)
		m.search.CursorEnd()
	}
	previous := models.ModelID("")
	if m.selectedIdx >= 0 && m.selectedIdx < len(m.filtered) {
		previous = m.filtered[m.selectedIdx].ID
	}
	m.filtered = nil
	for _, model := range m.models {
		if _, ok := modelMatchScore(m.query, model.Name+" "+model.APIModel); ok {
			m.filtered = append(m.filtered, model)
		}
	}
	prefs := config.Get().Preferences
	slices.SortStableFunc(m.filtered, func(a, b models.Model) int {
		if m.query != "" {
			sa, _ := modelMatchScore(m.query, a.Name+" "+a.APIModel)
			sb, _ := modelMatchScore(m.query, b.Name+" "+b.APIModel)
			if sa != sb {
				return sa - sb
			}
		}
		if m.query == "" {
			aa, bb := a.ID == m.activeModelID(), b.ID == m.activeModelID()
			if aa != bb {
				if aa {
					return -1
				}
				return 1
			}
		}
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
	if previous != "" && slices.ContainsFunc(m.filtered, func(model models.Model) bool { return model.ID == previous }) {
		selected = previous
	}
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
func (m *modelDialogCmp) visibleRows() int  { return m.geometry().rows }
func (m *modelDialogCmp) contentWidth() int { return m.geometry().width }
func (m *modelDialogCmp) View() string {
	if m.reasoningOpen {
		return m.reasoningView()
	}
	if m.probeConfirm || m.probeRunning {
		return m.probeView()
	}
	return m.catalogView()
}
func (m *modelDialogCmp) BindingKeys() []key.Binding {
	return append(layout.KeyMapToSlice(modelKeys), key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", config.Text("detalhes", "details"))))
}
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
func NewModelDialogCmp() ModelDialog {
	search := textinput.New()
	search.Prompt = ""
	search.Placeholder = config.Text("Buscar modelos", "Search models")
	search.CharLimit = 256
	search.Cursor.SetMode(cursor.CursorStatic)
	return &modelDialogCmp{search: search}
}

func (m *modelDialogCmp) SetSetup(enabled bool) { m.setup = enabled }
