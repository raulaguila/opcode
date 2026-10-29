package dialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (m *providerDialogCmp) settings() tea.Cmd {
	m.Close()
	m.screen = "settings"
	m.selected = 0
	m.settingsFilter = textinput.New()
	m.settingsFilter.Prompt = config.Text("Buscar: ", "Search: ")
	m.settingsFilter.CharLimit = 100
	m.settingsFilter.Focus()
	m.note = config.Text("Enter edita · Ctrl+A opções avançadas · Ctrl+E valores e origens · Esc volta", "Enter edits · Ctrl+A advanced · Ctrl+E effective values and origins · Esc back")
	return textinput.Blink
}
func (m *providerDialogCmp) filteredSettings() []config.Setting {
	result := []config.Setting{}
	q := strings.ToLower(strings.TrimSpace(m.settingsFilter.Value()))
	for _, s := range config.UserSettings {
		if s.Advanced && !m.advanced {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(s.Path+" "+s.Label+" "+s.Category+" "+s.Help), q) {
			result = append(result, s)
		}
	}
	return result
}
func (m *providerDialogCmp) experienceUpdate(msg tea.Msg) (tea.Cmd, bool) {
	if m.screen != "settings" && m.screen != "setting" && m.screen != "effective" && m.screen != "presets" {
		return nil, false
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		return nil, true
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			if m.screen == "settings" || m.screen == "presets" {
				return m.Open(), true
			}
			return m.settings(), true
		}
		if m.screen == "setting" {
			if choiceKey(&m.fields[0], settingOptions(m.settingPath), msg) {
				return nil, true
			}
			switch key.String() {
			case "ctrl+s":
				if m.allowSave != nil {
					if err := m.allowSave(); err != nil {
						m.note = err.Error()
						return nil, true
					}
				}
				err := config.SaveUserSetting(m.settingPath, m.fields[0].Value(), false)
				if err != nil {
					m.note = err.Error()
					return nil, true
				}
				m.settings()
				return util.CmdHandler(UserPreferencesChangedMsg{}), true
			case "ctrl+r":
				if m.allowSave != nil {
					if err := m.allowSave(); err != nil {
						m.note = err.Error()
						return nil, true
					}
				}
				if err := config.SaveUserSetting(m.settingPath, "", true); err != nil {
					m.note = err.Error()
					return nil, true
				}
				m.settings()
				return util.CmdHandler(UserPreferencesChangedMsg{}), true
			}
			var cmd tea.Cmd
			m.fields[0], cmd = m.fields[0].Update(msg)
			return cmd, true
		}
		if m.screen == "presets" {
			switch key.String() {
			case "up":
				m.selected = (m.selected + len(config.ProviderPresets) - 1) % len(config.ProviderPresets)
			case "down":
				m.selected = (m.selected + 1) % len(config.ProviderPresets)
			case "enter":
				p := config.ProviderPresets[m.selected]
				cmd := m.edit(nil)
				m.fields[0].SetValue(p.ID)
				m.fields[1].SetValue(p.Profile.Name)
				m.fields[2].SetValue(string(p.Profile.Backend))
				m.fields[3].SetValue(p.Profile.BaseURL)
				m.fields[4].SetValue(p.Profile.APIKeyEnv)
				m.fields[5].SetValue(p.Profile.API)
				m.fields[6].SetValue(fmt.Sprint(p.Profile.ContextWindow))
				m.fields[7].SetValue(fmt.Sprint(p.Profile.DefaultMaxTokens))
				return cmd, true
			}
			return nil, true
		}
		if m.screen == "effective" {
			switch key.String() {
			case "up":
				m.selected = max(0, m.selected-1)
			case "down":
				m.selected++
			case "pgup":
				m.selected = max(0, m.selected-10)
			case "pgdown":
				m.selected += 10
			default:
				var cmd tea.Cmd
				m.settingsFilter, cmd = m.settingsFilter.Update(msg)
				m.selected = 0
				return cmd, true
			}
			return nil, true
		}
		list := m.filteredSettings()
		switch key.String() {
		case "ctrl+a":
			m.advanced = !m.advanced
			m.selected = 0
		case "ctrl+e":
			m.screen = "effective"
			m.selected = 0
			m.settingsFilter.SetValue("")
		case "up":
			m.selected = max(0, m.selected-1)
		case "down":
			m.selected = min(max(0, len(list)-1), m.selected+1)
		case "enter":
			if len(list) > 0 {
				s := list[min(m.selected, len(list)-1)]
				v := config.EffectiveSetting(s.Path)
				m.settingPath = s.Path
				m.screen = "setting"
				m.setFields([]string{s.Label}, []string{fmt.Sprint(v.Value)})
				m.note = localizedSettingHelp(s.Help) + " · " + v.Origin + config.Text(" · Ctrl+S salvar · Ctrl+R restaurar", " · Ctrl+S save · Ctrl+R reset")
				return textinput.Blink, true
			}
		default:
			var cmd tea.Cmd
			m.settingsFilter, cmd = m.settingsFilter.Update(msg)
			m.selected = 0
			return cmd, true
		}
		return nil, true
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		if mouse.Button == tea.MouseButtonWheelUp {
			m.selected = max(0, m.selected-1)
		}
		if mouse.Button == tea.MouseButtonWheelDown {
			m.selected++
		}
		if mouse.Button == tea.MouseButtonLeft && mouse.Action == tea.MouseActionPress && mouse.Y >= 4 {
			start := max(0, m.selected-max(1, m.height-10)+1)
			m.selected = max(0, mouse.Y-4+start)
			return m.experienceUpdate(tea.KeyMsg{Type: tea.KeyEnter})
		}
		return nil, true
	}
	var cmd tea.Cmd
	if m.screen == "setting" && len(m.fields) > 0 {
		if len(settingOptions(m.settingPath)) == 0 {
			m.fields[0], cmd = m.fields[0].Update(msg)
		}
	} else if m.screen == "settings" || m.screen == "effective" {
		before := m.settingsFilter.Value()
		m.settingsFilter, cmd = m.settingsFilter.Update(msg)
		if before != m.settingsFilter.Value() {
			m.selected = 0
		}
	}
	return cmd, true
}

type UserPreferencesChangedMsg struct{}

func (m *providerDialogCmp) experienceView() string {
	width := m.contentWidth()
	base := styles.BaseStyle().Width(width)
	line := func(s string) string { return base.Render(ansi.Truncate(s, width, "…")) }
	lines := []string{}
	title := config.Text("Preferências", "Preferences")
	switch m.screen {
	case "presets":
		title = config.Text("Adicionar conexão", "Add connection")
		lines = append(lines, line(config.Text("Escolha um preset; revise antes de salvar.", "Choose a preset; review before saving.")))
		for i, p := range config.ProviderPresets {
			mark := "  "
			if i == m.selected {
				mark = "› "
			}
			lines = append(lines, line(mark+p.Profile.Name))
		}
	case "settings", "effective":
		if m.screen == "effective" {
			title = config.Text("Valores efetivos e origem", "Effective values and origins")
		}
		m.settingsFilter.Width = max(4, width-10)
		lines = append(lines, line(m.settingsFilter.View()))
		rows := max(1, m.height-10)
		if m.screen == "settings" {
			list := m.filteredSettings()
			effective := map[string]config.EffectiveValue{}
			for _, v := range config.EffectiveConfiguration() {
				effective[v.Path] = v
			}
			if len(list) == 0 {
				lines = append(lines, line(config.Text("Nenhuma preferência encontrada.", "No preferences found.")))
			}
			m.selected = min(m.selected, max(0, len(list)-1))
			start := max(0, m.selected-rows+1)
			for i := start; i < min(len(list), start+rows); i++ {
				s := list[i]
				v := effective[s.Path]
				mark := "  "
				if i == m.selected {
					mark = "› "
				}
				lines = append(lines, line(mark+localizedCategory(s.Category)+" · "+localizedSettingLabel(s.Label)+" = "+fmt.Sprint(v.Value)))
			}
			if m.advanced {
				lines = append(lines, line(config.Text("Opções avançadas visíveis", "Advanced options visible")))
			}
		} else {
			q := strings.ToLower(m.settingsFilter.Value())
			values := []config.EffectiveValue{}
			for _, v := range config.EffectiveConfiguration() {
				if q == "" || strings.Contains(strings.ToLower(v.Path), q) {
					values = append(values, v)
				}
			}
			m.selected = min(m.selected, max(0, len(values)-1))
			start := max(0, m.selected-rows+1)
			for i := start; i < min(len(values), start+rows); i++ {
				v := values[i]
				lines = append(lines, line(v.Path+" = "+fmt.Sprint(v.Value)+" · "+localizedOrigin(v.Origin)))
			}
		}
	case "setting":
		title = m.settingPath
		v := config.EffectiveSetting(m.settingPath)
		lines = append(lines, line(config.Text("Origem: ", "Origin: ")+localizedOrigin(v.Origin)), line(localizedSettingLabel(m.labels[0])), line(choiceView(m.fields[0], settingOptions(m.settingPath))))
	}
	title = styles.BaseStyle().Foreground(theme.CurrentTheme().Primary()).Bold(true).Render(title)
	lines = append([]string{line(title)}, lines...)
	lines = append(lines, line(m.note))
	return styles.BaseStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(theme.CurrentTheme().TextMuted()).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func localizedSettingLabel(s string) string {
	parts := strings.Split(s, " / ")
	if len(parts) == 2 {
		return config.Text(parts[0], parts[1])
	}
	return s
}
func localizedCategory(s string) string {
	switch s {
	case "interface":
		return config.Text("Interface", "Interface")
	case "sessions":
		return config.Text("Conversas", "Sessions")
	case "execution":
		return config.Text("Execução", "Execution")
	case "shortcuts":
		return config.Text("Atalhos", "Shortcuts")
	}
	return s
}

func localizedOrigin(origin string) string {
	switch origin {
	case "default":
		return config.Text("padrão", "default")
	case "project":
		return config.Text("projeto", "project")
	case "global":
		return "global"
	case "error":
		return config.Text("erro", "error")
	}
	if strings.HasPrefix(origin, "environment (") {
		return config.Text("ambiente (", "environment (") + strings.TrimPrefix(origin, "environment (")
	}
	return origin
}
