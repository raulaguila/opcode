package tui

import (
	"context"
	"fmt"
	"github.com/raulaguila/opcode/internal/clipboard"
	"io"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/models"
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
	case util.CopyTextMsg:
		return productivityMessageAppModelCopyTextMsg(a, &msg)
	case page.TerminalClipboardMsg:
		return productivityMessageAppModelTerminalClipboardMsg(a, &msg)
	case dialog.ReasoningSelectedMsg:
		return productivityMessageCaseDialogReasoningSelectedMsg(a, &msg)

	case dialog.OpenProviderDialogMsg:
		return productivityMessageAppModelOpenProviderDialogMsg(a, &msg)
	case dialog.CloseProviderDialogMsg:
		a.providerDialog.Close()
		a.showProviderDialog = false
		if a.providerReturnToModels {
			a.providerReturnToModels = false
			a.showModelDialog = true
			return a.openModelCatalog("", true), true
		}
		return nil, true
	case dialog.UserPreferencesChangedMsg:
		if p, ok := a.pages[page.ChatPage]; ok {
			updated, cmd := p.Update(chat.ProcessPreferencesChangedMsg{})
			a.pages[page.ChatPage] = updated
			return cmd, true
		}
		return nil, true
	case dialog.ProviderSettingsSavedMsg:
		return productivityMessageAppModelProviderSettingsSavedMsg(a, &msg)
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
			return util.ReportInfo(config.Text("MCP salvo. As próximas chamadas usam a configuração atualizada.", "MCP saved. Subsequent calls use the updated configuration.")), true
		}
		return util.ReportInfo(config.Text("Servidor LSP salvo. Reinicie para carregar o cadastro atualizado.", "LSP server saved. Restart to load the updated configuration.")), true
	case page.OpenModelsMsg:
		a.closeSettings()
		a.showInitDialog = false
		a.showModelDialog = true
		a.showCommandDialog = false
		return a.openModelCatalog("", false), true
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
	case dialog.SessionPreviewRequestMsg:
		return productivityMessageCaseDialogSessionPreviewRequestMsg(a, &msg)
	case dialog.SessionActionMsg:
		return a.sessionAction(msg), true
	}
	return nil, false
}

// The dialog receives only an in-memory snapshot. View never performs model
// discovery, usage estimation, credential resolution or a network request.
func (a *appModel) openModelCatalog(destination models.ModelProvider, resume bool) tea.Cmd {
	openModelCatalogModelDialogInterface(a)
	if resume {
		if d, ok := a.modelDialog.(interface{ Resume() tea.Cmd }); ok {
			return d.Resume()
		}
	}
	if destination != "" {
		if d, ok := a.modelDialog.(interface {
			OpenForProvider(models.ModelProvider) tea.Cmd
		}); ok {
			return d.OpenForProvider(destination)
		}
	}
	return a.modelDialog.Open()
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
	a.providerReturnToModels = false
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
	return a.sessionDialog.Init()
}
func (a *appModel) sessionAction(msg dialog.SessionActionMsg) tea.Cmd {
	cfg := config.Get()
	catalog := session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory)
	if a.app.CoderAgent != nil && a.app.CoderAgent.IsSessionBusy(msg.Session.ID) {
		return util.ReportWarn(config.Text("Aguarde ou cancele esta conversa antes de gerenciá-la", "Wait or cancel this conversation before managing it"))
	}
	switch msg.Action {
	case "rename":
		return sessionActionCaseRename(a, &msg)
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
		return sessionActionAppModelExport(a, &msg, cfg)
	}
	return util.ReportError(fmt.Errorf("%s", config.Text("ação de conversa desconhecida", "unknown conversation action")))
}
func (a appModel) dialogOrigin(overlay string) (int, int) {
	base := a.baseView()
	w, h := lipgloss.Width(base), lipgloss.Height(base)
	ow, oh := lipgloss.Width(overlay), lipgloss.Height(overlay)
	return max(0, min(w/2-ow/2, w-ow-1)), max(0, min(h/2-oh/2, h-oh-1))
}

func productivityMessageAppModelProviderSettingsSavedMsg(a *appModel, msg *dialog.ProviderSettingsSavedMsg) (extractedResult0 tea.Cmd, extractedResult1 bool) {
	a.providerDialog.Close()
	a.showProviderDialog = false
	a.providerReturnToModels = false
	if msg.Deleted {
		a.showProviderDialog = true
		return tea.Batch(a.providerDialog.Open(), util.ReportInfo(config.Text("Conexão excluída", "Connection deleted"))), true
	}
	if msg.ProviderChanged && msg.Disabled {
		a.showProviderDialog = true
		return tea.Batch(a.providerDialog.Open(), util.ReportInfo(config.Text("Conexão salva como desativada. Ative-a para escolher um modelo.", "Connection saved as disabled. Enable it to choose a model."))), true
	}
	if msg.ProviderChanged {
		var err error
		if a.app.CoderAgent != nil && a.app.CoderAgent.Model().ID != "" {
			err = a.app.ConfigureCoderAgent()
		}
		a.showModelDialog = true
		if d, ok := a.modelDialog.(interface{ SetSetup(bool) }); ok {
			d.SetSetup(a.setupActive)
		}
		return tea.Batch(a.openModelCatalog(msg.Provider, false), func() tea.Msg {
			if err != nil {
				return util.InfoMsg{Type: util.InfoTypeWarn, Msg: tools.RedactCredentials(err.Error())}
			}
			return nil
		}), true
	}
	if p, ok := a.pages[page.ChatPage]; ok {
		updated, cmd := p.Update(chat.ProcessPreferencesChangedMsg{})
		a.pages[page.ChatPage] = updated
		return tea.Batch(cmd, util.ReportInfo(config.Text("Preferências salvas", "Preferences saved"))), true
	}
	return util.ReportInfo(config.Text("Preferências salvas", "Preferences saved")), true

}

func productivityMessageAppModelOpenProviderDialogMsg(a *appModel, msg *dialog.OpenProviderDialogMsg) (extractedResult0 tea.Cmd, extractedResult1 bool) {
	a.closeSettings()
	a.modelDialog.Close()
	a.showModelDialog = false
	a.showInitDialog = false
	a.showCommandDialog = false
	a.showProviderDialog = true
	cmd := a.providerDialog.Open()
	a.providerReturnToModels = msg.ReturnToModels
	if msg.Provider != "" {
		if d, ok := a.providerDialog.(interface {
			OpenForProvider(models.ModelProvider) tea.Cmd
		}); ok {
			cmd = d.OpenForProvider(msg.Provider)
		}
	}
	a.setupActive = msg.Setup
	if msg.Setup {
		if d, ok := a.providerDialog.(interface{ Setup() }); ok {
			d.Setup()
		}
	}
	return cmd, true

}

func productivityMessageAppModelCopyTextMsg(a *appModel, msg *util.CopyTextMsg) (extractedResult0 tea.Cmd, extractedResult1 bool) {
	writer := a.terminalOutput
	return func() tea.Msg {
		confirmed, err := clipboard.Copy(context.Background(), msg.Text, writer)
		if msg.Result != nil {
			return msg.Result(confirmed, err)
		}
		if err != nil {
			return util.InfoMsg{Type: util.InfoTypeError, Msg: err.Error()}
		}
		if confirmed {
			return util.InfoMsg{Msg: config.Text("Seleção copiada.", "Selection copied.")}
		}
		return util.InfoMsg{Msg: config.Text("Cópia enviada ao terminal; recebimento não confirmado. Shift+arrastar permite seleção nativa.", "Copy sent to terminal; receipt unconfirmed. Shift+drag allows native selection.")}
	}, true

}

func sessionActionAppModelExport(a *appModel, msg *dialog.SessionActionMsg, cfg *config.Config) (extractedResult0 tea.Cmd) {
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

func productivityMessageAppModelTerminalClipboardMsg(a *appModel, msg *page.TerminalClipboardMsg) (extractedResult0 tea.Cmd, extractedResult1 bool) {
	writer := a.terminalOutput
	return func() tea.Msg {
		if !clipboard.ValidOSC52(msg.Sequence) {
			return msg.Result(fmt.Errorf("invalid clipboard request"))
		}
		if writer == nil {
			return msg.Result(fmt.Errorf("terminal output unavailable; use text selection"))
		}
		_, err := io.WriteString(writer, msg.Sequence)
		if err != nil {
			return msg.Result(fmt.Errorf("terminal copy failed; use text selection"))
		}
		return msg.Result(nil)
	}, true

}

func productivityMessageCaseDialogReasoningSelectedMsg(a *appModel, msg *dialog.ReasoningSelectedMsg) (extractedResult0 tea.Cmd, extractedResult1 bool) {
	control, ok := a.app.CoderAgent.(interface {
		SetReasoning(config.AgentName, models.ModelID, string) error
	})
	if !ok {
		return util.ReportError(fmt.Errorf("reasoning control unavailable")), true
	}
	if err := control.SetReasoning(msg.Agent, msg.Model, msg.Effort); err != nil {
		return util.ReportError(err), true
	}
	a.modelDialog.Close()
	a.showModelDialog = false
	return util.ReportInfo(config.Text("Esforço salvo para as próximas chamadas", "Reasoning effort saved for subsequent calls")), true

}

func productivityMessageCaseDialogSessionPreviewRequestMsg(a *appModel, msg *dialog.SessionPreviewRequestMsg) (extractedResult0 tea.Cmd, extractedResult1 bool) {
	if !a.showSessionDialog || a.app.Messages == nil {
		return nil, true
	}
	return a.sessionPreview(msg.ID), true

}

func openModelCatalogModelDialogInterface(a *appModel) {
	if snapshot, ok := a.modelDialog.(interface {
		SetActiveContext(models.ModelID, int64, string)
	}); ok {
		var id models.ModelID
		var window int64
		var source string
		if a.app != nil && a.app.CoderAgent != nil {
			if control, ok := a.app.CoderAgent.(agent.UsageControl); ok {
				if usage, err := control.Usage(context.Background(), a.selectedSession.ID); err == nil {
					id, window, source = usage.ActiveModel, usage.ActiveContextWindow, usage.ActiveContextSource
				}
			} else {
				model := a.app.CoderAgent.Model()
				id, window, source = model.ID, model.ContextWindow, model.ContextSource
			}
		}
		snapshot.SetActiveContext(id, window, source)
	}
}

func sessionActionCaseRename(a *appModel, msg *dialog.SessionActionMsg) (extractedResult0 tea.Cmd) {
	target := msg.Session
	target.Title = strings.TrimSpace(msg.Title)
	if target.Title == "" || len(target.Title) > 200 {
		return util.ReportWarn(config.Text("Use um título entre 1 e 200 caracteres", "Use a title between 1 and 200 characters"))
	}
	saved, err := a.app.Sessions.Save(context.Background(), target)
	if err != nil {
		return util.ReportError(err)
	}
	if a.selectedSession.ID == saved.ID {
		a.selectedSession = saved
	}
	return tea.Batch(a.openSessions(), util.ReportInfo("Conversa renomeada"))

}
