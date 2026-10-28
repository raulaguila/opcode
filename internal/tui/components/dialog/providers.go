package dialog

import (
	"context"
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

type OpenProviderDialogMsg struct{ Setup bool }
type CloseProviderDialogMsg struct{}
type ProviderSettingsSavedMsg struct{ ProviderChanged bool }
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
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.request++
	m.testing = false
}
func (m *providerDialogCmp) setFields(labels, values []string) {
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
	m.note = "Ctrl+T tests metadata only; Ctrl+S saves. Credentials stay in your environment."
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
	m.setFields([]string{"Profile ID", "Name", "Backend (local/openai/anthropic/gemini/groq/openrouter/xai)", "API base URL", "API key environment variable", "API (empty/chat_completions/responses/ollama)", "Context window (0 uses catalog)", "Default output tokens (0 uses catalog)", "Disabled (true/false)"}, []string{id, profile.Name, string(profile.Backend), profile.BaseURL, profile.APIKeyEnv, profile.API, strconv.FormatInt(profile.ContextWindow, 10), strconv.FormatInt(profile.DefaultMaxTokens, 10), strconv.FormatBool(profile.Disabled)})
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
	p := config.ProviderProfile{Name: value(1), Backend: models.ModelProvider(value(2)), Provider: config.Provider{BaseURL: value(3), APIKeyEnv: value(4), API: value(5), APIKey: m.originalKey}}
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
	p.Disabled, err = strconv.ParseBool(value(8))
	if err != nil {
		return "", p, fmt.Errorf("disabled must be true or false")
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
	if m.screen == "provider" {
		var id string
		var p config.ProviderProfile
		id, p, err = m.readProfile()
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
	return util.CmdHandler(ProviderSettingsSavedMsg{ProviderChanged: changed})
}
func (m *providerDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			m.note = fmt.Sprintf(config.Text("Conectado · %d modelos disponíveis. Ctrl+S salva.", "Connected · %d models available. Ctrl+S saves this connection."), msg.count)
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.MouseMsg:
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
				start := max(0, m.focus-count+1)
				i := start + (msg.Y-3)/2
				if msg.Y >= 3 && msg.Y < 3+count*2 && i < len(m.fields) {
					m.fields[m.focus].Blur()
					m.focus = i
					return m, m.fields[i].Focus()
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
				return m, m.Open()
			}
			m.Close()
			return m, util.CmdHandler(CloseProviderDialogMsg{})
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
		case "ctrl+s":
			return m, m.save()
		case "ctrl+a":
			if m.screen == "provider" {
				m.advanced = !m.advanced
				if !m.advanced {
					m.focus = min(m.focus, 4)
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
			m.focus = (m.focus + delta + m.fieldCount()) % m.fieldCount()
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
				label += " · legacy"
			} else {
				p := config.Get().ProviderProfiles[row.id]
				label = p.Name + " · " + row.id + " (" + string(p.Backend) + ")"
			}
			style := base
			if i == m.selected {
				style = style.Foreground(t.Background()).Background(t.Primary()).Bold(true)
			}
			lines = append(lines, style.Render(ansi.Truncate(label, width, "…")))
		}
		lines = append(lines, line("N add · Enter edit · P preferences · I integrations · Esc close"))
	} else {
		// Keep the focused field visible on small terminals; labels remain attached.
		count := m.formRows()
		start := max(0, m.focus-count+1)
		for i := start; i < min(m.fieldCount(), start+count); i++ {
			label := m.labels[i]
			if i == m.focus {
				label = "› " + label
			}
			lines = append(lines, line(label))
			m.fields[i].Width = max(8, width-2)
			lines = append(lines, line(m.fields[i].View()))
		}
		hint := "Tab/↑↓ fields · Ctrl+S save · Esc back"
		if m.screen == "provider" {
			hint = config.Text("Ctrl+A avançado · Ctrl+T catálogo · Ctrl+S salvar", "Ctrl+A advanced · Ctrl+T metadata · Ctrl+S save")
		}
		lines = append(lines, line(hint))
	}
	note := m.note
	if m.testing {
		note = "Testing connection… Esc cancels."
	}
	lines = append(lines, line(note))
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
	return max(1, min(m.fieldCount(), (m.height-8)/2))
}

func (m *providerDialogCmp) fieldCount() int {
	if m.screen == "provider" && !m.advanced {
		return min(5, len(m.fields))
	}
	return len(m.fields)
}

func (m *providerDialogCmp) Setup() {
	path, _ := config.Paths(config.WorkingDirectory())
	m.note = config.Text("Configuração · 1/3 · salve o provedor para escolher o modelo. Arquivo: ", "Setup · 1/3 · save a provider to choose a model. File: ") + path
}
