package dialog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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
	util.Model
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
	catalogFailed                            bool
	request                                  uint64
	cancel                                   context.CancelFunc
	target                                   config.AgentName
	probeConfirm, probeRunning               bool
	setup                                    bool
	catalogProvider                          models.ModelProvider
	refreshSelection                         models.ModelID
	metadataCancel                           context.CancelFunc
	metadataRequest                          uint64
	metadataFor, metadataNote                string
	selecting                                bool
	details                                  bool
	activeContextModel                       models.ModelID
	activeContextWindow                      int64
	activeContextSource                      string
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
	return m.OpenForProvider("")
}

// OpenForProvider starts at a saved connection rather than the previous active
// model. Catalog work remains asynchronous and is cancelled on navigation.
func (m *modelDialogCmp) OpenForProvider(destination models.ModelProvider) tea.Cmd {
	m.Close()
	m.catalogProvider, m.refreshSelection = "", ""
	m.details = false
	m.reasoningOpen = false
	cfg := config.Get()
	m.availableProviders = getEnabledProviders(cfg)
	selected := GetSelectedModel(cfg)
	m.provider = selected.Provider
	if selected.ProviderProfile != "" {
		m.provider = models.ModelProvider("profile:" + selected.ProviderProfile)
	}
	if destination != "" {
		m.provider = destination
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

// Resume refreshes the same catalog after editing its connection and keeps the
// role, query and highlighted model when they still exist.
func (m *modelDialogCmp) Resume() tea.Cmd {
	m.availableProviders = getEnabledProviders(config.Get())
	if !slices.Contains(m.availableProviders, m.provider) {
		return m.Open()
	}
	return m.load()
}

func (m *modelDialogCmp) SetActiveContext(id models.ModelID, window int64, source string) {
	m.activeContextModel, m.activeContextWindow, m.activeContextSource = id, window, source
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
	if m.catalogProvider != m.provider {
		m.refreshSelection = ""
	} else if m.selectedIdx >= 0 && m.selectedIdx < len(m.filtered) {
		m.refreshSelection = m.filtered[m.selectedIdx].ID
	}
	m.catalogProvider = m.provider
	m.models = nil
	m.catalogFailed = false
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
func (m *modelDialogCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	if cmd, handled := m.reasoningUpdate(msg); handled {
		return m, cmd
	}
	if result0, result1, handled := updateSelecting(m, msg); handled {
		return result0, result1
	}
	if result, ok := msg.(modelMetadataLoadedMsg); ok {
		return m, m.acceptMetadata(result)
	}
	if cmd, handled := m.modelProbeUpdate(msg); handled {
		return m, cmd
	}
	switch msg := msg.(type) {
	case ModelCatalogLoadedMsg:
		if result0, result1, handled := updateModelDialogCmpDefault(m, &msg); handled {
			return result0, result1
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.keepVisible()
	case tea.KeyPressMsg:
		if result0, result1, handled := updateModelDialogCmpKeyMsg(m, &msg); handled {
			return result0, result1
		}
	case tea.MouseMsg:
		// The app translates mouse coordinates into this centered dialog.
		if result0, result1, handled := updateModelDialogCmpMouseMsg(m, msg); handled {
			return result0, result1
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
	if key, ok := msg.(tea.KeyPressMsg); ok && key.Text != "" {
		key.Text = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, key.Text)
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
	if selected.EmbeddingOnly() {
		return nil
	}
	lookup := config.ModelMetadata(selected)
	agent := m.target
	m.Close()
	if lookup != nil && (selected.ContextEstimated || selected.ContextSource == "cache") {
		m.selecting = true
		m.note = config.Text("Selecionando modelo… Esc cancela", "Selecting model… Esc cancels")
		ctx, cancel := context.WithCancel(context.Background())
		m.metadataCancel = cancel
		request := m.metadataRequest
		return func() tea.Msg {
			defer cancel()
			if found, err := lookup(ctx); err == nil {
				selected = found
			}
			if ctx.Err() != nil {
				return nil
			}
			if selected.EmbeddingOnly() {
				return modelMetadataLoadedMsg{request: request, model: selected}
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
	filterSelectedIdxSelectedIdxFiltered(m, &previous)
	m.filtered = nil
	filterModels(m)
	prefs := config.Get().Preferences
	slices.SortStableFunc(m.filtered, func(a, b models.Model) int {
		if result0, handled := filterQueryCase(m, &a, &b); handled {
			return result0
		}
		if result0, handled := filterQuery(m, &a, &b); handled {
			return result0
		}
		fa, fb := slices.Contains(prefs.FavoriteModels, a.ID), slices.Contains(prefs.FavoriteModels, b.ID)
		if result0, handled := filterFaFb(fa, fb); handled {
			return result0
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
	filterModelModelsModel(m, &previous, &selected)
	filterFiltered(m, selected)
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
	search := newDialogInput()
	search.Prompt = ""
	search.Placeholder = config.Text("Buscar modelos", "Search models")
	search.CharLimit = 256
	inputStyle := search.Styles()
	inputStyle.Cursor.Blink = false
	search.SetStyles(inputStyle)
	return &modelDialogCmp{search: search}
}

func (m *modelDialogCmp) SetSetup(enabled bool) { m.setup = enabled }

func updateModelDialogCmpMouseMsg(m *modelDialogCmp, msg tea.MouseMsg) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	if clickedLabel(m.View(), config.Text("Ctrl+D detalhes", "Ctrl+D details"), msg) {
		m.details = !m.details
		m.keepVisible()
		return m, m.scheduleMetadata(), true
	}
	if msg.Mouse().Button == tea.MouseWheelUp {
		m.move(-1)
	}
	if msg.Mouse().Button == tea.MouseWheelDown {
		m.move(1)
	}
	geometry := m.geometry()
	if leftClick(msg) && msg.Mouse().X >= geometry.contentX && msg.Mouse().X < geometry.contentX+geometry.width {
		if msg.Mouse().Y == geometry.titleY && len(m.availableProviders) > 1 {
			if msg.Mouse().X < geometry.contentX+geometry.width/2 {
				return m, m.switchProvider(-1), true
			}
			return m, m.switchProvider(1), true
		}
		index := msg.Mouse().Y - geometry.listY + m.scrollOffset
		if msg.Mouse().Y >= geometry.listY && msg.Mouse().Y < geometry.listY+geometry.rows && index >= 0 && index < len(m.filtered) {
			m.selectedIdx = index
			return m, m.choose(), true
		}
	}
	return extractedResult0, extractedResult1, false
}

func updateModelDialogCmpDefault(m *modelDialogCmp, msg *ModelCatalogLoadedMsg) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	if msg.request != m.request {
		return m, nil, true
	}
	m.loading = false
	m.cancel = nil
	if msg.err == nil {
		m.models = msg.models
		m.note = fmt.Sprintf(config.Text("Catálogo do provedor · %d modelos", "Provider catalog · %d models"), len(m.models))
		m.filter()
		for i, model := range m.filtered {
			if model.ID == m.refreshSelection {
				m.selectedIdx = i
				m.keepVisible()
				break
			}
		}
	} else if errors.Is(msg.err, models.ErrCatalogUnsupported) {
		m.models = getModelsForProvider(m.provider)
		m.filter()
		m.note = config.Text("Catálogo configurado · consulta indisponível", "Configured catalog · live listing unavailable")
	} else {
		m.catalogFailed = true
		m.models = nil
		m.filter()
		m.note = config.Text("Falha ao consultar provedor · Ctrl+R tenta novamente · Ctrl+P conexões", "Provider query failed · Ctrl+R retries · Ctrl+P connections")
	}
	return extractedResult0, extractedResult1, false
}

func updateModelDialogCmpKeyMsg(m *modelDialogCmp, msg *tea.KeyPressMsg) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	switch {
	case msg.String() == "ctrl+d":
		m.details = !m.details
		m.keepVisible()
		return m, m.scheduleMetadata(), true
	case key.Matches((*msg), modelKeys.Up):
		m.move(-1)
	case key.Matches((*msg), modelKeys.Down):
		m.move(1)
	case msg.Code == tea.KeyPgUp:
		m.selectedIdx = max(0, m.selectedIdx-m.visibleRows())
		m.keepVisible()
	case msg.Code == tea.KeyPgDown:
		m.selectedIdx = max(0, min(len(m.filtered)-1, m.selectedIdx+m.visibleRows()))
		m.keepVisible()
	case key.Matches((*msg), modelKeys.Left):
		return m, m.switchProvider(-1), true
	case key.Matches((*msg), modelKeys.Right):
		return m, m.switchProvider(1), true
	case msg.String() == "ctrl+p":
		m.Close()
		return m, util.CmdHandler(OpenProviderDialogMsg{Provider: m.provider, ReturnToModels: true}), true
	case msg.String() == "ctrl+f":
		if len(m.filtered) > 0 {
			if err := config.ToggleFavoriteModel(m.filtered[m.selectedIdx].ID); err != nil {
				return m, util.ReportError(err), true
			}
			m.filter()
		}
		return m, nil, true
	case msg.String() == "ctrl+a":
		roles := []config.AgentName{config.AgentCoder, config.AgentTask, config.AgentTitle, config.AgentSummarizer}
		m.target = roles[(slices.Index(roles, m.target)+1)%len(roles)]
		m.selectedIdx = -1
		m.filter()
		return m, nil, true
	case key.Matches((*msg), modelKeys.Refresh):
		return m, m.load(), true
	case key.Matches((*msg), modelKeys.Enter):
		return m, m.choose(), true
	case key.Matches((*msg), modelKeys.Escape):
		m.Close()
		return m, util.CmdHandler(CloseModelDialogMsg{}), true
	default:
		if cmd := m.updateSearch((*msg)); cmd != nil {
			return m, cmd, true
		}

	}
	return extractedResult0, extractedResult1, false
}

func filterQuery(m *modelDialogCmp, a *models.Model, b *models.Model) (extractedResult0 int, handled bool) {
	if m.query == "" {
		aa, bb := a.ID == m.activeModelID(), b.ID == m.activeModelID()
		if aa != bb {
			if aa {
				return -1, true
			}
			return 1, true
		}
	}
	return extractedResult0, false
}

func filterSelectedIdxSelectedIdxFiltered(m *modelDialogCmp, previous *models.ModelID) {
	if m.selectedIdx >= 0 && m.selectedIdx < len(m.filtered) {
		(*previous) = m.filtered[m.selectedIdx].ID
	}
}

func filterModels(m *modelDialogCmp) {
	for _, model := range m.models {
		if model.EmbeddingOnly() {
			continue
		}
		if _, ok := modelMatchScore(m.query, model.Name+" "+model.APIModel); ok {
			m.filtered = append(m.filtered, model)
		}
	}
}

func filterQueryCase(m *modelDialogCmp, a *models.Model, b *models.Model) (extractedResult0 int, handled bool) {
	if m.query != "" {
		sa, _ := modelMatchScore(m.query, a.Name+" "+a.APIModel)
		sb, _ := modelMatchScore(m.query, b.Name+" "+b.APIModel)
		if sa != sb {
			return sa - sb, true
		}
	}
	return extractedResult0, false
}

func filterFaFb(fa bool, fb bool) (extractedResult0 int, handled bool) {
	if fa != fb {
		if fa {
			return -1, true
		}
		return 1, true
	}
	return extractedResult0, false
}

func updateSelecting(m *modelDialogCmp, msg tea.Msg) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	if m.selecting {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			if key.Code == tea.KeyEsc {
				m.Close()
				return m, util.CmdHandler(CloseModelDialogMsg{}), true
			}
			return m, nil, true
		}
		if _, ok := msg.(tea.MouseMsg); ok {
			return m, nil, true
		}
	}
	return extractedResult0, extractedResult1, false
}

func filterModelModelsModel(m *modelDialogCmp, previous *models.ModelID, selected *models.ModelID) {
	if (*previous) != "" && slices.ContainsFunc(m.filtered, func(model models.Model) bool { return model.ID == (*previous) }) {
		(*selected) = (*previous)
	}
}

func filterFiltered(m *modelDialogCmp, selected models.ModelID) {
	for i, model := range m.filtered {
		if model.ID == selected {
			m.selectedIdx = i
			break
		}
	}
}
