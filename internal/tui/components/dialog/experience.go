package dialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
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
	if result0, result1, handled := experienceUpdateTeaKeyMsg(m, msg); handled {
		return result0, result1
	}
	if result0, result1, handled := experienceUpdateTeaMouseMsg(m, msg); handled {
		return result0, result1
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
		experienceViewProviderDialogCmpSettings(m, width, line, &lines, &title)
	case "setting":
		title = localizedSettingLabel(m.labels[0])
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
	if after, ok := strings.CutPrefix(origin, "environment ("); ok {
		return config.Text("ambiente (", "environment (") + after
	}
	return origin
}

func experienceUpdateProviderDialogCmpEnter(m *providerDialogCmp) (extractedResult0 tea.Cmd, extractedResult1 bool, handled bool) {
	if m.selected < 0 || m.selected >= len(config.ProviderPresets) {
		return nil, true, true
	}
	p := config.ProviderPresets[m.selected]
	cmd := m.edit(nil)
	id := p.ID
	for n := 2; ; n++ {
		_, profileExists := config.Get().ProviderProfiles[id]
		_, legacyExists := config.Get().Providers[models.ModelProvider(id)]
		if !profileExists && !legacyExists {
			break
		}
		id = fmt.Sprintf("%s-%d", p.ID, n)
	}
	m.fields[0].SetValue(id)
	m.fields[1].SetValue(p.Profile.Name)
	m.fields[2].SetValue(string(p.Profile.Backend))
	m.fields[3].SetValue(p.Profile.BaseURL)
	m.fields[4].SetValue(p.Profile.APIKeyEnv)
	if p.Profile.Backend != models.ProviderLocal || p.ID == "openai-compatible" {
		m.fields[providerAuthField].SetValue("api_key")
	}
	m.fields[5].SetValue(p.Profile.API)
	m.fields[6].SetValue(fmt.Sprint(p.Profile.ContextWindow))
	m.fields[7].SetValue(fmt.Sprint(p.Profile.DefaultMaxTokens))
	return cmd, true, true

}

func experienceUpdateProviderDialogCmpCtrlS(m *providerDialogCmp) (extractedResult0 tea.Cmd, extractedResult1 bool, handled bool) {
	if m.allowSave != nil {
		if err := m.allowSave(); err != nil {
			m.note = err.Error()
			return nil, true, true
		}
	}
	err := config.SaveUserSetting(m.settingPath, m.fields[0].Value(), false)
	if err != nil {
		m.note = err.Error()
		return nil, true, true
	}
	m.settings()
	return util.CmdHandler(UserPreferencesChangedMsg{}), true, true

}

func experienceUpdateProviderDialogCmpCtrlR(m *providerDialogCmp) (extractedResult0 tea.Cmd, extractedResult1 bool, handled bool) {
	if m.allowSave != nil {
		if err := m.allowSave(); err != nil {
			m.note = err.Error()
			return nil, true, true
		}
	}
	if err := config.SaveUserSetting(m.settingPath, "", true); err != nil {
		m.note = err.Error()
		return nil, true, true
	}
	m.settings()
	return util.CmdHandler(UserPreferencesChangedMsg{}), true, true

}

func experienceUpdateProviderDialogCmpEsc(m *providerDialogCmp) (extractedResult0 tea.Cmd, extractedResult1 bool, handled bool) {
	if m.screen == "settings" || m.screen == "presets" {
		return m.Open(), true, true
	}
	return m.settings(), true, true

}

func experienceViewProviderDialogCmpSettings(m *providerDialogCmp, width int, line func(s string) string, lines *[]string, title *string) {
	if m.screen == "effective" {
		(*title) = config.Text("Valores efetivos e origem", "Effective values and origins")
	}
	m.settingsFilter.Width = max(4, width-10)
	(*lines) = append((*lines), line(m.settingsFilter.View()))
	rows := max(1, m.height-11)
	experienceViewProviderDialogCmpSettingsSettings(m, line, lines, rows)
}

func experienceUpdateTeaKeyMsg(m *providerDialogCmp, msg tea.Msg) (extractedResult0 tea.Cmd, extractedResult1 bool, handled bool) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false, false
	}

	switch key.String() {
	case "esc":
		if result0, result1, handled := experienceUpdateProviderDialogCmpEsc(m); handled {
			return result0, result1, true
		}
	}
	if result0, result1, result2, handled := experienceUpdateTeaKeyMsgSetting(m, msg, &key); handled {
		return result0, result1, result2
	}
	if result0, result1, result2, handled := experienceUpdateTeaKeyMsgPresets(m, &key); handled {
		return result0, result1, result2
	}
	if result0, result1, result2, handled := experienceUpdateTeaKeyMsgEffective(m, msg, &key); handled {
		return result0, result1, result2
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
			return textinput.Blink, true, true
		}
	default:
		var cmd tea.Cmd
		m.settingsFilter, cmd = m.settingsFilter.Update(msg)
		m.selected = 0
		return cmd, true, true
	}
	return nil, true, true
}

func experienceUpdateTeaKeyMsgSetting(m *providerDialogCmp, msg tea.Msg, key *tea.KeyMsg) (extractedResult0 tea.Cmd, extractedResult1 bool, extractedResult2 bool, handled bool) {
	if m.screen == "setting" {
		if choiceKey(&m.fields[0], settingOptions(m.settingPath), msg) {
			return nil, true, true, true
		}
		switch key.String() {
		case "ctrl+s":
			if result0, result1, handled := experienceUpdateProviderDialogCmpCtrlS(m); handled {
				return result0, result1, true, true
			}
		case "ctrl+r":
			if result0, result1, handled := experienceUpdateProviderDialogCmpCtrlR(m); handled {
				return result0, result1, true, true
			}
		}
		var cmd tea.Cmd
		m.fields[0], cmd = m.fields[0].Update(msg)
		return cmd, true, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func experienceUpdateTeaMouseMsg(m *providerDialogCmp, msg tea.Msg) (extractedResult0 tea.Cmd, extractedResult1 bool, handled bool) {
	if mouse, ok := msg.(tea.MouseMsg); ok {
		if mouse.Button == tea.MouseButtonWheelUp {
			m.selected = max(0, m.selected-1)
		}
		if mouse.Button == tea.MouseButtonWheelDown {
			m.selected++
			if m.screen == "presets" {
				m.selected = min(m.selected, max(0, len(config.ProviderPresets)-1))
			}
		}
		if result0, result1, result2, handled := experienceUpdateTeaMouseMsgMouseXContentWidth(m, &mouse); handled {
			return result0, result1, result2
		}
		return nil, true, true
	}
	return extractedResult0, extractedResult1, false
}

func experienceUpdateTeaKeyMsgPresets(m *providerDialogCmp, key *tea.KeyMsg) (extractedResult0 tea.Cmd, extractedResult1 bool, extractedResult2 bool, handled bool) {
	if m.screen == "presets" {
		switch key.String() {
		case "up":
			m.selected = (m.selected + len(config.ProviderPresets) - 1) % len(config.ProviderPresets)
		case "down":
			m.selected = (m.selected + 1) % len(config.ProviderPresets)
		case "enter":
			if result0, result1, handled := experienceUpdateProviderDialogCmpEnter(m); handled {
				return result0, result1, true, true
			}
		}
		return nil, true, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func experienceViewProviderDialogCmpSettingsSettings(m *providerDialogCmp, line func(s string) string, lines *[]string, rows int) {
	if m.screen == "settings" {
		list := m.filteredSettings()
		effective := map[string]config.EffectiveValue{}
		for _, v := range config.EffectiveConfiguration() {
			effective[v.Path] = v
		}
		if len(list) == 0 {
			(*lines) = append((*lines), line(config.Text("Nenhuma preferência encontrada.", "No preferences found.")))
		}
		m.selected = min(m.selected, max(0, len(list)-1))
		start := max(0, m.selected-rows+1)
		if len(list) > 0 {
			(*lines) = append((*lines), line(localizedCategory(list[m.selected].Category)))
		}
		experienceViewProviderDialogCmpSettingsSettingsStart(m, line, lines, rows, list, effective, start)
		if m.advanced {
			(*lines) = append((*lines), line(config.Text("Opções avançadas visíveis", "Advanced options visible")))
		}
	} else {
		q := strings.ToLower(m.settingsFilter.Value())
		values := []config.EffectiveValue{}
		filterEffectiveSettings(q, &values)
		m.selected = min(m.selected, max(0, len(values)-1))
		start := max(0, m.selected-rows+1)
		for i := start; i < min(len(values), start+rows); i++ {
			v := values[i]
			(*lines) = append((*lines), line(v.Path+" = "+fmt.Sprint(v.Value)+" · "+localizedOrigin(v.Origin)))
		}
	}
}

func experienceUpdateTeaMouseMsgMouseXContentWidth(m *providerDialogCmp, mouse *tea.MouseMsg) (extractedResult0 tea.Cmd, extractedResult1 bool, extractedResult2 bool, handled bool) {
	if mouse.Button == tea.MouseButtonLeft && mouse.Action == tea.MouseActionPress && mouse.Y >= 4 && mouse.X >= 3 && mouse.X < m.contentWidth()+3 {
		if m.screen == "setting" {
			return nil, true, true, true
		}
		if m.screen == "presets" && mouse.Y-4 >= len(config.ProviderPresets) {
			return nil, true, true, true
		}
		offset := 4
		if m.screen == "settings" {
			offset = 5
		}
		if mouse.Y < offset {
			return nil, true, true, true
		}
		start := max(0, m.selected-max(1, m.height-11)+1)
		m.selected = max(0, mouse.Y-offset+start)
		returned1Value0, returned1Value1 := m.experienceUpdate(tea.KeyMsg{Type: tea.KeyEnter})
		return returned1Value0, returned1Value1, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func filterEffectiveSettings(q string, values *[]config.EffectiveValue) {
	for _, v := range config.EffectiveConfiguration() {
		if q == "" || strings.Contains(strings.ToLower(v.Path), q) {
			(*values) = append((*values), v)
		}
	}
}

func experienceUpdateTeaKeyMsgEffective(m *providerDialogCmp, msg tea.Msg, key *tea.KeyMsg) (extractedResult0 tea.Cmd, extractedResult1 bool, extractedResult2 bool, handled bool) {
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
			return cmd, true, true, true
		}
		return nil, true, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func experienceViewProviderDialogCmpSettingsSettingsStart(m *providerDialogCmp, line func(s string) string, lines *[]string, rows int, list []config.Setting, effective map[string]config.EffectiveValue, start int) {
	for i := start; i < min(len(list), start+rows); i++ {
		s := list[i]
		v := effective[s.Path]
		mark := "  "
		if i == m.selected {
			mark = "› "
		}
		(*lines) = append((*lines), line(mark+localizedSettingLabel(s.Label)+" = "+fmt.Sprint(v.Value)))
	}
}
