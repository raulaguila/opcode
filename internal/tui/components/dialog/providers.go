package dialog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
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

const (
	providerAuthField = 10
	providerKeyField  = 11
)

type OpenProviderDialogMsg struct{ Setup bool }
type CloseProviderDialogMsg struct{}
type ProviderSettingsSavedMsg struct {
	ProviderChanged bool
	Disabled        bool
}
type providerTestMsg struct {
	request uint64
	count   int
	err     error
}
type ProviderDialog interface {
	tea.Model
	layout.Bindings
	Open() tea.Cmd
	Close()
}
type providerRow struct {
	id     string
	legacy bool
}
type providerDialogCmp struct {
	baseline                       []string
	resumeDraft                    func()
	allowSave                      func() error
	rows                           []providerRow
	selected, width, height, focus int
	screen                         string
	fields                         []textinput.Model
	labels                         []string
	originalID                     string
	legacy                         bool
	originalKey                    string
	note                           string
	noteFull                       bool
	noteOffset                     int
	testing                        bool
	request                        uint64
	cancel                         context.CancelFunc
	settingsFilter                 textinput.Model
	advanced                       bool
	settingPath                    string
}

func NewProviderDialogCmp(allowSave ...func() error) ProviderDialog {
	m := &providerDialogCmp{}
	if len(allowSave) > 0 {
		m.allowSave = allowSave[0]
	}
	return m
}
func (m *providerDialogCmp) Init() tea.Cmd { return nil }
func (m *providerDialogCmp) Open() tea.Cmd {
	m.Close()
	m.screen = "list"
	m.note = "Configure a connection once; choose its model with Ctrl+O."
	m.selected = 0
	m.rows = nil
	c := config.Get()
	if c == nil {
		return nil
	}
	for id := range c.ProviderProfiles {
		m.rows = append(m.rows, providerRow{id: id})
	}
	for id := range c.Providers {
		m.rows = append(m.rows, providerRow{id: string(id), legacy: true})
	}
	slices.SortFunc(m.rows, func(a, b providerRow) int {
		if a.legacy != b.legacy {
			if a.legacy {
				return 1
			}
			return -1
		}
		return strings.Compare(a.id, b.id)
	})
	return nil
}
func (m *providerDialogCmp) Close() {
	m.noteFull = false
	m.noteOffset = 0
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.request++
	m.testing = false
}
func (m *providerDialogCmp) setFields(labels, values []string) {
	m.baseline = append([]string(nil), values...)
	m.labels = labels
	m.fields = make([]textinput.Model, len(labels))
	m.focus = 0
	for i := range labels {
		f := textinput.New()
		f.CharLimit = 512
		f.Prompt = ""
		f.SetValue(values[i])
		f.Width = max(8, m.contentWidth()-3)
		if i == 0 {
			f.Focus()
		}
		m.fields[i] = f
	}
}
func (m *providerDialogCmp) edit(row *providerRow) tea.Cmd {
	m.Close()
	m.screen = "provider"
	m.note = ""
	m.legacy = false
	m.originalID = ""
	m.originalKey = ""
	id := "local"
	profile := config.ProviderProfile{Name: "Local Ollama", Backend: models.ProviderLocal, Provider: config.Provider{BaseURL: "http://127.0.0.1:11434/v1", API: "ollama", ContextWindow: 0, DefaultMaxTokens: 2048}}
	if row != nil {
		id = row.id
		m.originalID = id
		m.legacy = row.legacy
		if row.legacy {
			profile = config.ProviderProfile{Name: id, Backend: models.ModelProvider(id), Provider: config.Get().Providers[models.ModelProvider(id)]}
		} else {
			profile = config.Get().ProviderProfiles[id]
		}
		m.originalKey = config.StoredProviderKey(id, m.legacy)
		if profile.APIKeyEnv == "" && m.originalKey == "" {
			profile.APIKeyEnv = config.ProviderEnvironmentName(profile.Backend)
		}
	}
	auth := "none"
	if profile.APIKeyEnv != "" {
		auth = "environment"
	} else if m.originalKey != "" || (profile.Backend != "" && profile.Backend != models.ProviderLocal) {
		auth = "api_key"
	}
	m.setFields([]string{"Profile ID", "Name", "Backend (local/openai/anthropic/gemini/groq/openrouter/xai)", "API base URL", "API key environment variable", "API (empty/chat_completions/responses/ollama)", "Context window (0 uses catalog)", "Default output tokens (0 uses catalog)", "Connection active (true/false)", "TLS insecure (true/false)", "Authentication", "API key"}, []string{id, profile.Name, string(profile.Backend), profile.BaseURL, profile.APIKeyEnv, profile.API, strconv.FormatInt(profile.ContextWindow, 10), strconv.FormatInt(profile.DefaultMaxTokens, 10), strconv.FormatBool(!profile.Disabled), strconv.FormatBool(profile.TLSInsecure), auth, ""})
	m.fields[providerKeyField].EchoMode = textinput.EchoPassword
	m.fields[providerKeyField].EchoCharacter = '•'
	m.fields[providerKeyField].CharLimit = 65536
	m.fields[providerKeyField].SetValue(m.originalKey)
	m.baseline[providerKeyField] = m.originalKey
	m.advanced = false
	m.fields[0].Blur()
	m.focus = 1
	m.fields[m.focus].Focus()
	return textinput.Blink
}
func (m *providerDialogCmp) preferences() tea.Cmd {
	m.Close()
	m.screen = "preferences"
	m.note = "Applies to new turns and sessions; current approvals remain unchanged."
	c := config.Get()
	r := c.Runtime.Defaults()
	m.setFields([]string{"Initial mode (ask/plan/execute)", "Maximum steps", "Maximum seconds", "Tool output budget (bytes)", "First token timeout (seconds)", "Idle timeout (seconds)", "Resume most recent session (true/false)", "Show process by default (true/false)"}, []string{r.Mode, strconv.Itoa(r.MaxSteps), strconv.Itoa(r.MaxSeconds), strconv.Itoa(r.ToolBudget), strconv.Itoa(r.FirstTokenTimeout), strconv.Itoa(r.IdleTimeout), strconv.FormatBool(c.Preferences.ResumeLast), strconv.FormatBool(c.Preferences.ProcessVisible())})
	return textinput.Blink
}
func (m *providerDialogCmp) readProfile() (string, config.ProviderProfile, error) {
	value := func(i int) string { return strings.TrimSpace(m.fields[i].Value()) }
	id := value(0)
	p := config.ProviderProfile{Name: value(1), Backend: models.ModelProvider(value(2)), Provider: config.Provider{BaseURL: value(3), API: value(5)}}
	if m.originalID != "" && id != m.originalID {
		return "", p, fmt.Errorf("profile ID cannot be changed; add a new connection instead")
	}
	if m.legacy && p.Backend != models.ModelProvider(id) {
		return "", p, fmt.Errorf("legacy backend cannot be changed; add a named profile instead")
	}
	var err error
	p.ContextWindow, err = strconv.ParseInt(value(6), 10, 64)
	if err != nil || p.ContextWindow < 0 {
		return "", p, fmt.Errorf("context window must be a nonnegative integer")
	}
	p.DefaultMaxTokens, err = strconv.ParseInt(value(7), 10, 64)
	if err != nil || p.DefaultMaxTokens < 0 {
		return "", p, fmt.Errorf("output tokens must be a nonnegative integer")
	}
	active, err := strconv.ParseBool(value(8))
	p.Disabled = !active
	if err != nil {
		return "", p, fmt.Errorf("connection active must be true or false")
	}
	p.TLSInsecure, err = strconv.ParseBool(value(9))
	if err != nil {
		return "", p, fmt.Errorf("TLS insecure must be true or false")
	}
	switch value(providerAuthField) {
	case "api_key":
		p.APIKey = value(providerKeyField)
		if p.APIKey == "" && !p.Disabled {
			return "", p, errors.New(config.Text("Informe a chave da API ou escolha outra autenticação", "Enter the API key or choose another authentication method"))
		}
	case "environment":
		p.APIKeyEnv = value(4)
		if p.APIKeyEnv == "" && !p.Disabled {
			return "", p, errors.New(config.Text("Informe o nome da variável de ambiente", "Enter the environment variable name"))
		}
	case "none":
	default:
		return "", p, fmt.Errorf("invalid authentication method")
	}
	return id, p, nil
}
func (m *providerDialogCmp) test() tea.Cmd {
	id, p, err := m.readProfile()
	if err != nil {
		m.note = err.Error()
		return nil
	}
	m.Close()
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.testing = true
	request := m.request
	fetch := config.TestProviderProfile(id, p)
	return func() tea.Msg {
		found, err := fetch(ctx)
		cancel()
		return providerTestMsg{request: request, count: len(found), err: err}
	}
}
func (m *providerDialogCmp) save() tea.Cmd {
	if m.allowSave != nil {
		if err := m.allowSave(); err != nil {
			m.note = err.Error()
			return nil
		}
	}
	var err error
	disabled := false
	if m.screen == "provider" {
		var id string
		var p config.ProviderProfile
		id, p, err = m.readProfile()
		disabled = p.Disabled
		if err == nil {
			if m.legacy {
				err = config.SaveProvider(p.Backend, p.Provider)
			} else {
				err = config.SaveProviderProfile(id, p)
			}
		}
	} else if m.screen == "preferences" {
		c := config.Get()
		r := c.Runtime
		p := c.Preferences
		r.Mode = strings.TrimSpace(m.fields[0].Value())
		targets := []*int{&r.MaxSteps, &r.MaxSeconds, &r.ToolBudget, &r.FirstTokenTimeout, &r.IdleTimeout}
		for i, target := range targets {
			*target, err = strconv.Atoi(strings.TrimSpace(m.fields[i+1].Value()))
			if err != nil || *target < 1 {
				err = fmt.Errorf("limits must be positive integers")
				break
			}
		}
		if err == nil {
			p.ResumeLast, err = strconv.ParseBool(strings.TrimSpace(m.fields[6].Value()))
		}
		if err == nil {
			var visible bool
			visible, err = strconv.ParseBool(strings.TrimSpace(m.fields[7].Value()))
			p.ShowProcess = &visible
		}
		if err == nil {
			err = config.SavePreferences(p, r)
		}
	}
	if err != nil {
		m.note = err.Error()
		return nil
	}
	changed := m.screen == "provider"
	m.Close()
	return util.CmdHandler(ProviderSettingsSavedMsg{ProviderChanged: changed, Disabled: disabled})
}
func (m *providerDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.noteUpdate(msg) {
		return m, nil
	}
	if cmd, handled := m.experienceUpdate(msg); handled {
		return m, cmd
	}
	switch msg := msg.(type) {
	case providerTestMsg:
		if msg.request != m.request {
			return m, nil
		}
		m.testing = false
		m.cancel = nil
		if msg.err != nil {
			m.note = config.Text("Falha na consulta: ", "Connection test failed: ") + msg.err.Error()
		} else {
			m.note = fmt.Sprintf(config.Text("Catálogo consultado · %d modelos. Ferramentas não testadas. Ctrl+S salva.", "Catalog queried · %d models. Tools not tested. Ctrl+S saves."), msg.count)
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.MouseMsg:
		if m.screen == "list" {
			if clickedLabel(m.View(), config.Text("[Preferências]", "[Preferences]"), msg) {
				return m, m.settings()
			}
			if clickedLabel(m.View(), config.Text("[Integrações]", "[Integrations]"), msg) {
				return m, util.CmdHandler(OpenIntegrationDialogMsg{})
			}
		}
		if m.screen == "provider" {
			for _, a := range []struct{ label, key string }{{config.Text("[Salvar]", "[Save]"), "ctrl+s"}, {config.Text("[Catálogo]", "[Catalog]"), "ctrl+t"}, {config.Text("[Mais]", "[More]"), "ctrl+a"}} {
				if clickedLabel(m.View(), a.label, msg) {
					return m.Update(tea.KeyMsg{Type: map[string]tea.KeyType{"ctrl+s": tea.KeyCtrlS, "ctrl+t": tea.KeyCtrlT, "ctrl+a": tea.KeyCtrlA}[a.key]})
				}
			}
		}

		if m.screen == "list" && len(m.rows) > 0 {
			if msg.Button == tea.MouseButtonWheelUp {
				m.selected = max(0, m.selected-1)
			}
			if msg.Button == tea.MouseButtonWheelDown {
				m.selected = min(len(m.rows)-1, m.selected+1)
			}
		}
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.X >= 3 && msg.X < m.contentWidth()+3 {
			if m.screen == "list" {
				rows := m.listRows()
				start := max(0, m.selected-rows+1)
				i := msg.Y - 3 + start
				if msg.Y >= 3 && msg.Y < 3+rows && i < len(m.rows) {
					m.selected = i
					return m, m.edit(&m.rows[i])
				}
			} else {
				count := m.formRows()
				indices := m.fieldIndices()
				start := max(0, slices.Index(indices, m.focus)-count+1)
				i := start + (msg.Y-3)/2
				if msg.Y >= 3 && msg.Y < 3+count*2 && i < len(indices) {
					m.fields[m.focus].Blur()
					m.focus = indices[i]
					return m, m.fields[m.focus].Focus()
				}
			}
		}
	case tea.KeyMsg:
		if msg.String() == "esc" {
			if m.testing {
				m.Close()
				m.note = "Connection test cancelled; your edits are preserved."
				return m, nil
			}
			if m.screen != "list" {
				saved := *m
				saved.cancel = nil
				saved.testing = false
				saved.resumeDraft = nil
				cmd := m.Open()
				m.resumeDraft = func() {
					w, h, request := m.width, m.height, m.request+1
					*m = saved
					m.width, m.height, m.request = w, h, request
				}
				m.note = config.Text("R retoma a edição preservada", "R resumes preserved edits")
				return m, cmd
			}
			m.Close()
			return m, util.CmdHandler(CloseProviderDialogMsg{})
		}
		if m.screen != "list" && len(m.fields) > m.focus && choiceKey(&m.fields[m.focus], fieldOptions(m.labels[m.focus]), msg) {
			if m.testing {
				m.Close()
				m.note = "Edited; test again to validate these settings."
			}
			return m, nil
		}
		if m.screen == "list" {
			switch msg.String() {
			case "up":
				if len(m.rows) > 0 {
					m.selected = (m.selected - 1 + len(m.rows)) % len(m.rows)
				}
			case "down":
				if len(m.rows) > 0 {
					m.selected = (m.selected + 1) % len(m.rows)
				}
			case "enter":
				if len(m.rows) > 0 {
					return m, m.edit(&m.rows[m.selected])
				}
				return m, m.edit(nil)
			case "r":
				if m.resumeDraft != nil {
					m.resumeDraft()
					return m, textinput.Blink
				}
			case "n":
				m.screen = "presets"
				m.selected = 0
				m.note = config.Text("↑↓ escolhe · Enter configura · Esc volta", "↑↓ choose · Enter configure · Esc back")
				return m, nil
			case "p":
				return m, m.settings()
			case "i":
				m.Close()
				return m, util.CmdHandler(OpenIntegrationDialogMsg{})
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+g":
			if m.screen == "provider" {
				m.fields[8].SetValue("true")
				return m, m.save()
			}
			return m, nil
		case "ctrl+s":
			return m, m.save()
		case "ctrl+a":
			if m.screen == "provider" {
				m.advanced = !m.advanced
				if !slices.Contains(m.fieldIndices(), m.focus) {
					m.fields[m.focus].Blur()
					m.focus = m.fieldIndices()[0]
					m.fields[m.focus].Focus()
				}
			}
			return m, nil
		case "ctrl+t":
			if m.screen == "provider" {
				return m, m.test()
			}
			return m, nil
		case "tab", "down", "enter", "shift+tab", "up":
			m.fields[m.focus].Blur()
			delta := 1
			if msg.String() == "shift+tab" || msg.String() == "up" {
				delta = -1
			}
			indices := m.fieldIndices()
			index := slices.Index(indices, m.focus)
			m.focus = indices[(index+delta+len(indices))%len(indices)]
			return m, m.fields[m.focus].Focus()
		}
	}
	if m.screen != "list" && len(m.fields) > 0 {
		var cmd tea.Cmd
		m.fields[m.focus], cmd = m.fields[m.focus].Update(msg)
		if _, ok := msg.(tea.KeyMsg); ok && m.testing {
			m.Close()
			m.note = "Edited; test again to validate these settings."
		}
		return m, cmd
	}
	return m, nil
}
func (m *providerDialogCmp) contentWidth() int {
	if m.width == 0 {
		return 72
	}
	return max(12, min(72, m.width-8))
}
func (m *providerDialogCmp) View() string {
	if m.noteFull {
		return m.noteView()
	}
	if m.screen == "settings" || m.screen == "setting" || m.screen == "effective" || m.screen == "presets" {
		return m.experienceView()
	}
	t := theme.CurrentTheme()
	width := m.contentWidth()
	base := styles.BaseStyle().Width(width)
	line := func(s string) string { return base.Render(ansi.Truncate(interfaceCopy(s), width, "…")) }
	title := "Connections and preferences"
	if m.screen == "provider" {
		title = "Configure provider"
	}
	if m.screen == "preferences" {
		title = "Preferences"
	}
	lines := []string{base.Foreground(t.Primary()).Bold(true).Render(interfaceCopy(title))}
	if m.screen == "list" {
		if len(m.rows) == 0 {
			lines = append(lines, line("No connections yet. Press N to add Ollama or another provider."))
		}
		rows := m.listRows()
		start := max(0, m.selected-rows+1)
		for i := start; i < min(len(m.rows), start+rows); i++ {
			row := m.rows[i]
			label := row.id
			if row.legacy {
				label = config.Text("Servidor local", "Local server") + " · " + row.id
			} else {
				p := config.Get().ProviderProfiles[row.id]
				label = p.Name + " · " + row.id + " (" + string(p.Backend) + ")"
			}
			disabled := false
			if row.legacy {
				disabled = config.Get().Providers[models.ModelProvider(row.id)].Disabled
			} else {
				disabled = config.Get().ProviderProfiles[row.id].Disabled
			}
			if disabled {
				label += config.Text(" · desativada", " · disabled")
			}
			style := base
			if i == m.selected {
				style = style.Foreground(t.Background()).Background(t.Primary()).Bold(true)
			}
			lines = append(lines, style.Render(ansi.Truncate(selectionMark(i == m.selected)+label, width, "…")))
		}
		lines = append(lines, line(config.Text("N Adicionar · Enter Editar · Esc", "N Add · Enter Edit · Esc")))
		lines = append(lines, line(config.Text("[Preferências] P · [Integrações] I", "[Preferences] P · [Integrations] I")))
	} else {
		// Keep the focused field visible on small terminals; labels remain attached.
		if m.screen == "provider" {
			m.fields[3].Placeholder = config.Text("Padrão do provedor", "Provider default")
			m.fields[4].Placeholder = config.Text("Nome da variável com a chave", "Variable holding the API key")
			if m.fields[2].Value() == "local" {
				m.fields[3].Placeholder = "https://server.example/v1"
			}
			m.fields[providerKeyField].Placeholder = config.Text("Cole sua chave da API", "Paste your API key")
		}
		count := m.formRows()
		indices := m.fieldIndices()
		start := max(0, slices.Index(indices, m.focus)-count+1)
		for _, i := range indices[start:min(len(indices), start+count)] {
			label := fieldLabel(m.labels[i])
			if i == m.focus {
				label = "› " + label
			}
			lines = append(lines, line(label))
			m.fields[i].Width = max(8, width-2)
			lines = append(lines, line(choiceView(m.fields[i], fieldOptions(m.labels[i]))))
		}
		hint := "Tab/↑↓ fields · Ctrl+S save · Esc back"
		if m.screen == "provider" {
			hint = config.Text("[Salvar] [Catálogo] [Mais] · Esc", "[Save] [Catalog] [More] · Esc")
			if m.fields[8].Value() == "false" {
				hint = config.Text("Ctrl+G salvar e ativar · ", "Ctrl+G save and enable · ") + hint
			}
		}
		lines = append(lines, line(hint))
	}
	note := m.note
	if note == "" && m.screen == "provider" && m.fields[providerAuthField].Value() == "api_key" {
		note = config.Text("Ao salvar: chave no JSON local, sem criptografia.", "On save: key in local JSON, unencrypted.")
	}
	if m.testing {
		note = "Testing connection… Esc cancels."
	}
	if m.screen == "provider" && m.dirty() {
		lines = append(lines, line(config.Text("Alterações não salvas", "Unsaved changes")))
	}
	if m.screen == "provider" && len(m.fields) > 9 && m.fields[9].Value() == "true" {
		lines = append(lines, line(config.Text("TLS sem validação: certificado e identidade não verificados.", "Insecure TLS: certificate and server identity are not verified.")))
	}
	lines = append(lines, m.noteLines(note)...)
	return styles.BaseStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(t.TextMuted()).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
func (m *providerDialogCmp) BindingKeys() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")), key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "test connection")), key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))}
}

func (m *providerDialogCmp) listRows() int {
	if m.height == 0 {
		return 10
	}
	return max(1, min(10, m.height-8))
}
func (m *providerDialogCmp) formRows() int {
	if m.height == 0 {
		return m.fieldCount()
	}
	available := m.height - 10
	if m.screen == "provider" && len(m.fields) > 9 && m.fields[9].Value() == "true" {
		available--
	}
	return max(1, min(m.fieldCount(), available/2))
}

func (m *providerDialogCmp) fieldCount() int {
	return len(m.fieldIndices())
}

func (m *providerDialogCmp) fieldIndices() []int {
	if m.screen == "provider" {
		indices := []int{1, 3, providerAuthField}
		if m.advanced {
			indices = []int{0, 1, 2, 3, providerAuthField}
		}
		switch m.fields[providerAuthField].Value() {
		case "api_key":
			indices = append(indices, providerKeyField)
		case "environment":
			indices = append(indices, 4)
		}
		if m.advanced {
			indices = append(indices, 5, 6, 7)
		}
		if m.advanced || m.fields[5].Value() == "chat_completions" || m.fields[5].Value() == "responses" || m.fields[9].Value() == "true" {
			indices = append(indices, 9)
		}
		return append(indices, 8)
	}
	indices := make([]int, len(m.fields))
	for i := range indices {
		indices[i] = i
	}
	return indices
}

func (m *providerDialogCmp) Setup() {
	path, _ := config.Paths(config.WorkingDirectory())
	m.note = config.Text("Configuração · 1/3 · salve o provedor para escolher o modelo. Arquivo: ", "Setup · 1/3 · save a provider to choose a model. File: ") + path
}

func (m *providerDialogCmp) dirty() bool {
	if len(m.baseline) != len(m.fields) {
		return false
	}
	for i := range m.fields {
		if m.fields[i].Value() != m.baseline[i] {
			return true
		}
	}
	return false
}
