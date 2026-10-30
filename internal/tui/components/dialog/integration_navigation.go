package dialog

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (m *integrationDialogCmp) handleIntegrationKey(msg tea.KeyMsg) connectionAction {
	if action := m.openFocusedJSONEditor(msg); action.handled {
		return action
	}
	if m.screen == "edit" && len(m.fields) > m.focus && choiceKey(&m.fields[m.focus], fieldOptions(m.labels[m.focus]), msg) {
		m.invalidateIntegrationTest()
		return connectionHandled(nil)
	}
	if msg.String() == "esc" {
		return connectionHandled(m.leaveIntegrationScreen())
	}
	switch m.screen {
	case "result", "oauth":
		m.scrollIntegrationResult(msg.String())
		return connectionHandled(nil)
	case "list":
		return connectionHandled(m.handleIntegrationListKey(msg))
	}
	return m.handleIntegrationFormKey(msg)
}

func (m *integrationDialogCmp) handleIntegrationFormKey(msg tea.KeyMsg) connectionAction {
	switch msg.String() {
	case "ctrl+s":
		return connectionHandled(m.saveIntegration())
	case "ctrl+t":
		return connectionHandled(m.testIntegration())
	case "tab", "down", "enter", "up", "shift+tab":
		delta := 1
		if msg.String() == "up" || msg.String() == "shift+tab" {
			delta = -1
		}
		return connectionHandled(m.focusVisibleField(m.visibleFields(), delta))
	}
	return connectionAction{}
}

func (m *integrationDialogCmp) handleIntegrationListKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+a":
		return m.oauthAction("login")
	case "ctrl+d":
		return m.oauthAction("logout")
	case "ctrl+u":
		return m.oauthAction("status")
	case "up", "down":
		if len(m.entries) > 0 {
			delta := 1
			if msg.String() == "up" {
				delta = -1
			}
			m.selected = (m.selected + delta + len(m.entries)) % len(m.entries)
		}
	case "enter":
		if len(m.entries) > 0 {
			return m.edit(m.entries[m.selected])
		}
	case "r":
		if m.resumeDraft != nil {
			m.resumeDraft()
			return textinput.Blink
		}
	case "n":
		return m.edit(config.Integration{Kind: "mcp", MCP: config.MCPServer{Type: config.MCPStdio}})
	case "l":
		return m.edit(config.Integration{Kind: "lsp"})
	}
	return nil
}

func (m *integrationDialogCmp) leaveIntegrationScreen() tea.Cmd {
	if m.screen == "oauth" {
		m.Close()
		return m.Open()
	}
	if m.testing {
		m.Close()
		m.note = "Test cancelled; edits are preserved."
		return nil
	}
	if m.screen == "result" {
		m.screen = "edit"
		m.note = "Tab fields · Ctrl+T test · Ctrl+S save · Esc back"
		return nil
	}
	if m.screen == "list" {
		m.Close()
		return util.CmdHandler(CloseIntegrationDialogMsg{})
	}
	saved := *m
	saved.cancel, saved.testing, saved.resumeDraft = nil, false, nil
	cmd := m.Open()
	m.resumeDraft = func() {
		width, height, request := m.width, m.height, m.request+1
		*m = saved
		m.width, m.height, m.request = width, height, request
	}
	m.note = config.Text("R retoma a edição preservada", "R resumes preserved edits")
	return cmd
}

func (m *integrationDialogCmp) scrollIntegrationResult(key string) {
	if key == "up" {
		m.resultOffset = max(0, m.resultOffset-1)
	}
	if key == "down" {
		if m.screen == "result" {
			m.resultOffset = min(max(0, len(m.result)-1), m.resultOffset+1)
		} else {
			m.resultOffset++
		}
	}
}

func (m *integrationDialogCmp) openFocusedJSONEditor(msg tea.KeyMsg) connectionAction {
	if m.screen != "edit" || msg.String() != "enter" {
		return connectionAction{}
	}
	label := m.labels[m.focus]
	var err error
	switch {
	case strings.HasPrefix(label, "Arguments"):
		m.arguments, err = newArgumentList(m.fields[m.focus].Value())
	case strings.HasPrefix(label, "Headers from"), strings.HasPrefix(label, "Child environment"):
		m.arguments, err = newPairList(m.fields[m.focus].Value())
	default:
		return connectionAction{}
	}
	if err != nil {
		m.note = err.Error()
		return connectionHandled(nil)
	}
	m.Close()
	m.fields[m.focus].Blur()
	return connectionHandled(textinput.Blink)
}

func (m *integrationDialogCmp) updateArgumentEditor(msg tea.Msg) tea.Cmd {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
	}
	done, save, cmd := m.arguments.update(msg)
	if done {
		if save {
			m.fields[m.focus].SetValue(m.arguments.value())
		}
		m.arguments = nil
		m.fields[m.focus].Focus()
	}
	return cmd
}

func (m *integrationDialogCmp) invalidateIntegrationTest() {
	if m.testing {
		m.Close()
		m.note = "Edited; test again for current settings."
	}
}

func (m *integrationDialogCmp) updateIntegrationInput(msg tea.Msg) tea.Cmd {
	if m.screen != "edit" || len(m.fields) == 0 {
		return nil
	}
	var cmd tea.Cmd
	m.fields[m.focus], cmd = m.fields[m.focus].Update(msg)
	if _, ok := msg.(tea.KeyMsg); ok {
		m.invalidateIntegrationTest()
	}
	return cmd
}

func (m *integrationDialogCmp) handleIntegrationMouse(msg tea.MouseMsg) connectionAction {
	if action := m.integrationFieldClick(msg); action.handled {
		return action
	}
	if m.screen == "list" && len(m.entries) > 0 {
		if msg.Button == tea.MouseButtonWheelUp {
			m.selected = max(0, m.selected-1)
		}
		if msg.Button == tea.MouseButtonWheelDown {
			m.selected = min(len(m.entries)-1, m.selected+1)
		}
	}
	if m.screen == "result" {
		if msg.Button == tea.MouseButtonWheelUp {
			m.scrollIntegrationResult("up")
		}
		if msg.Button == tea.MouseButtonWheelDown {
			m.scrollIntegrationResult("down")
		}
		return connectionHandled(nil)
	}
	return m.integrationRowClick(msg)
}

func (m *integrationDialogCmp) integrationRowClick(msg tea.MouseMsg) connectionAction {
	if m.screen != "list" || !m.isFormClick(msg) {
		return connectionAction{}
	}
	rows := m.listRows()
	start := max(0, m.selected-rows+1)
	i := msg.Y - 3 + start
	if msg.Y < 3 || msg.Y >= 3+rows || i < 0 || i >= len(m.entries) {
		return connectionAction{}
	}
	m.selected = i
	return connectionHandled(m.edit(m.entries[i]))
}

func (m *integrationDialogCmp) integrationFieldClick(msg tea.MouseMsg) connectionAction {
	if m.screen != "edit" || !m.isFormClick(msg) {
		return connectionAction{}
	}
	indices, count, start := m.integrationFieldWindow()
	i := start + (msg.Y-3)/2
	if msg.Y < 3 || msg.Y >= 3+count*2 || i >= len(indices) {
		return connectionAction{}
	}
	m.fields[m.focus].Blur()
	m.focus = indices[i]
	return connectionHandled(m.fields[m.focus].Focus())
}

func (m *providerDialogCmp) isFormClick(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.X >= 3 && msg.X < m.contentWidth()+3
}

func (m *integrationDialogCmp) finishIntegrationProbe(result integrationProbeMsg) {
	if result.request != m.request {
		return
	}
	m.testing, m.cancel = false, nil
	if result.err != nil {
		m.note = config.Text("Falha no teste: ", "Test failed: ") + result.err.Error()
		return
	}
	m.result = result.lines
	if len(m.result) > 1024 {
		m.result = m.result[:1024]
	}
	m.resultOffset, m.screen = 0, "result"
	m.note = "↑↓ scroll results · Esc returns to the preserved form"
}
