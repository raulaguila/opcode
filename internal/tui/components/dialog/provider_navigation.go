package dialog

import (
	"fmt"
	"slices"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (m *providerDialogCmp) handleProviderMessage(msg tea.Msg) connectionAction {
	switch msg := msg.(type) {
	case providerTestMsg:
		if msg.request != m.request {
			return connectionHandled(nil)
		}
		m.finishCatalogTest(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.MouseMsg:
		return m.handleProviderMouse(msg)
	case tea.KeyMsg:
		return m.handleProviderKey(msg)
	}
	return connectionAction{}
}

func (m *providerDialogCmp) finishCatalogTest(result providerTestMsg) {
	m.testing, m.cancel = false, nil
	if result.err != nil {
		m.note = config.Text("Falha na consulta: ", "Connection test failed: ") + result.err.Error()
	} else {
		m.note = fmt.Sprintf(config.Text("Catálogo consultado · %d modelos. Ferramentas não testadas. Ctrl+S salva.", "Catalog queried · %d models. Tools not tested. Ctrl+S saves."), result.count)
	}
}

func (m *providerDialogCmp) handleProviderKey(msg tea.KeyMsg) connectionAction {
	if msg.String() == "esc" {
		return connectionHandled(m.leaveProviderScreen())
	}
	if m.screen != "list" && len(m.fields) > m.focus && choiceKey(&m.fields[m.focus], fieldOptions(m.labels[m.focus]), msg) {
		m.invalidateCatalogTest()
		return connectionHandled(nil)
	}
	if m.screen == "list" {
		return connectionHandled(m.handleProviderListKey(msg))
	}
	return m.handleProviderFormKey(msg)
}

func (m *providerDialogCmp) handleProviderFormKey(msg tea.KeyMsg) connectionAction {
	switch msg.String() {
	case "ctrl+g":
		if m.screen == "provider" {
			m.fields[providerActiveField].SetValue("true")
			return connectionHandled(m.save())
		}
		return connectionHandled(nil)
	case "ctrl+s":
		return connectionHandled(m.save())
	case "ctrl+a":
		m.toggleAdvancedFields()
		return connectionHandled(nil)
	case "ctrl+t":
		if m.screen == "provider" {
			return connectionHandled(m.test())
		}
		return connectionHandled(nil)
	case "tab", "down", "enter", "shift+tab", "up":
		delta := 1
		if msg.String() == "shift+tab" || msg.String() == "up" {
			delta = -1
		}
		return connectionHandled(m.focusVisibleField(m.fieldIndices(), delta))
	}
	return connectionAction{}
}

func (m *providerDialogCmp) handleProviderListKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "d", "delete":
		return m.startDelete()
	case "up", "down":
		if len(m.rows) > 0 {
			delta := 1
			if msg.String() == "up" {
				delta = -1
			}
			m.selected = (m.selected + delta + len(m.rows)) % len(m.rows)
		}
	case "enter":
		if len(m.rows) > 0 {
			return m.edit(&m.rows[m.selected])
		}
		return m.edit(nil)
	case "r":
		if m.resumeDraft != nil {
			m.resumeDraft()
			return textinput.Blink
		}
	case "n":
		m.screen, m.selected = "presets", 0
		m.note = config.Text("↑↓ escolhe · Enter configura · Esc volta", "↑↓ choose · Enter configure · Esc back")
	case "p":
		return m.settings()
	case "i":
		m.Close()
		return util.CmdHandler(OpenIntegrationDialogMsg{})
	}
	return nil
}

func (m *providerDialogCmp) leaveProviderScreen() tea.Cmd {
	if m.testing {
		m.Close()
		m.note = "Connection test cancelled; your edits are preserved."
		return nil
	}
	if m.screen == "list" {
		m.Close()
		return util.CmdHandler(CloseProviderDialogMsg{})
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

func (m *providerDialogCmp) focusVisibleField(indices []int, delta int) tea.Cmd {
	m.fields[m.focus].Blur()
	index := slices.Index(indices, m.focus)
	m.focus = indices[(index+delta+len(indices))%len(indices)]
	return m.fields[m.focus].Focus()
}

func (m *providerDialogCmp) toggleAdvancedFields() {
	if m.screen != "provider" {
		return
	}
	m.advanced = !m.advanced
	if !slices.Contains(m.fieldIndices(), m.focus) {
		m.fields[m.focus].Blur()
		m.focus = m.fieldIndices()[0]
		m.fields[m.focus].Focus()
	}
}

func (m *providerDialogCmp) invalidateCatalogTest() {
	if m.testing {
		m.Close()
		m.note = "Edited; test again to validate these settings."
	}
}

func (m *providerDialogCmp) updateProviderInput(msg tea.Msg) tea.Cmd {
	if m.screen == "list" || len(m.fields) == 0 {
		return nil
	}
	var cmd tea.Cmd
	m.fields[m.focus], cmd = m.fields[m.focus].Update(msg)
	if _, ok := msg.(tea.KeyMsg); ok {
		m.invalidateCatalogTest()
	}
	return cmd
}

func (m *providerDialogCmp) handleProviderMouse(msg tea.MouseMsg) connectionAction {
	if action := m.providerFooterClick(msg); action.handled {
		return action
	}
	if m.screen == "list" && len(m.rows) > 0 {
		if msg.Button == tea.MouseButtonWheelUp {
			m.selected = max(0, m.selected-1)
		}
		if msg.Button == tea.MouseButtonWheelDown {
			m.selected = min(len(m.rows)-1, m.selected+1)
		}
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft || msg.X < 3 || msg.X >= m.contentWidth()+3 {
		return connectionAction{}
	}
	if m.screen == "list" {
		return m.providerRowClick(msg)
	}
	indices := m.fieldIndices()
	count := m.formRows()
	start := max(0, slices.Index(indices, m.focus)-count+1)
	i := start + (msg.Y-3)/2
	if msg.Y < 3 || msg.Y >= 3+count*2 || i >= len(indices) {
		return connectionAction{}
	}
	m.fields[m.focus].Blur()
	m.focus = indices[i]
	return connectionHandled(m.fields[m.focus].Focus())
}

func (m *providerDialogCmp) providerRowClick(msg tea.MouseMsg) connectionAction {
	rows := m.listRows()
	start := max(0, m.selected-rows+1)
	i := msg.Y - 3 + start
	if msg.Y < 3 || msg.Y >= 3+rows || i >= len(m.rows) {
		return connectionAction{}
	}
	m.selected = i
	return connectionHandled(m.edit(&m.rows[i]))
}

func (m *providerDialogCmp) providerFooterClick(msg tea.MouseMsg) connectionAction {
	if m.screen == "list" {
		if clickedLabel(m.View(), config.Text("[Excluir]", "[Delete]"), msg) {
			return connectionHandled(m.startDelete())
		}
		if clickedLabel(m.View(), config.Text("[Preferências]", "[Preferences]"), msg) {
			return connectionHandled(m.settings())
		}
		if clickedLabel(m.View(), config.Text("[Integrações]", "[Integrations]"), msg) {
			return connectionHandled(util.CmdHandler(OpenIntegrationDialogMsg{}))
		}
	}
	if m.screen == "provider" {
		for _, action := range []struct {
			label string
			key   tea.KeyType
		}{{config.Text("[Salvar]", "[Save]"), tea.KeyCtrlS}, {config.Text("[Catálogo]", "[Catalog]"), tea.KeyCtrlT}, {config.Text("[Mais]", "[More]"), tea.KeyCtrlA}} {
			if clickedLabel(m.View(), action.label, msg) {
				_, cmd := m.Update(tea.KeyMsg{Type: action.key})
				return connectionHandled(cmd)
			}
		}
	}
	return connectionAction{}
}
