package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/commands"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/components/core"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/page"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type keyMap struct {
	Logs          key.Binding
	Quit          key.Binding
	Help          key.Binding
	SwitchSession key.Binding
	Commands      key.Binding
	Filepicker    key.Binding
	Models        key.Binding
	SwitchTheme   key.Binding
}

type startCompactSessionMsg struct{}

const (
	quitKey = "q"
)

var keys = keyMap{
	Logs: key.NewBinding(
		key.WithKeys("ctrl+l"),
		key.WithHelp("ctrl+l", "logs"),
	),

	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("ctrl+_", "ctrl+h"),
		key.WithHelp("ctrl+?", "toggle help"),
	),

	SwitchSession: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "switch session"),
	),

	Commands: key.NewBinding(
		key.WithKeys("ctrl+k"),
		key.WithHelp("ctrl+k", "commands"),
	),
	Filepicker: key.NewBinding(
		key.WithKeys("ctrl+f"),
		key.WithHelp("ctrl+f", "select files to upload"),
	),
	Models: key.NewBinding(
		key.WithKeys("ctrl+o"),
		key.WithHelp("ctrl+o", "model selection"),
	),

	SwitchTheme: key.NewBinding(
		key.WithKeys("ctrl+t"),
		key.WithHelp("ctrl+t", "switch theme"),
	),
}

var helpEsc = key.NewBinding(
	key.WithKeys("?"),
	key.WithHelp("?", "close help"),
)

var returnKey = key.NewBinding(
	key.WithKeys("esc"),
	key.WithHelp("esc", "close"),
)

var logsKeyReturnKey = key.NewBinding(
	key.WithKeys("esc", "backspace", quitKey),
	key.WithHelp("esc/q", "go back"),
)

type appModel struct {
	width, height   int
	currentPage     page.PageID
	previousPage    page.PageID
	pages           map[page.PageID]tea.Model
	loadedPages     map[page.PageID]bool
	status          core.StatusCmp
	app             *app.App
	selectedSession session.Session

	showPermissions bool
	permissions     dialog.PermissionDialogCmp

	showHelp bool
	help     dialog.HelpCmp

	showQuit bool
	quit     dialog.QuitDialog

	showSessionDialog bool
	sessionDialog     dialog.SessionDialog

	showCommandDialog bool
	commandDialog     dialog.CommandDialog
	commands          []dialog.Command

	showModelDialog       bool
	modelDialog           dialog.ModelDialog
	showProviderDialog    bool
	providerDialog        dialog.ProviderDialog
	showIntegrationDialog bool
	integrationDialog     dialog.IntegrationDialog

	showInitDialog bool
	initDialog     dialog.InitDialogCmp

	showFilepicker bool
	filepicker     dialog.FilepickerCmp

	showThemeDialog bool
	themeDialog     dialog.ThemeDialog

	showMultiArgumentsDialog bool
	multiArgumentsDialog     dialog.MultiArgumentsDialogCmp

	isCompacting      bool
	compactingMessage string
}

func (a appModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmd := a.pages[a.currentPage].Init()
	a.loadedPages[a.currentPage] = true
	cmds = append(cmds, cmd)
	cmd = a.status.Init()
	cmds = append(cmds, cmd)
	cmd = a.quit.Init()
	cmds = append(cmds, cmd)
	cmd = a.help.Init()
	cmds = append(cmds, cmd)
	cmd = a.sessionDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.commandDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.modelDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.initDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.filepicker.Init()
	cmds = append(cmds, cmd)
	cmd = a.themeDialog.Init()
	cmds = append(cmds, cmd)

	// Check if we should show the init dialog
	cmds = append(cmds, func() tea.Msg {
		if a.app.CoderAgent != nil && a.app.CoderAgent.Model().ID == "" {
			return dialog.OpenProviderDialogMsg{}
		}
		shouldShow, err := config.ShouldShowInitDialog()
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  "Failed to check init status: " + err.Error(),
			}
		}
		return dialog.ShowInitDialogMsg{Show: shouldShow}
	})

	if a.selectedSession.ID != "" {
		cmds = append(cmds, util.CmdHandler(chat.SessionSelectedMsg(a.selectedSession)))
	}
	return tea.Batch(cmds...)
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, handled := a.productivityMessage(msg); handled {
		return a, cmd
	}
	var cmds []tea.Cmd
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case dialog.ModelCatalogLoadedMsg:
		d, cmd := a.modelDialog.Update(msg)
		a.modelDialog = d.(dialog.ModelDialog)
		return a, cmd
	case page.TerminalTickMsg, page.TerminalStartedMsg, page.WorkflowCommandMsg, page.WorkspaceCommandMsg, page.WorkspaceResultMsg, chat.ResponseActionsMsg, chat.SubmissionResultMsg:
		if chatPage, ok := a.pages[page.ChatPage]; ok {
			a.pages[page.ChatPage], cmd = chatPage.Update(msg)
			return a, cmd
		}
		return a, nil
	case tea.MouseMsg:
		// Capturing the wheel must not scroll the conversation underneath a
		// modal dialog. The model picker handles its own mouse navigation.
		if a.showPermissions || a.showHelp || a.showQuit ||
			a.showCommandDialog || a.showInitDialog ||
			a.showFilepicker || a.showThemeDialog || a.showMultiArgumentsDialog || a.isCompacting {
			return a, nil
		}
		if a.showSessionDialog {
			if a.sessionDialog == nil {
				return a, nil
			}
			col, row := a.dialogOrigin(a.sessionDialog.View())
			msg.X -= col
			msg.Y -= row
			updated, c := a.sessionDialog.Update(msg)
			a.sessionDialog = updated.(dialog.SessionDialog)
			return a, c
		}
		if a.showProviderDialog {
			col, row := a.dialogOrigin(a.providerDialog.View())
			msg.X -= col
			msg.Y -= row
			d, cmd := a.providerDialog.Update(msg)
			a.providerDialog = d.(dialog.ProviderDialog)
			return a, cmd
		}
		if a.showIntegrationDialog {
			col, row := a.dialogOrigin(a.integrationDialog.View())
			msg.X -= col
			msg.Y -= row
			d, cmd := a.integrationDialog.Update(msg)
			a.integrationDialog = d.(dialog.IntegrationDialog)
			return a, cmd
		}
		if a.showModelDialog {
			if a.modelDialog == nil {
				return a, nil
			}
			col, row := a.modelDialogOrigin()
			msg.X -= col
			msg.Y -= row
			d, cmd := a.modelDialog.Update(msg)
			a.modelDialog = d.(dialog.ModelDialog)
			return a, cmd
		}
		if a.currentPage == page.ChatPage && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft &&
			msg.Y == lipgloss.Height(a.pages[a.currentPage].View()) &&
			msg.X >= lipgloss.Width(a.status.View())-core.ModelButtonWidth() && msg.X < lipgloss.Width(a.status.View()) {
			a.showModelDialog = true
			return a, a.modelDialog.Open()
		}
	case tea.WindowSizeMsg:
		msg.Height -= 1 // Make space for the status bar
		a.width, a.height = msg.Width, msg.Height
		if a.providerDialog != nil {
			a.providerDialog.Update(msg)
		}
		if a.integrationDialog != nil {
			a.integrationDialog.Update(msg)
		}
		a.modelDialog.Update(msg)

		s, _ := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
		cmds = append(cmds, cmd)

		prm, permCmd := a.permissions.Update(msg)
		a.permissions = prm.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permCmd)

		help, helpCmd := a.help.Update(msg)
		a.help = help.(dialog.HelpCmp)
		cmds = append(cmds, helpCmd)

		session, sessionCmd := a.sessionDialog.Update(msg)
		a.sessionDialog = session.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)

		command, commandCmd := a.commandDialog.Update(msg)
		a.commandDialog = command.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)

		filepicker, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = filepicker.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

		a.initDialog.SetSize(msg.Width, msg.Height)

		if a.showMultiArgumentsDialog {
			a.multiArgumentsDialog.SetSize(msg.Width, msg.Height)
			args, argsCmd := a.multiArgumentsDialog.Update(msg)
			a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
			cmds = append(cmds, argsCmd, a.multiArgumentsDialog.Init())
		}

		return a, tea.Batch(cmds...)
	// Status
	case util.InfoMsg:
		s, cmd := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		cmds = append(cmds, cmd)
		return a, tea.Batch(cmds...)
	case pubsub.Event[logging.LogMessage]:
		if msg.Payload.Persist {
			switch msg.Payload.Level {
			case "error":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeError,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			case "info":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)

			case "warn":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeWarn,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})

				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			default:
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			}
		}
	case util.ClearStatusMsg:
		s, _ := a.status.Update(msg)
		a.status = s.(core.StatusCmp)

	// Permission
	case pubsub.Event[permission.PermissionRequest]:
		a.showPermissions = true
		return a, a.permissions.SetPermissions(msg.Payload)
	case dialog.PermissionResponseMsg:
		var cmd tea.Cmd
		switch msg.Action {
		case dialog.PermissionAllow:
			a.app.Permissions.Grant(msg.Permission)
		case dialog.PermissionAllowForSession:
			a.app.Permissions.GrantPersistant(msg.Permission)
		case dialog.PermissionDeny:
			a.app.Permissions.Deny(msg.Permission)
		}
		a.showPermissions = false
		return a, cmd

	case page.PageChangeMsg:
		return a, a.moveToPage(msg.ID)

	case dialog.ConfirmQuitMsg:
		if current, ok := a.pages[page.ChatPage].(interface{ PersistDraft() error }); ok {
			if err := current.PersistDraft(); err != nil {
				return a, util.ReportError(err)
			}
		}
		return a, tea.Quit
	case dialog.CloseQuitMsg:
		a.showQuit = false
		return a, nil

	case dialog.CloseSessionDialogMsg:
		a.showSessionDialog = false
		return a, nil

	case dialog.CloseCommandDialogMsg:
		a.showCommandDialog = false
		return a, nil

	case startCompactSessionMsg:
		if chatPage, ok := a.pages[page.ChatPage]; ok {
			a.pages[page.ChatPage], cmd = chatPage.Update(page.WorkspaceCommandMsg{Command: "compact"})
			return a, cmd
		}
		return a, nil

	case pubsub.Event[agent.AgentEvent]:
		payload := msg.Payload
		if payload.Type == agent.AgentEventTypeInstructions {
			if payload.SessionID == a.selectedSession.ID {
				return a, util.ReportInfo(payload.Progress)
			}
			return a, nil
		}
		if payload.Error != nil {
			a.isCompacting = false
			id := payload.SessionID
			if id == "" {
				id = a.selectedSession.ID
			}
			p, pcmd := a.pages[page.ChatPage].Update(page.AgentOutcomeMsg{SessionID: id, Error: payload.Error, Done: payload.Done})
			a.pages[page.ChatPage] = p
			return a, tea.Batch(pcmd, util.ReportError(payload.Error))
		}
		if payload.Done {
			p, pcmd := a.pages[page.ChatPage].Update(page.AgentOutcomeMsg{SessionID: agentEventSession(payload, a.selectedSession.ID), Done: true})
			a.pages[page.ChatPage] = p
			cmds = append(cmds, pcmd)
		}

		a.compactingMessage = payload.Progress

		if payload.Done && payload.Type == agent.AgentEventTypeSummarize {
			a.isCompacting = false
			return a, util.ReportInfo("Session summarization complete")
		} else if payload.Done && payload.Type == agent.AgentEventTypeResponse && a.selectedSession.ID != "" {
			model := a.app.CoderAgent.Model()
			contextWindow := model.ContextWindow
			tokens := a.selectedSession.CompletionTokens + a.selectedSession.PromptTokens
			if (tokens >= int64(float64(contextWindow)*0.95)) && config.Get().AutoCompact {
				return a, util.CmdHandler(startCompactSessionMsg{})
			}
		}
		// Continue listening for events
		return a, nil

	case dialog.CloseThemeDialogMsg:
		a.showThemeDialog = false
		return a, nil

	case dialog.ThemeChangedMsg:
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
		a.showThemeDialog = false
		return a, tea.Batch(cmd, util.ReportInfo("Theme changed to: "+msg.ThemeName))

	case dialog.CloseModelDialogMsg:
		a.modelDialog.Close()
		a.showModelDialog = false
		return a, nil

	case dialog.ModelSelectedMsg:
		target := msg.Agent
		if target == "" {
			target = config.AgentCoder
		}
		model, err := a.app.CoderAgent.SelectModel(target, msg.Model)
		if err != nil {
			return a, util.ReportError(err)
		}
		a.modelDialog.Close()
		a.showModelDialog = false

		return a, util.ReportInfo(fmt.Sprintf("Model changed to %s", model.Name))

	case dialog.ShowInitDialogMsg:
		a.showInitDialog = msg.Show && !a.showProviderDialog && !a.showIntegrationDialog && a.app.CoderAgent != nil && a.app.CoderAgent.Model().ID != ""
		return a, nil

	case dialog.CloseInitDialogMsg:
		a.showInitDialog = false
		if msg.Initialize {
			// Run the initialization command
			for _, cmd := range a.commands {
				if cmd.ID == "init" {
					// Mark the project as initialized
					if err := config.MarkProjectInitialized(); err != nil {
						return a, util.ReportError(err)
					}
					return a, cmd.Handler(cmd)
				}
			}
		} else {
			// Mark the project as initialized without running the command
			if err := config.MarkProjectInitialized(); err != nil {
				return a, util.ReportError(err)
			}
		}
		return a, nil

	case chat.SessionClearedMsg:
		if current, ok := a.pages[page.ChatPage].(interface{ PrepareSessionDraft(string) error }); ok {
			if err := current.PrepareSessionDraft(""); err != nil {
				return a, util.ReportError(err)
			}
		}
		if a.selectedSession.ID != "" && a.app.Permissions != nil {
			a.app.Permissions.RevokeSession(a.selectedSession.ID)
		}
		a.selectedSession = session.Session{}
		a.sessionDialog.SetSelectedSession("")
	case chat.SessionSelectedMsg:
		if current, ok := a.pages[page.ChatPage].(interface{ PrepareSessionDraft(string) error }); ok {
			if err := current.PrepareSessionDraft(msg.ID); err != nil {
				return a, util.ReportError(err)
			}
		}
		if a.selectedSession.ID != "" && a.selectedSession.ID != msg.ID && a.app.Permissions != nil {
			a.app.Permissions.RevokeSession(a.selectedSession.ID)
		}
		a.selectedSession = msg
		if cfg := config.Get(); cfg != nil && msg.ID != "" {
			if err := session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory).Activate(msg.ID); err != nil {
				cmds = append(cmds, util.ReportError(err))
			}
		}
		a.sessionDialog.SetSelectedSession(msg.ID)

	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == a.selectedSession.ID {
			a.selectedSession = msg.Payload
		}
	case dialog.SessionSelectedMsg:
		a.showSessionDialog = false
		if a.currentPage == page.ChatPage {
			return a, util.CmdHandler(chat.SessionSelectedMsg(msg.Session))
		}
		return a, nil

	case dialog.CommandSelectedMsg:
		a.showCommandDialog = false
		// Execute the command handler if available
		if msg.Command.Handler != nil {
			return a, msg.Command.Handler(msg.Command)
		}
		return a, util.ReportInfo("Command selected: " + msg.Command.Title)

	case dialog.ShowMultiArgumentsDialogMsg:
		// Show multi-arguments dialog
		a.multiArgumentsDialog = dialog.NewMultiArgumentsDialogCmp(msg.CommandID, msg.Content, msg.ArgNames)
		a.showMultiArgumentsDialog = true
		return a, a.multiArgumentsDialog.Init()

	case dialog.CloseMultiArgumentsDialogMsg:
		// Close multi-arguments dialog
		a.showMultiArgumentsDialog = false

		// If submitted, replace all named arguments and run the command
		if msg.Submit {
			content := msg.Content

			// Replace each named argument with its value
			for name, value := range msg.Args {
				placeholder := "$" + name
				content = strings.ReplaceAll(content, placeholder, value)
			}

			// Execute the command with arguments
			return a, util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: content,
				Args:    msg.Args,
			})
		}
		return a, nil

	case tea.KeyMsg:
		if msg.String() != "ctrl+c" {
			if a.showQuit {
				updated, c := a.quit.Update(msg)
				a.quit = updated.(dialog.QuitDialog)
				return a, c
			}
			if a.showPermissions {
				updated, c := a.permissions.Update(msg)
				a.permissions = updated.(dialog.PermissionDialogCmp)
				return a, c
			}
		}
		if msg.String() != "ctrl+c" && !a.showPermissions && !a.showHelp && !a.showQuit && !a.showSessionDialog && !a.showCommandDialog && !a.showModelDialog && !a.showInitDialog && !a.showFilepicker && !a.showThemeDialog && !a.showMultiArgumentsDialog {
			if a.showProviderDialog {
				d, c := a.providerDialog.Update(msg)
				a.providerDialog = d.(dialog.ProviderDialog)
				return a, c
			}
			if a.showIntegrationDialog {
				d, c := a.integrationDialog.Update(msg)
				a.integrationDialog = d.(dialog.IntegrationDialog)
				return a, c
			}
			if focused, ok := a.pages[a.currentPage].(interface{ WorkspaceFocused() bool }); ok && focused.WorkspaceFocused() {
				a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
				return a, cmd
			}
		}
		// In the terminal pane control keys belong to the child, including
		// Ctrl+C and Escape. Permission/modal dialogs always keep precedence.
		if !a.showPermissions && !a.showHelp && !a.showQuit && !a.showSessionDialog &&
			!a.showCommandDialog && !a.showModelDialog && !a.showInitDialog && !a.showFilepicker &&
			!a.showThemeDialog && !a.showMultiArgumentsDialog && !a.showProviderDialog && !a.showIntegrationDialog && !a.isCompacting {
			if focused, ok := a.pages[a.currentPage].(interface{ TerminalFocused() bool }); ok && focused.TerminalFocused() {
				a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
				return a, cmd
			}
		}
		// Modal shortcuts belong to the focused dialog. In particular Ctrl+H
		// shows archived sessions and Ctrl+F filters favorite models.
		if msg.String() != "ctrl+c" && !a.showPermissions && !a.showQuit && !a.showHelp {
			if a.showSessionDialog {
				updated, c := a.sessionDialog.Update(msg)
				a.sessionDialog = updated.(dialog.SessionDialog)
				return a, c
			}
			if a.showModelDialog && msg.String() != "ctrl+o" {
				updated, c := a.modelDialog.Update(msg)
				a.modelDialog = updated.(dialog.ModelDialog)
				return a, c
			}
		}
		// If multi-arguments dialog is open, let it handle the key press first
		if a.showMultiArgumentsDialog {
			args, cmd := a.multiArgumentsDialog.Update(msg)
			a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
			return a, cmd
		}

		switch {

		case key.Matches(msg, keys.Quit):
			a.showQuit = !a.showQuit
			if a.showHelp {
				a.showHelp = false
			}
			if a.showSessionDialog {
				a.showSessionDialog = false
			}
			if a.showCommandDialog {
				a.showCommandDialog = false
			}
			if a.showFilepicker {
				a.showFilepicker = false
				a.filepicker.ToggleFilepicker(a.showFilepicker)
			}
			if a.showModelDialog {
				a.modelDialog.Close()
				a.showModelDialog = false
			}
			if a.showMultiArgumentsDialog {
				a.showMultiArgumentsDialog = false
			}
			return a, nil
		case key.Matches(msg, keys.SwitchSession):
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showCommandDialog {
				return a, a.openSessions()
			}
			return a, nil
		case key.Matches(msg, keys.Commands):
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showThemeDialog && !a.showFilepicker {
				// Show commands dialog
				if len(a.commands) == 0 {
					return a, util.ReportWarn("No commands available")
				}
				a.commandDialog.SetCommands(a.commands)
				a.showCommandDialog = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.Models):
			if a.showModelDialog {
				a.modelDialog.Close()
				a.showModelDialog = false
				return a, nil
			}
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				a.showModelDialog = true
				return a, a.modelDialog.Open()
			}
			return a, nil
		case key.Matches(msg, keys.SwitchTheme):
			if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				// Show theme switcher dialog
				a.showThemeDialog = true
				// Theme list is dynamically loaded by the dialog component
				return a, a.themeDialog.Init()
			}
			return a, nil
		case key.Matches(msg, returnKey) || key.Matches(msg):
			if msg.String() == quitKey {
				if a.currentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			} else if !a.filepicker.IsCWDFocused() {
				if a.showQuit {
					a.showQuit = !a.showQuit
					return a, nil
				}
				if a.showHelp {
					a.showHelp = !a.showHelp
					return a, nil
				}
				if a.showInitDialog {
					a.showInitDialog = false
					// Mark the project as initialized without running the command
					if err := config.MarkProjectInitialized(); err != nil {
						return a, util.ReportError(err)
					}
					return a, nil
				}
				if a.showFilepicker {
					a.showFilepicker = false
					a.filepicker.ToggleFilepicker(a.showFilepicker)
					return a, nil
				}
				if a.currentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			}
		case key.Matches(msg, keys.Logs):
			return a, a.moveToPage(page.LogsPage)
		case key.Matches(msg, keys.Help):
			if a.showQuit {
				return a, nil
			}
			a.showHelp = !a.showHelp
			return a, nil
		case key.Matches(msg, helpEsc):
			if a.showHelp {
				a.showHelp = false
				return a, nil
			}
		case key.Matches(msg, keys.Filepicker):
			a.showFilepicker = !a.showFilepicker
			a.filepicker.ToggleFilepicker(a.showFilepicker)
			return a, nil
		}
	default:
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

	}

	if a.showProviderDialog {
		d, c := a.providerDialog.Update(msg)
		a.providerDialog = d.(dialog.ProviderDialog)
		cmds = append(cmds, c)
	}
	if a.showIntegrationDialog {
		d, c := a.integrationDialog.Update(msg)
		a.integrationDialog = d.(dialog.IntegrationDialog)
		cmds = append(cmds, c)
	}

	if a.showFilepicker {
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showQuit {
		q, quitCmd := a.quit.Update(msg)
		a.quit = q.(dialog.QuitDialog)
		cmds = append(cmds, quitCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}
	if a.showPermissions {
		d, permissionsCmd := a.permissions.Update(msg)
		a.permissions = d.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permissionsCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showSessionDialog {
		d, sessionCmd := a.sessionDialog.Update(msg)
		a.sessionDialog = d.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showCommandDialog {
		d, commandCmd := a.commandDialog.Update(msg)
		a.commandDialog = d.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showModelDialog {
		d, modelCmd := a.modelDialog.Update(msg)
		a.modelDialog = d.(dialog.ModelDialog)
		cmds = append(cmds, modelCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showInitDialog {
		d, initCmd := a.initDialog.Update(msg)
		a.initDialog = d.(dialog.InitDialogCmp)
		cmds = append(cmds, initCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showThemeDialog {
		d, themeCmd := a.themeDialog.Update(msg)
		a.themeDialog = d.(dialog.ThemeDialog)
		cmds = append(cmds, themeCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	s, _ := a.status.Update(msg)
	a.status = s.(core.StatusCmp)
	a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
	cmds = append(cmds, cmd)
	return a, tea.Batch(cmds...)
}

// RegisterCommand adds a command to the command dialog
func (a *appModel) RegisterCommand(cmd dialog.Command) {
	a.commands = append(a.commands, cmd)
}

func (a *appModel) findCommand(id string) (dialog.Command, bool) {
	for _, cmd := range a.commands {
		if cmd.ID == id {
			return cmd, true
		}
	}
	return dialog.Command{}, false
}

func (a *appModel) moveToPage(pageID page.PageID) tea.Cmd {
	if a.app.CoderAgent.IsBusy() {
		// For now we don't move to any page if the agent is busy
		return util.ReportWarn("Agent is busy, please wait...")
	}

	var cmds []tea.Cmd
	if _, ok := a.loadedPages[pageID]; !ok {
		cmd := a.pages[pageID].Init()
		cmds = append(cmds, cmd)
		a.loadedPages[pageID] = true
	}
	a.previousPage = a.currentPage
	a.currentPage = pageID
	if sizable, ok := a.pages[a.currentPage].(layout.Sizeable); ok {
		cmd := sizable.SetSize(a.width, a.height)
		cmds = append(cmds, cmd)
	}

	return tea.Batch(cmds...)
}

func (a appModel) View() string {
	components := []string{
		a.pages[a.currentPage].View(),
	}

	components = append(components, a.status.View())

	appView := lipgloss.JoinVertical(lipgloss.Top, components...)

	if a.showPermissions {
		overlay := a.permissions.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showFilepicker {
		overlay := a.filepicker.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)

	}

	// Show compacting status overlay
	if a.isCompacting {
		t := theme.CurrentTheme()
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.BorderFocused()).
			BorderBackground(t.Background()).
			Padding(1, 2).
			Background(t.Background()).
			Foreground(t.Text())

		overlay := style.Render("Summarizing\n" + a.compactingMessage)
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showHelp {
		bindings := layout.KeyMapToSlice(keys)
		if p, ok := a.pages[a.currentPage].(layout.Bindings); ok {
			bindings = append(bindings, p.BindingKeys()...)
		}
		if a.showPermissions {
			bindings = append(bindings, a.permissions.BindingKeys()...)
		}
		if a.currentPage == page.LogsPage {
			bindings = append(bindings, logsKeyReturnKey)
		}
		bindings = append(bindings, helpEsc)
		a.help.SetBindings(bindings)

		overlay := a.help.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showQuit {
		overlay := a.quit.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showSessionDialog {
		overlay := a.sessionDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showModelDialog {
		overlay := a.modelDialog.View()
		col, row := a.modelDialogOrigin()
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showCommandDialog {
		overlay := a.commandDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showInitDialog {
		overlay := a.initDialog.View()
		appView = layout.PlaceOverlay(
			a.width/2-lipgloss.Width(overlay)/2,
			a.height/2-lipgloss.Height(overlay)/2,
			overlay,
			appView,
			true,
		)
	}

	if a.showThemeDialog {
		overlay := a.themeDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showMultiArgumentsDialog {
		overlay := a.multiArgumentsDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showProviderDialog {
		overlay := a.providerDialog.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(col, row, overlay, appView, true)
	}
	if a.showIntegrationDialog {
		overlay := a.integrationDialog.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(col, row, overlay, appView, true)
	}
	// Authorization always appears above settings/probes that requested it.
	if a.showPermissions {
		overlay := a.permissions.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(col, row, overlay, appView, true)
	}

	if a.showQuit {
		overlay := a.quit.View()
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(col, row, overlay, appView, true)
	}
	return appView
}

func New(app *app.App, initial ...session.Session) tea.Model {
	startPage := page.ChatPage
	if app.Questions != nil {
		app.Questions.SetInteractive(true)
	}
	model := &appModel{
		currentPage:   startPage,
		loadedPages:   make(map[page.PageID]bool),
		status:        core.NewStatusCmp(app.LSPClients),
		help:          dialog.NewHelpCmp(),
		quit:          dialog.NewQuitCmp(),
		sessionDialog: dialog.NewSessionDialogCmp(),
		commandDialog: dialog.NewCommandDialogCmp(),
		modelDialog:   dialog.NewModelDialogCmp(),
		permissions:   dialog.NewPermissionDialogCmp(),
		initDialog:    dialog.NewInitDialogCmp(),
		themeDialog:   dialog.NewThemeDialogCmp(),
		app:           app,
		commands:      []dialog.Command{},
		pages: map[page.PageID]tea.Model{
			page.ChatPage: page.NewChatPage(app),
			page.LogsPage: page.NewLogsPage(),
		},
		filepicker: dialog.NewFilepickerCmp(app),
	}

	guard := func() error {
		if app.CoderAgent != nil && app.CoderAgent.IsBusy() {
			return fmt.Errorf("Aguarde ou cancele a execução antes de alterar configurações")
		}
		return nil
	}
	model.providerDialog = dialog.NewProviderDialogCmp(guard)
	model.integrationDialog = dialog.NewIntegrationDialogCmp(app.ProbeIntegration, guard)
	for _, entry := range commands.Builtins {
		id := strings.TrimSpace(entry.ID)
		if id == "mode" || id == "plan" || id == "task" || id == "resume" || id == "compact" || id == "skill" {
			continue
		}
		model.RegisterCommand(dialog.Command{ID: id, Title: entry.Title, Description: entry.Description, Handler: func(dialog.Command) tea.Cmd { return util.CmdHandler(page.WorkspaceCommandMsg{Command: id}) }})
	}

	for _, mode := range []struct{ id, title string }{{"ask", "Perguntar — somente leitura"}, {"plan", "Planejar — sem comandos ou edições"}, {"execute", "Executar — com permissões"}} {
		model.RegisterCommand(dialog.Command{ID: "mode-" + mode.id, Title: mode.title, Description: "Trocar o modo da sessão", Handler: func(dialog.Command) tea.Cmd {
			return util.CmdHandler(page.WorkflowCommandMsg{Command: "mode", Value: mode.id})
		}})
	}
	model.RegisterCommand(dialog.Command{ID: "plan", Title: "Mostrar/recolher plano", Description: "Etapas e progresso salvos na sessão", Handler: func(dialog.Command) tea.Cmd { return util.CmdHandler(page.WorkflowCommandMsg{Command: "plan"}) }})
	model.RegisterCommand(dialog.Command{ID: "resume-task", Title: "Retomar tarefa", Description: "Continuar o plano com novas aprovações quando necessárias", Handler: func(dialog.Command) tea.Cmd { return util.CmdHandler(page.WorkflowCommandMsg{Command: "resume"}) }})
	model.RegisterCommand(dialog.Command{
		ID:          "init",
		Title:       "Initialize Project",
		Description: "Create/Update the Opcode.md memory file",
		Handler: func(cmd dialog.Command) tea.Cmd {
			prompt := `Please analyze this codebase and create a Opcode.md file containing:
1. Build/lint/test commands - especially for running a single test
2. Code style guidelines including imports, formatting, types, naming conventions, error handling, etc.

The file you create will be given to agentic coding agents (such as yourself) that operate in this repository. Make it about 20 lines long.
If there's already a opcode.md, improve it.
If there are Cursor rules (in .cursor/rules/ or .cursorrules) or Copilot rules (in .github/copilot-instructions.md), make sure to include them.`
			return tea.Batch(
				util.CmdHandler(chat.SendMsg{
					Text: prompt,
				}),
			)
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "compact",
		Title:       "Compactar contexto",
		Description: "Preparar contexto menor preservando a conversa salva",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				return page.WorkspaceCommandMsg{Command: "compact"}
			}
		},
	})
	if len(initial) > 0 {
		model.selectedSession = initial[0]
	} else if cfg := config.Get(); cfg != nil && cfg.Preferences.ResumeLast && app.Sessions != nil {
		metadata, last, err := session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory).Snapshot()
		if err == nil && last != "" && !metadata[last].Archived {
			if sess, err := app.Sessions.Get(context.Background(), last); err == nil {
				model.selectedSession = sess
			}
		}
	}
	// Load custom commands
	customCommands, err := dialog.LoadCustomCommands()
	if err != nil {
		logging.Warn("Failed to load custom commands", "error", err)
	} else {
		for _, cmd := range customCommands {
			model.RegisterCommand(cmd)
		}
	}

	return model
}

func (a appModel) modelDialogOrigin() (int, int) {
	base := lipgloss.JoinVertical(lipgloss.Top, a.pages[a.currentPage].View(), a.status.View())
	overlay := a.modelDialog.View()
	width, height := lipgloss.Width(base), lipgloss.Height(base)
	ow, oh := lipgloss.Width(overlay), lipgloss.Height(overlay)
	// PlaceOverlay adds a one-cell shadow, then clamps the origin.
	return max(0, min(width/2-ow/2, width-ow-1)), max(0, min(height/2-oh/2, height-oh-1))
}

func agentEventSession(event agent.AgentEvent, fallback string) string {
	if event.SessionID != "" {
		return event.SessionID
	}
	if event.Message.SessionID != "" {
		return event.Message.SessionID
	}
	return fallback
}
