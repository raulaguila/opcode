package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/localstate"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/transcript"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/util"
)

func (a *appModel) productivityMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case dialog.OpenProviderDialogMsg:
		a.closeSettings()
		a.modelDialog.Close()
		a.showModelDialog = false
		a.showInitDialog = false
		a.showCommandDialog = false
		a.showProviderDialog = true
		cmd := a.providerDialog.Open()
		a.setupActive = msg.Setup
		if msg.Setup {
			if d, ok := a.providerDialog.(interface{ Setup() }); ok {
				d.Setup()
			}
		}
		return cmd, true
	case dialog.CloseProviderDialogMsg:
		a.providerDialog.Close()
		a.showProviderDialog = false
		return nil, true
	case dialog.UserPreferencesChangedMsg:
		if p, ok := a.pages[page.ChatPage]; ok {
			updated, cmd := p.Update(chat.ProcessPreferencesChangedMsg{})
			a.pages[page.ChatPage] = updated
			return cmd, true
		}
		return nil, true
	case dialog.ProviderSettingsSavedMsg:
		a.providerDialog.Close()
		a.showProviderDialog = false
		if msg.ProviderChanged {
			var err error
			if a.app.CoderAgent != nil && a.app.CoderAgent.Model().ID != "" {
				err = a.app.ConfigureCoderAgent()
			}
			a.showModelDialog = true
			if d, ok := a.modelDialog.(interface{ SetSetup(bool) }); ok {
				d.SetSetup(a.setupActive)
			}
			return tea.Batch(a.modelDialog.Open(), func() tea.Msg {
				if err != nil {
					return util.InfoMsg{Type: util.InfoTypeWarn, Msg: tools.RedactCredentials(err.Error())}
				}
				return nil
			}), true
		}
		if p, ok := a.pages[page.ChatPage]; ok {
			updated, cmd := p.Update(chat.ProcessPreferencesChangedMsg{})
			a.pages[page.ChatPage] = updated
			return tea.Batch(cmd, util.ReportInfo("Preferências salvas")), true
		}
		return util.ReportInfo("Preferências salvas"), true
	case dialog.OpenIntegrationDialogMsg:
		a.closeSettings()
		a.modelDialog.Close()
		a.showModelDialog = false
		a.showInitDialog = false
		a.showCommandDialog = false
		a.showIntegrationDialog = true
		return a.integrationDialog.Open(), true
	case dialog.CloseIntegrationDialogMsg:
		a.integrationDialog.Close()
		a.showIntegrationDialog = false
		return nil, true
	case dialog.IntegrationSettingsSavedMsg:
		if msg.Kind == "mcp" {
			return util.ReportInfo("MCP salvo. As próximas chamadas usam a configuração atualizada."), true
		}
		return util.ReportInfo("Servidor LSP salvo. Reinicie para carregar o cadastro atualizado."), true
	case page.OpenModelsMsg:
		a.closeSettings()
		a.showInitDialog = false
		a.showModelDialog = true
		a.showCommandDialog = false
		return a.modelDialog.Open(), true
	case page.OpenSessionsMsg:
		a.closeSettings()
		a.showCommandDialog = false
		return a.openSessions(), true
	case page.OpenFilesMsg:
		a.showFilepicker = true
		a.filepicker.ToggleFilepicker(true)
		return nil, true
	case page.OpenCommandsMsg:
		a.commandDialog.SetCommands(a.contextualCommands())
		a.showCommandDialog = true
		return nil, true
	case page.OpenModeChooserMsg:
		return a.openModeChooser(), true
	case dialog.SessionActionMsg:
		return a.sessionAction(msg), true
	}
	return nil, false
}
func (a *appModel) closeSettings() {
	if a.providerDialog != nil {
		a.providerDialog.Close()
	}
	if a.integrationDialog != nil {
		a.integrationDialog.Close()
	}
	a.showProviderDialog = false
	a.showIntegrationDialog = false
}
func (a *appModel) openSessions() tea.Cmd {
	items, err := a.app.Sessions.List(context.Background())
	if err != nil {
		return util.ReportError(err)
	}
	cfg := config.Get()
	metadata, _, err := session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory).Snapshot()
	if err != nil {
		return util.ReportError(err)
	}
	archived := map[string]bool{}
	for id, state := range metadata {
		archived[id] = state.Archived
	}
	a.sessionDialog.SetArchived(archived)
	a.sessionDialog.SetSessions(items)
	a.sessionDialog.SetSelectedSession(a.selectedSession.ID)
	a.showSessionDialog = true
	return nil
}
func (a *appModel) sessionAction(msg dialog.SessionActionMsg) tea.Cmd {
	cfg := config.Get()
	catalog := session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory)
	if a.app.CoderAgent != nil && a.app.CoderAgent.IsSessionBusy(msg.Session.ID) {
		return util.ReportWarn("Aguarde ou cancele esta conversa antes de gerenciá-la")
	}
	switch msg.Action {
	case "rename":
		target := msg.Session
		target.Title = strings.TrimSpace(msg.Title)
		if target.Title == "" || len(target.Title) > 200 {
			return util.ReportWarn("Use um título entre 1 e 200 caracteres")
		}
		saved, err := a.app.Sessions.Save(context.Background(), target)
		if err != nil {
			return util.ReportError(err)
		}
		if a.selectedSession.ID == saved.ID {
			a.selectedSession = saved
		}
		return tea.Batch(a.openSessions(), util.ReportInfo("Conversa renomeada"))
	case "archive":
		snapshot, _, err := catalog.Snapshot()
		if err != nil {
			return util.ReportError(err)
		}
		if err = catalog.Archive(msg.Session.ID, !snapshot[msg.Session.ID].Archived); err != nil {
			return util.ReportError(err)
		}
		return tea.Batch(a.openSessions(), util.ReportInfo("Estado de arquivamento atualizado"))
	case "export":
		messages, err := a.app.Messages.List(context.Background(), msg.Session.ID)
		if err != nil {
			return util.ReportError(err)
		}
		data, err := transcript.Markdown(msg.Session, messages, tools.RedactCredentials)
		if err != nil {
			return util.ReportError(err)
		}
		safe := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, msg.Session.ID)
		dir := cfg.Data.Directory
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cfg.WorkingDir, dir)
		}
		path := filepath.Join(dir, "exports", safe+"-"+time.Now().Format("20060102-150405.000000000")+".md")
		if err = localstate.WriteBytes(path, data); err != nil {
			return util.ReportError(err)
		}
		return util.ReportInfo("Conversa exportada: " + path)
	}
	return util.ReportError(fmt.Errorf("ação de conversa desconhecida"))
}
func (a appModel) dialogOrigin(overlay string) (int, int) {
	base := lipgloss.JoinVertical(lipgloss.Top, a.pages[a.currentPage].View(), a.statusView())
	w, h := lipgloss.Width(base), lipgloss.Height(base)
	ow, oh := lipgloss.Width(overlay), lipgloss.Height(overlay)
	return max(0, min(w/2-ow/2, w-ow-1)), max(0, min(h/2-oh/2, h-oh-1))
}
