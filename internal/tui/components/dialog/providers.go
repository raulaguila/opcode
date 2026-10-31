package dialog

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

const (
	providerIDField = iota
	providerNameField
	providerBackendField
	providerURLField
	providerEnvironmentField
	providerAPIField
	providerContextField
	providerOutputField
	providerActiveField
	providerTLSField
	providerAuthField
	providerKeyField
)

type OpenProviderDialogMsg struct {
	Setup          bool
	Provider       models.ModelProvider
	ReturnToModels bool
}
type CloseProviderDialogMsg struct{}
type ProviderSettingsSavedMsg struct {
	ProviderChanged bool
	Provider        models.ModelProvider
	Disabled        bool
	Deleted         bool
}

// OpenForProvider edits the exact connection whose catalog is visible. It does
// not resolve credentials, probe a server or change the selected model.
func (m *providerDialogCmp) OpenForProvider(provider models.ModelProvider) tea.Cmd {
	m.Open()
	id, profile := strings.CutPrefix(string(provider), "profile:")
	for i, row := range m.rows {
		if row.id == id && row.legacy != profile {
			m.selected = i
			return m.edit(&row)
		}
	}
	return nil
}

type providerVaultRollbackMsg struct{ err error }

type providerVaultSaveMsg struct {
	id       string
	profile  config.ProviderProfile
	prepared *config.PreparedVaultCredential
	err      error
}

type providerTestMsg struct {
	request uint64
	count   int
	err     error
}
type ProviderDialog interface {
	util.Model
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
	originalKeyRef                 string
	saving                         bool
	prepareVault                   func(string, string) (*config.PreparedVaultCredential, error)
	note                           string
	noteFull                       bool
	noteOffset                     int
	testing                        bool
	request                        uint64
	cancel                         context.CancelFunc
	settingsFilter                 textinput.Model
	advanced                       bool
	settingPath                    string
	deleting                       providerRow
	deleteSelected                 bool
}

func NewProviderDialogCmp(allowSave ...func() error) ProviderDialog {
	m := &providerDialogCmp{prepareVault: config.PrepareVaultCredential}
	if len(allowSave) > 0 {
		m.allowSave = allowSave[0]
	}
	return m
}
func (m *providerDialogCmp) Init() tea.Cmd { return nil }

// PendingNativeOperation is queried by the app before quitting. Native vault
// writes cannot be cancelled; their result must commit or roll back first.
func (m *providerDialogCmp) PendingNativeOperation() bool { return m.saving }

func (m *providerDialogCmp) Open() tea.Cmd {
	if m.saving {
		return nil
	}
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
	for id, p := range c.Providers {
		if p == (config.Provider{Disabled: true}) {
			continue // Removed legacy backend marker; suppress environment recreation.
		}
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
	if m.saving {
		return
	}
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
		f := newDialogInput()
		f.CharLimit = 512
		f.Prompt = ""
		f.SetValue(values[i])
		f.SetWidth(max(8, m.contentWidth()-3))
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
	m.originalKeyRef = ""
	id := "local"
	profile := config.ProviderProfile{Name: "Local Ollama", Backend: models.ProviderLocal, BaseURL: "http://127.0.0.1:11434/v1", API: "ollama", ContextWindow: 0, DefaultMaxTokens: 2048}
	if row != nil {
		id = row.id
		m.originalID = id
		m.legacy = row.legacy
		if row.legacy {
			profile = config.ProviderProfile{Name: id, Backend: models.ModelProvider(id), Provider: config.Get().Providers[models.ModelProvider(id)]}
		} else {
			profile = config.Get().ProviderProfiles[id]
		}
		m.originalKeyRef = profile.APIKeyRef
		m.originalKey = config.StoredProviderKey(id, m.legacy)
		if profile.APIKeyRef == "" && profile.APIKeyEnv == "" && m.originalKey == "" {
			profile.APIKeyEnv = config.ProviderEnvironmentName(profile.Backend)
		}
	}
	auth := "none"
	if profile.APIKeyRef != "" {
		auth = "vault"
	} else if profile.APIKeyEnv != "" {
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
	id := m.fieldValue(providerIDField)
	profile := config.ProviderProfile{Name: m.fieldValue(providerNameField), Backend: models.ModelProvider(m.fieldValue(providerBackendField)), BaseURL: m.fieldValue(providerURLField), API: m.fieldValue(providerAPIField)}
	if err := m.validateProfileIdentity(id, profile); err != nil {
		return "", profile, err
	}
	profile, err := m.readProfileLimits(profile)
	if err != nil {
		return "", profile, err
	}
	profile, err = m.readProfileAuthentication(profile)
	if err != nil {
		return "", profile, err
	}
	return id, profile, nil
}

func (m *providerDialogCmp) test() tea.Cmd {
	id, p, err := m.readProfile()
	if err != nil {
		m.note = err.Error()
		return nil
	}
	if m.fields[providerAuthField].Value() == "vault" && m.fields[providerKeyField].Value() != "" {
		p.APIKeyRef, p.APIKey = "", strings.TrimSpace(m.fields[providerKeyField].Value())
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
	if m.saving {
		return nil
	}
	if m.allowSave != nil {
		if err := m.allowSave(); err != nil {
			m.note = err.Error()
			return nil
		}
	}
	result := m.persistSettings()
	if result.pending {
		return result.command
	}
	if result.err != nil {
		m.note = result.err.Error()
		return nil
	}
	changed := m.screen == "provider"
	m.Close()
	return util.CmdHandler(ProviderSettingsSavedMsg{ProviderChanged: changed, Provider: result.destination, Disabled: result.disabled})
}

func (m *providerDialogCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	switch result := msg.(type) {
	case providerVaultRollbackMsg:
		m.saving = false
		m.note = result.err.Error()
		return m, nil
	case providerVaultSaveMsg:
		return m, m.finishVaultSave(result)
	}
	if m.saving {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = size.Width, size.Height
		}
		return m, nil // Native operations must finish commit/rollback before navigation.
	}
	if cmd, handled := m.deleteUpdate(msg); handled {
		return m, cmd
	}
	if m.noteUpdate(msg) {
		return m, nil
	}
	if cmd, handled := m.experienceUpdate(msg); handled {
		return m, cmd
	}
	action := m.handleProviderMessage(msg)
	if action.handled {
		return m, action.command
	}
	return m, m.updateProviderInput(msg)
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
	if m.screen == "delete-provider" {
		return m.deleteView()
	}
	switch m.screen {
	case "settings", "setting", "effective", "presets":
		return m.experienceView()
	}
	t := theme.CurrentTheme()
	width := m.contentWidth()
	base := dialogBody().Width(width)
	line := func(s string) string { return base.Render(ansi.Truncate(interfaceCopy(s), width, "…")) }
	title := "Connections and preferences"
	if m.screen == "provider" {
		title = "Configure provider"
	}
	if m.screen == "preferences" {
		title = "Preferences"
	}
	lines := []string{base.Foreground(t.Primary()).Bold(true).Render(interfaceCopy(title))}
	lines = append(lines, m.providerBodyLines(t, width, base, line)...)
	note := m.note
	if note == "" && m.screen == "provider" && m.fields[providerAuthField].Value() == "api_key" {
		note = config.Text("Ao salvar: chave no JSON local, sem criptografia.", "On save: key in local JSON, unencrypted.")
	}
	if note == "" && m.screen == "provider" && m.fields[providerAuthField].Value() == "vault" {
		note = config.Text("Chave no cofre do SO; apenas referência no JSON.", "Key in the OS vault; only a reference in JSON.")
	}
	if m.saving {
		note = config.Text("Salvando no cofre… aguarde o SO; não é cancelável.", "Saving to vault… wait for the OS; cannot cancel.")
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
	return styles.Panel().Padding(1, 2).Border(lipgloss.NormalBorder()).BorderForeground(t.TextMuted()).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
func (m *providerDialogCmp) BindingKeys() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")), key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "test connection")), key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))}
}

func (m *providerDialogCmp) listRows() int {
	if m.height == 0 {
		return 10
	}
	return max(1, min(10, m.height-9))
}
func (m *providerDialogCmp) formRows() int {
	if m.height == 0 {
		return m.fieldCount()
	}
	available := m.height - 11
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
		case "api_key", "vault":
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

// Metadata is committed on the UI owner; all native rollback runs in a command.
// Keep saving true until its message arrives, including failure to clean up.
