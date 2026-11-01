package ui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/util"
)

// hosted floats a settings dialog of the earlier interface. Connections,
// preferences and integrations write configuration and credentials; their
// editors are reused rather than rewritten, framed with the same palette.
type hosted struct {
	cmp  util.Model
	x, y int // where the overlay placed it, for mouse translation
}

func (h *hosted) view(*Model) string { return h.cmp.View() }

func (h *hosted) key(_ *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return h.forward(msg), true
}

func (h *hosted) paste(text string) { h.forward(tea.PasteMsg{Content: text}) }

func (h *hosted) forward(msg tea.Msg) tea.Cmd {
	updated, cmd := h.cmp.Update(msg)
	h.cmp = updated
	return cmd
}

func (m *Model) saveGuard() error {
	if m.app != nil && m.app.CoderAgent != nil && m.app.CoderAgent.IsBusy() {
		return errors.New(textFor("aguarde ou cancele a execução antes de alterar configurações", "wait for or cancel the run before changing settings"))
	}
	return nil
}

func (m *Model) openSettings() tea.Cmd {
	providers := dialog.NewProviderDialogCmp(m.saveGuard)
	h := &hosted{cmp: providers}
	m.dialog = h
	return tea.Batch(h.forward(tea.WindowSizeMsg{Width: m.width, Height: m.height}), providers.Open())
}

func (m *Model) openIntegrations() tea.Cmd {
	integrations := dialog.NewIntegrationDialogCmp(m.app.ProbeIntegration, m.saveGuard)
	h := &hosted{cmp: integrations}
	m.dialog = h
	return tea.Batch(h.forward(tea.WindowSizeMsg{Width: m.width, Height: m.height}), integrations.Open())
}

// settingsMessage handles what the hosted dialogs report; ok is false for
// messages meant for the rest of the screen.
func (m *Model) settingsMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case dialog.CloseProviderDialogMsg, dialog.CloseIntegrationDialogMsg:
		m.dialog = nil
		return nil, true
	case dialog.OpenProviderDialogMsg:
		return m.openSettings(), true
	case dialog.OpenIntegrationDialogMsg:
		return m.openIntegrations(), true
	case dialog.IntegrationSettingsSavedMsg:
		m.info = textFor("Integração salva", "Integration saved")
		return nil, true
	case dialog.UserPreferencesChangedMsg:
		m.refresh()
		return nil, true
	case dialog.ProviderSettingsSavedMsg:
		switch {
		case msg.Deleted:
			m.info = textFor("Conexão excluída", "Connection deleted")
			return m.openSettings(), true
		case msg.ProviderChanged && msg.Disabled:
			m.info = textFor("Conexão salva como desativada", "Connection saved as disabled")
			return m.openSettings(), true
		case msg.ProviderChanged:
			// A changed connection reloads the agent, then offers its models.
			if m.app.CoderAgent != nil && m.app.CoderAgent.Model().ID != "" {
				if err := m.app.ConfigureCoderAgent(); err != nil {
					m.notice = tools.RedactCredentials(err.Error())
				}
			}
			return m.openModels(), true
		}
		m.dialog = nil
		m.info = textFor("Preferências salvas", "Preferences saved")
		return nil, true
	case util.InfoMsg:
		if msg.Type == util.InfoTypeError || msg.Type == util.InfoTypeWarn {
			m.notice = strings.Join(strings.Fields(msg.Msg), " ")
		} else {
			m.info = msg.Msg
		}
		return nil, true
	}
	return nil, false
}

// placeHosted remembers where a hosted dialog was drawn.
func (m *Model) placeHosted(x, y int) {
	if h, ok := m.dialog.(*hosted); ok {
		h.x, h.y = x, y
	}
}

// hostedMouse forwards a mouse event in the dialog's own coordinates.
func (m *Model) hostedMouse(msg tea.MouseMsg) tea.Cmd {
	h, ok := m.dialog.(*hosted)
	if !ok {
		return nil
	}
	return h.forward(util.TranslateMouse(msg, -h.x, -h.y))
}
