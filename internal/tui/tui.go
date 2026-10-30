package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/clipboard"
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
	terminalOutput  io.Writer
	width, height   int
	unfocused       bool
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

	showModelDialog        bool
	modelDialog            dialog.ModelDialog
	setupActive            bool
	showProviderDialog     bool
	providerReturnToModels bool
	providerDialog         dialog.ProviderDialog
	showIntegrationDialog  bool
	integrationDialog      dialog.IntegrationDialog

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
			return dialog.OpenProviderDialogMsg{Setup: true}
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

func (a appModel) Update(msg tea.Msg) (updated tea.Model, result tea.Cmd) {
	defer func() {
		if next, ok := updated.(appModel); ok {
			if focus := next.syncInputFocus(); focus != nil {
				result = tea.Batch(result, focus)
			}
		}
	}()
	if cmd, handled := a.productivityMessage(msg); handled {
		return a, cmd
	}
	next, cmd, stop := a.routeEvent(msg)
	a = next.(appModel)
	if stop {
		return a, cmd
	}
	return a, a.deliverEvent(msg, cmd)
}

func (a *appModel) routeEvent(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg.(type) {
	case tea.FocusMsg, tea.BlurMsg, dialog.ModelCatalogLoadedMsg, dialog.ModelProbeResultMsg, tea.MouseMsg, tea.WindowSizeMsg, tea.KeyMsg:
		return a.routeInput(msg)
	case util.InfoMsg, pubsub.Event[logging.LogMessage], util.ClearStatusMsg:
		return a.routeStatus(msg)
	case pubsub.Event[permission.PermissionRequest], dialog.PermissionResponseMsg:
		return a.routePermission(msg)
	case chat.SessionClearedMsg, chat.SessionSelectedMsg, pubsub.Event[session.Session], dialog.SessionSelectedMsg:
		return a.routeSession(msg)
	case dialog.CommandSelectedMsg, dialog.ShowMultiArgumentsDialogMsg, dialog.CloseMultiArgumentsDialogMsg:
		return a.routeCommands(msg)
	case dialog.CloseThemeDialogMsg, dialog.ThemeChangedMsg:
		return a.routeTheme(msg)
	case dialog.ModelSelectedMsg, dialog.ShowInitDialogMsg, dialog.CloseInitDialogMsg:
		return a.routeSetup(msg)
	case dialog.ConfirmQuitMsg, dialog.CloseQuitMsg, dialog.CloseSessionDialogMsg, dialog.CloseCommandDialogMsg, dialog.CloseModelDialogMsg:
		return a.routeLifecycle(msg)
	case page.GitWorkspaceMsg, page.TerminalTickMsg, page.TerminalStartedMsg, page.WorkflowCommandMsg, page.WorkspaceCommandMsg, page.WorkspaceResultMsg, chat.ResponseActionsMsg, chat.SubmissionResultMsg, page.PageChangeMsg, startCompactSessionMsg, pubsub.Event[agent.AgentEvent]:
		return a.routeWorkspace(msg)
	}
	return a.routeWorkspace(msg)
}

func (a *appModel) routeInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.FocusMsg:
		a.unfocused = false
		return *a, nil, true

	case tea.BlurMsg:
		a.unfocused = true
		return *a, nil, true

	case dialog.ModelCatalogLoadedMsg, dialog.ModelProbeResultMsg:
		d, cmd := a.modelDialog.Update(msg)
		a.modelDialog = d.(dialog.ModelDialog)
		return *a, cmd, true

	case tea.MouseMsg:
		if result0, result1, handled := updateAppModelMouseMsg(a, &msg); handled {
			return result0, result1, true
		}

	case tea.WindowSizeMsg:
		if result0, result1, handled := updateAppModelWindowSizeMsg(a, &cmds, &cmd, &msg); handled {
			return result0, result1, true
		}
	// Status

	case tea.KeyMsg:
		if result0, result1, handled := updateAppModelKeyMsg(a, &cmd, &msg); handled {
			return result0, result1, true
		}

	}
	return *a, tea.Batch(cmds...), false
}

func (a *appModel) routeStatus(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case util.InfoMsg:
		s, cmd := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		cmds = append(cmds, cmd)
		return *a, tea.Batch(cmds...), true

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

	}
	return *a, tea.Batch(cmds...), false
}

func (a *appModel) routePermission(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case pubsub.Event[permission.PermissionRequest]:
		a.showPermissions = true
		activity, activityCmd := a.pages[page.ChatPage].Update(page.ActivityMsg{SessionID: msg.Payload.SessionID, State: config.Text("Aguardando aprovação", "Waiting for approval")})
		a.pages[page.ChatPage] = activity
		return *a, tea.Batch(a.permissions.SetPermissions(msg.Payload), activityCmd, a.notify()), true

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
		activity, activityCmd := a.pages[page.ChatPage].Update(page.ActivityMsg{SessionID: msg.Permission.SessionID, State: config.Text("Executando operação", "Running operation")})
		a.pages[page.ChatPage] = activity
		return *a, tea.Batch(cmd, activityCmd), true

	}
	return *a, tea.Batch(cmds...), false
}

func (a *appModel) routeSession(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case chat.SessionClearedMsg:
		if result0, result1, result2, handled := routeSessionCaseChatSessionClearedMsg(a); handled {
			return result0, result1, result2
		}

	case chat.SessionSelectedMsg:
		if result0, result1, handled := updateAppModelSessionSelectedMsg(a, &cmds, &msg); handled {
			return result0, result1, true
		}

	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == a.selectedSession.ID {
			a.selectedSession = msg.Payload
		}

	case dialog.SessionSelectedMsg:
		a.showSessionDialog = false
		if a.currentPage == page.ChatPage {
			return *a, util.CmdHandler(chat.SessionSelectedMsg(msg.Session)), true
		}
		return *a, nil, true

	}
	return *a, tea.Batch(cmds...), false
}

func (a *appModel) routeCommands(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case dialog.CommandSelectedMsg:
		a.showCommandDialog = false
		// Execute the command handler if available
		if msg.Command.Handler != nil {
			return *a, msg.Command.Handler(msg.Command), true
		}
		return *a, util.ReportInfo(config.Text("Comando selecionado: ", "Command selected: ") + msg.Command.Title), true

	case dialog.ShowMultiArgumentsDialogMsg:
		// Show multi-arguments dialog
		a.multiArgumentsDialog = dialog.NewMultiArgumentsDialogCmp(msg.CommandID, msg.Content, msg.ArgNames)
		a.showMultiArgumentsDialog = true
		return *a, a.multiArgumentsDialog.Init(), true

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
			return *a, util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: content,
				Args:    msg.Args,
			}), true
		}
		return *a, nil, true

	}
	return *a, tea.Batch(cmds...), false
}

func (a *appModel) routeTheme(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case dialog.CloseThemeDialogMsg:
		a.showThemeDialog = false
		if msg.RestoreTheme != "" {
			if err := theme.PreviewTheme(msg.RestoreTheme); err != nil {
				return *a, util.ReportError(err), true
			}
			a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(dialog.ThemeChangedMsg{ThemeName: msg.RestoreTheme, Preview: true})
		}
		return *a, cmd, true

	case dialog.ThemeChangedMsg:
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
		if msg.Preview {
			return *a, cmd, true
		}
		a.showThemeDialog = false
		return *a, tea.Batch(cmd, util.ReportInfo(config.Text("Tema alterado para: ", "Theme changed to: ")+msg.ThemeName)), true

	}
	return *a, tea.Batch(cmds...), false
}

func (a *appModel) routeSetup(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case dialog.ModelSelectedMsg:
		if result0, result1, handled := updateAppModelModelSelectedMsg(a, &msg); handled {
			return result0, result1, true
		}

	case dialog.ShowInitDialogMsg:
		a.showInitDialog = msg.Show && !a.showProviderDialog && !a.showIntegrationDialog && a.app.CoderAgent != nil && a.app.CoderAgent.Model().ID != ""
		return *a, nil, true

	case dialog.CloseInitDialogMsg:
		if result0, result1, handled := updateAppModelCloseInitDialogMsg(a, &msg); handled {
			return result0, result1, true
		}

	}
	return *a, tea.Batch(cmds...), false
}

func (a *appModel) routeLifecycle(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg.(type) {
	case dialog.ConfirmQuitMsg:
		if a.pendingProviderNativeOperation() {
			a.showQuit = false
			return *a, nativeOperationQuitNotice(), true
		}
		if current, ok := a.pages[page.ChatPage].(interface{ PersistDraft() error }); ok {
			if err := current.PersistDraft(); err != nil {
				return *a, util.ReportError(err), true
			}
		}
		return *a, tea.Quit, true

	case dialog.CloseQuitMsg:
		a.showQuit = false
		return *a, nil, true

	case dialog.CloseSessionDialogMsg:
		a.showSessionDialog = false
		return *a, nil, true

	case dialog.CloseCommandDialogMsg:
		a.showCommandDialog = false
		return *a, nil, true

	case dialog.CloseModelDialogMsg:
		a.modelDialog.Close()
		a.showModelDialog = false
		return *a, nil, true

	}
	return *a, tea.Batch(cmds...), false
}

func (a *appModel) routeWorkspace(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case page.GitWorkspaceMsg, page.TerminalTickMsg, page.TerminalStartedMsg, page.WorkflowCommandMsg, page.WorkspaceCommandMsg, page.WorkspaceResultMsg, chat.ResponseActionsMsg, chat.SubmissionResultMsg:
		if chatPage, ok := a.pages[page.ChatPage]; ok {
			a.pages[page.ChatPage], cmd = chatPage.Update(msg)
			return *a, cmd, true
		}
		return *a, nil, true

	case page.PageChangeMsg:
		return *a, a.moveToPage(msg.ID), true

	case startCompactSessionMsg:
		if chatPage, ok := a.pages[page.ChatPage]; ok {
			a.pages[page.ChatPage], cmd = chatPage.Update(page.WorkspaceCommandMsg{Command: "compact"})
			return *a, cmd, true
		}
		return *a, nil, true

	case pubsub.Event[agent.AgentEvent]:
		if result0, result1, handled := updateAgentEvent(a, &msg); handled {
			return result0, result1, true
		}

	default:
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

	}
	return *a, tea.Batch(cmds...), false
}

type dialogRoute uint8

const (
	routeProvider dialogRoute = iota
	routeIntegration
	routeFilepicker
	routeQuit
	routePermission
	routeSessions
	routeCommands
	routeModels
	routeInit
	routeTheme
)

func (a *appModel) updateDialog(route dialogRoute, msg tea.Msg) tea.Cmd {
	switch route {
	case routeProvider:
		next, cmd := a.providerDialog.Update(msg)
		a.providerDialog = next.(dialog.ProviderDialog)
		return cmd
	case routeIntegration:
		next, cmd := a.integrationDialog.Update(msg)
		a.integrationDialog = next.(dialog.IntegrationDialog)
		return cmd
	case routeFilepicker:
		next, cmd := a.filepicker.Update(msg)
		a.filepicker = next.(dialog.FilepickerCmp)
		return cmd
	case routeQuit:
		next, cmd := a.quit.Update(msg)
		a.quit = next.(dialog.QuitDialog)
		return cmd
	case routePermission:
		next, cmd := a.permissions.Update(msg)
		a.permissions = next.(dialog.PermissionDialogCmp)
		return cmd
	case routeSessions:
		next, cmd := a.sessionDialog.Update(msg)
		a.sessionDialog = next.(dialog.SessionDialog)
		return cmd
	case routeCommands:
		next, cmd := a.commandDialog.Update(msg)
		a.commandDialog = next.(dialog.CommandDialog)
		return cmd
	case routeModels:
		next, cmd := a.modelDialog.Update(msg)
		a.modelDialog = next.(dialog.ModelDialog)
		return cmd
	case routeInit:
		next, cmd := a.initDialog.Update(msg)
		a.initDialog = next.(dialog.InitDialogCmp)
		return cmd
	case routeTheme:
		next, cmd := a.themeDialog.Update(msg)
		a.themeDialog = next.(dialog.ThemeDialog)
		return cmd
	}
	return nil
}

func (a *appModel) deliverEvent(msg tea.Msg, initial tea.Cmd) tea.Cmd {
	cmds := []tea.Cmd{initial}
	// Preserve precedence: connection dialogs receive keys without capturing
	// the general path; the remaining visible modals capture keyboard input.
	routes := [...]struct {
		show    bool
		route   dialogRoute
		capture bool
	}{
		{a.showProviderDialog, routeProvider, false}, {a.showIntegrationDialog, routeIntegration, false},
		{a.showFilepicker, routeFilepicker, true}, {a.showQuit, routeQuit, true},
		{a.showPermissions, routePermission, true}, {a.showSessionDialog, routeSessions, true},
		{a.showCommandDialog, routeCommands, true}, {a.showModelDialog, routeModels, true},
		{a.showInitDialog, routeInit, true}, {a.showThemeDialog, routeTheme, true},
	}
	_, key := msg.(tea.KeyMsg)
	for _, entry := range routes {
		if !entry.show {
			continue
		}
		cmds = append(cmds, a.updateDialog(entry.route, msg))
		if key && entry.capture {
			return tea.Batch(cmds...)
		}
	}
	status, _ := a.status.Update(msg)
	a.status = status.(core.StatusCmp)
	next, cmd := a.pages[a.currentPage].Update(msg)
	a.pages[a.currentPage] = next
	return tea.Batch(append(cmds, cmd)...)
}

func (a appModel) pendingProviderNativeOperation() bool {
	pending, ok := a.providerDialog.(interface{ PendingNativeOperation() bool })
	return ok && pending.PendingNativeOperation()
}

func nativeOperationQuitNotice() tea.Cmd {
	return util.ReportInfo(config.Text("Aguarde a operação do cofre do sistema terminar antes de sair. Ela não pode ser cancelada.", "Wait for the system vault operation to finish before quitting. It cannot be cancelled."))
}

// RegisterCommand adds a command to the command dialog
func (a *appModel) RegisterCommand(cmd dialog.Command) {
	a.commands = append(a.commands, cmd)
}

func (a *appModel) moveToPage(pageID page.PageID) tea.Cmd {
	if a.app.CoderAgent.IsBusy() {
		// For now we don't move to any page if the agent is busy
		return util.ReportWarn(config.Text("O agente está trabalhando; aguarde…", "Agent is busy, please wait..."))
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

// baseView is shared by rendering and mouse hit testing so both use the same
// bounded page dimensions, including while a page is loading or resizing.
func (a appModel) baseView() string {
	pageView := a.pages[a.currentPage].View()
	if a.width > 0 {
		if a.height == 0 {
			pageView = ""
		} else {
			pageView = lipgloss.NewStyle().Width(a.width).Height(a.height).MaxWidth(a.width).MaxHeight(a.height).Render(pageView)
		}
	}
	components := []string{
		pageView,
	}
	if pageView == "" {
		components = nil
	}

	components = append(components, a.statusView())

	return lipgloss.JoinVertical(lipgloss.Top, components...)
}

func (a appModel) View() string {
	appView := a.baseView()

	if a.showFilepicker {
		overlay := a.filepicker.View()
		col, row := a.dialogOrigin(overlay)
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
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	viewShowHelp(&a, &appView)

	if a.showQuit {
		overlay := a.quit.View()
		col, row := a.dialogOrigin(overlay)
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
		col, row := a.dialogOrigin(overlay)
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
		col, row := a.dialogOrigin(overlay)
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
		col, row := a.dialogOrigin(overlay)
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showThemeDialog {
		overlay := a.themeDialog.View()
		col, row := a.dialogOrigin(overlay)
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
		col, row := a.dialogOrigin(overlay)
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
	if a.width > 0 {
		appView = lipgloss.NewStyle().MaxWidth(a.width).MaxHeight(a.height + 1).Render(appView)
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
			return fmt.Errorf("aguarde ou cancele a execução antes de alterar configurações")
		}
		return nil
	}
	model.providerDialog = dialog.NewProviderDialogCmp(guard)
	model.integrationDialog = dialog.NewIntegrationDialogCmp(app.ProbeIntegration, guard)
	if d, ok := model.integrationDialog.(interface{ SetOAuthHandler(dialog.OAuthHandler) }); ok {
		d.SetOAuthHandler(func(ctx context.Context, action, name string, notify func(string) error) (string, error) {
			switch action {
			case "login":
				err := agent.LoginMCPOAuth(ctx, config.Get().MCPServers[name], notify)
				return config.Text("Autenticação concluída.", "Authentication completed."), err
			case "logout":
				err := agent.LogoutMCPOAuth(ctx, config.Get().MCPServers[name])
				return config.Text("Credenciais removidas.", "Credentials removed."), err
			default:
				connected, err := agent.MCPOAuthStatus(ctx, config.Get().MCPServers[name])
				return fmt.Sprintf("OAuth: %t", connected), err
			}
		})
	}
	newCommandsBuiltins(model)

	for _, mode := range []struct{ id, title string }{{"ask", config.Text("Perguntar — somente leitura", "Ask — read only")}, {"plan", config.Text("Planejar — sem comandos ou edições", "Plan — no commands or edits")}, {"execute", config.Text("Executar — com permissões", "Execute — with permissions")}} {
		model.RegisterCommand(dialog.Command{ID: "mode-" + mode.id, Title: mode.title, Description: config.Text("Trocar o modo da sessão", "Change the session mode"), Handler: func(dialog.Command) tea.Cmd {
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
	newInitial(app, initial, model)
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
	return a.dialogOrigin(a.modelDialog.View())
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

func configuredKeys() keyMap {
	k := keys
	k.Commands.SetKeys(config.Shortcut("commands"))
	k.Commands.SetHelp(config.Shortcut("commands"), "commands")
	k.Models.SetKeys(config.Shortcut("models"))
	k.Models.SetHelp(config.Shortcut("models"), "model selection")
	k.SwitchSession.SetKeys(config.Shortcut("sessions"))
	k.SwitchSession.SetHelp(config.Shortcut("sessions"), "switch session")
	k.Filepicker.SetKeys(config.Shortcut("files"))
	k.Filepicker.SetHelp(config.Shortcut("files"), "select files to upload")
	k.SwitchTheme.SetKeys(config.Shortcut("theme"))
	k.SwitchTheme.SetHelp(config.Shortcut("theme"), "switch theme")
	return k
}

func (a appModel) mouseModal() (tea.Model, bool) {
	switch {
	case a.showQuit:
		return a.quit, true
	case a.showCommandDialog:
		return a.commandDialog, true
	case a.showThemeDialog:
		return a.themeDialog, true
	case a.showFilepicker:
		return a.filepicker, true
	case a.showMultiArgumentsDialog:
		return a.multiArgumentsDialog, true
	case a.showInitDialog:
		return a.initDialog, true
	}
	return nil, false
}

// WithTerminalOutput shares the serialized terminal writer with explicit OSC commands.
func WithTerminalOutput(model tea.Model, output io.Writer) tea.Model {
	switch a := model.(type) {
	case *appModel: // New returns a pointer before Bubble Tea's first Update.
		a.terminalOutput = output
	case appModel:
		a.terminalOutput = output
		return a
	}
	return model
}

func updateAppModelKeyMsg(a *appModel, cmd *tea.Cmd, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, handled bool) {
	if result0, result1, result2, handled := routeRemotePasteKey(a, msg); handled {
		return result0, result1, result2
	}
	if result0, result1, result2, handled := routeQuitOrPermissionKey(a, msg); handled {
		return result0, result1, result2
	}
	if result0, result1, result2, handled := routeConnectionOrWorkspaceKey(a, cmd, msg); handled {
		return result0, result1, result2
	}
	// In the terminal pane control keys belong to the child, including
	// Ctrl+C and Escape. Permission/modal dialogs always keep precedence.
	if result0, result1, result2, handled := routeTerminalOrSelectionKey(a, cmd, msg); handled {
		return result0, result1, result2
	}
	// Modal shortcuts belong to the focused dialog. In particular Ctrl+H
	// shows archived sessions and Ctrl+F filters favorite models.
	if result0, result1, result2, handled := routeFocusedHelpKey(a, msg); handled {
		return result0, result1, result2
	}
	if result0, result1, result2, handled := routeSessionOrModelKey(a, msg); handled {
		return result0, result1, result2
	}
	// If multi-arguments dialog is open, let it handle the key press first
	if a.showMultiArgumentsDialog {
		args, cmd := a.multiArgumentsDialog.Update((*msg))
		a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
		return (*a), cmd, true
	}

	return updateApplicationShortcut(a, msg)
}

func updateApplicationShortcut(a *appModel, msg *tea.KeyMsg) (tea.Model, tea.Cmd, bool) {

	switch {

	case key.Matches((*msg), keys.Quit):
		return updateAppModelKeyMsgQuit(a)
	case msg.String() == config.Shortcut("sessions"):
		return updateAppModelKeyMsgSessions(a)
	case msg.String() == config.Shortcut("commands"):
		return updateAppModelKeyMsgCommands(a)
	case msg.String() == config.Shortcut("models"):
		return updateAppModelKeyMsgModels(a)
	case msg.String() == config.Shortcut("theme"):
		return updateAppModelKeyMsgTheme(a)
	case key.Matches((*msg), returnKey) || key.Matches((*msg)):
		if result0, result1, result2, handled := updateAppModelKeyMsgMatches(a, msg); handled {
			return result0, result1, result2
		}
	case key.Matches((*msg), keys.Logs):
		return (*a), a.moveToPage(page.LogsPage), true
	case key.Matches((*msg), keys.Help):
		if a.showQuit {
			return (*a), nil, true
		}
		a.showHelp = !a.showHelp
		return (*a), nil, true
	case key.Matches((*msg), helpEsc):
		if a.showHelp {
			a.showHelp = false
			return (*a), nil, true
		}
	case msg.String() == config.Shortcut("files"):
		a.showFilepicker = !a.showFilepicker
		a.filepicker.ToggleFilepicker(a.showFilepicker)
		return (*a), nil, true
	}
	return nil, nil, false
}

func updateAppModelMouseMsg(a *appModel, msg *tea.MouseMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, handled bool) {
	if a.showPermissions && a.permissions != nil {
		col, row := a.dialogOrigin(a.permissions.View())
		msg.X -= col
		msg.Y -= row
		updated, c := a.permissions.Update((*msg))
		a.permissions = updated.(dialog.PermissionDialogCmp)
		return (*a), c, true
	}
	if a.showHelp && a.help != nil {
		col, row := a.dialogOrigin(a.help.View())
		msg.X -= col
		msg.Y -= row
		updated, c := a.help.Update((*msg))
		a.help = updated.(dialog.HelpCmp)
		return (*a), c, true
	}
	if result0, result1, result2, handled := updateAppModelMouseMsgMouseModal(a, msg); handled {
		return result0, result1, result2
	}
	// Capturing the wheel must not scroll the conversation underneath a
	// modal dialog. The model picker handles its own mouse navigation.
	if result0, result1, result2, handled := updateAppModelMouseMsgShowPermissionsShowHelpShowQuit(a); handled {
		return result0, result1, result2
	}
	if a.showSessionDialog {
		if a.sessionDialog == nil {
			return (*a), nil, true
		}
		col, row := a.dialogOrigin(a.sessionDialog.View())
		msg.X -= col
		msg.Y -= row
		updated, c := a.sessionDialog.Update((*msg))
		a.sessionDialog = updated.(dialog.SessionDialog)
		return (*a), c, true
	}
	if a.showProviderDialog {
		col, row := a.dialogOrigin(a.providerDialog.View())
		msg.X -= col
		msg.Y -= row
		d, cmd := a.providerDialog.Update((*msg))
		a.providerDialog = d.(dialog.ProviderDialog)
		return (*a), cmd, true
	}
	if a.showIntegrationDialog {
		col, row := a.dialogOrigin(a.integrationDialog.View())
		msg.X -= col
		msg.Y -= row
		d, cmd := a.integrationDialog.Update((*msg))
		a.integrationDialog = d.(dialog.IntegrationDialog)
		return (*a), cmd, true
	}
	if a.showModelDialog {
		if a.modelDialog == nil {
			return (*a), nil, true
		}
		col, row := a.modelDialogOrigin()
		msg.X -= col
		msg.Y -= row
		d, cmd := a.modelDialog.Update((*msg))
		a.modelDialog = d.(dialog.ModelDialog)
		return (*a), cmd, true
	}
	if result0, result1, result2, handled := updateAppModelMouseMsgButtonTeaMouseButtonLeft(a, msg); handled {
		return result0, result1, result2
	}
	return extractedResult0, extractedResult1, false
}

func updateAgentEvent(a *appModel, msg *pubsub.Event[agent.AgentEvent]) (updated tea.Model, result tea.Cmd, handled bool) {
	var cmds []tea.Cmd
	defer func() { result = tea.Batch(append(cmds, result)...) }()
	payload := msg.Payload
	if p, ok := a.pages[page.ChatPage]; ok && payload.Message.SessionID != "" {
		updated, c := p.Update(page.ActivityMsg{SessionID: payload.Message.SessionID, State: page.ActivityForMessage(payload.Message)})
		a.pages[page.ChatPage] = updated
		cmds = append(cmds, c)
	}
	if payload.Done && payload.Type == agent.AgentEventTypeResponse {
		cmds = append(cmds, a.notify())
	}
	if payload.Type == agent.AgentEventTypeInstructions {
		if payload.SessionID == a.selectedSession.ID {
			return (*a), util.ReportInfo(payload.Progress), true
		}
		return (*a), nil, true
	}
	if payload.Error != nil {
		a.isCompacting = false
		id := payload.SessionID
		if id == "" {
			id = a.selectedSession.ID
		}
		p, pcmd := a.pages[page.ChatPage].Update(page.AgentOutcomeMsg{SessionID: id, Error: payload.Error, Done: payload.Done, Outcome: payload.Outcome})
		a.pages[page.ChatPage] = p
		return (*a), tea.Batch(pcmd, util.ReportWarn(page.RecoverySummary(payload.Error))), true
	}
	if payload.Done {
		p, pcmd := a.pages[page.ChatPage].Update(page.AgentOutcomeMsg{SessionID: agentEventSession(payload, a.selectedSession.ID), Done: true, Outcome: payload.Outcome})
		a.pages[page.ChatPage] = p
		cmds = append(cmds, pcmd)
	}

	a.compactingMessage = payload.Progress

	if result0, result1, result2, handled := finishAgentEvent(a, &payload); handled {
		return result0, result1, result2
	}
	// Continue listening for events
	return (*a), nil, true

}

func updateAppModelSessionSelectedMsg(a *appModel, cmds *[]tea.Cmd, msg *chat.SessionSelectedMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, handled bool) {
	if current, ok := a.pages[page.ChatPage].(interface{ PrepareSessionDraft(string) error }); ok {
		if err := current.PrepareSessionDraft(msg.ID); err != nil {
			return (*a), util.ReportError(err), true
		}
	}
	if a.selectedSession.ID != "" && a.selectedSession.ID != msg.ID && a.app.Permissions != nil {
		a.app.Permissions.RevokeSession(a.selectedSession.ID)
	}
	a.selectedSession = (*msg)
	if cfg := config.Get(); cfg != nil && msg.ID != "" {
		if err := session.NewCatalog(cfg.WorkingDir, cfg.Data.Directory).Activate(msg.ID); err != nil {
			(*cmds) = append((*cmds), util.ReportError(err))
		}
	}
	a.sessionDialog.SetSelectedSession(msg.ID)
	return extractedResult0, extractedResult1, false
}

func updateAppModelCloseInitDialogMsg(a *appModel, msg *dialog.CloseInitDialogMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, handled bool) {
	a.showInitDialog = false
	if msg.Initialize {
		// Run the initialization command
		for _, cmd := range a.commands {
			if cmd.ID == "init" {
				// Mark the project as initialized
				if err := config.MarkProjectInitialized(); err != nil {
					return (*a), util.ReportError(err), true
				}
				return (*a), cmd.Handler(cmd), true
			}
		}
	} else {
		// Mark the project as initialized without running the command
		if err := config.MarkProjectInitialized(); err != nil {
			return (*a), util.ReportError(err), true
		}
	}
	return (*a), nil, true

}

func updateAppModelWindowSizeMsg(a *appModel, cmds *[]tea.Cmd, cmd *tea.Cmd, msg *tea.WindowSizeMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, handled bool) {
	msg.Height = max(0, msg.Height-1) // Make space for the status bar
	a.width, a.height = msg.Width, msg.Height
	if a.providerDialog != nil {
		a.providerDialog.Update((*msg))
	}
	if a.integrationDialog != nil {
		a.integrationDialog.Update((*msg))
	}
	a.modelDialog.Update((*msg))
	if a.themeDialog != nil {
		a.themeDialog.Update((*msg))
	}

	s, _ := a.status.Update((*msg))
	a.status = s.(core.StatusCmp)
	a.pages[a.currentPage], (*cmd) = a.pages[a.currentPage].Update((*msg))
	(*cmds) = append((*cmds), (*cmd))

	prm, permCmd := a.permissions.Update((*msg))
	a.permissions = prm.(dialog.PermissionDialogCmp)
	(*cmds) = append((*cmds), permCmd)

	help, helpCmd := a.help.Update((*msg))
	a.help = help.(dialog.HelpCmp)
	(*cmds) = append((*cmds), helpCmd)

	session, sessionCmd := a.sessionDialog.Update((*msg))
	a.sessionDialog = session.(dialog.SessionDialog)
	(*cmds) = append((*cmds), sessionCmd)

	command, commandCmd := a.commandDialog.Update((*msg))
	a.commandDialog = command.(dialog.CommandDialog)
	(*cmds) = append((*cmds), commandCmd)

	filepicker, filepickerCmd := a.filepicker.Update((*msg))
	a.filepicker = filepicker.(dialog.FilepickerCmp)
	(*cmds) = append((*cmds), filepickerCmd)

	a.initDialog.SetSize(msg.Width, msg.Height)

	if a.showMultiArgumentsDialog {
		a.multiArgumentsDialog.SetSize(msg.Width, msg.Height)
		args, argsCmd := a.multiArgumentsDialog.Update((*msg))
		a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
		(*cmds) = append((*cmds), argsCmd)
	}

	return (*a), tea.Batch((*cmds)...), true

}

func updateAppModelKeyMsgMatches(a *appModel, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if msg.String() == quitKey {
		if a.currentPage == page.LogsPage {
			return (*a), a.moveToPage(page.ChatPage), true, true
		}
	} else if result0, result1, result2, result3, handled := updateAppModelKeyMsgMatchesFilepickerIsCWDFocused(a); handled {
		return result0, result1, result2, result3
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func updateAppModelModelSelectedMsg(a *appModel, msg *dialog.ModelSelectedMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, handled bool) {
	target := msg.Agent
	if target == "" {
		target = config.AgentCoder
	}
	model, err := a.app.CoderAgent.SelectModel(target, msg.Model)
	if err != nil {
		return (*a), util.ReportError(err), true
	}
	a.modelDialog.Close()
	a.showModelDialog = false
	if d, ok := a.modelDialog.(interface{ SetSetup(bool) }); ok {
		d.SetSetup(false)
	}

	if a.setupActive {
		a.setupActive = false
		return (*a), tea.Batch(util.ReportInfo(fmt.Sprintf(config.Text("Modelo alterado para %s", "Model changed to %s"), model.Name)), util.CmdHandler(page.WorkspaceCommandMsg{Command: "setup"})), true
	}
	return (*a), util.ReportInfo(fmt.Sprintf(config.Text("Modelo alterado para %s", "Model changed to %s"), model.Name)), true

}

func updateAppModelKeyMsgQuit(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool) {
	if a.pendingProviderNativeOperation() {
		a.showQuit = false
		return (*a), nativeOperationQuitNotice(), true
	}
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
	return (*a), nil, true

}

func updateAppModelKeyMsgCommands(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool) {
	if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showThemeDialog && !a.showFilepicker {
		// Show commands dialog
		if len(a.commands) == 0 {
			return (*a), util.ReportWarn(config.Text("Nenhum comando disponível", "No commands available")), true
		}
		a.commandDialog.SetCommands(a.contextualCommands())
		a.showCommandDialog = true
		return (*a), nil, true
	}
	return (*a), nil, true

}

func updateAppModelKeyMsgModels(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool) {
	if a.showModelDialog {
		a.modelDialog.Close()
		a.showModelDialog = false
		return (*a), nil, true
	}
	if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
		a.showModelDialog = true
		return (*a), a.openModelCatalog("", false), true
	}
	return (*a), nil, true

}

func updateAppModelKeyMsgSessions(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool) {
	if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showCommandDialog {
		return (*a), a.openSessions(), true
	}
	return (*a), nil, true

}

func updateAppModelKeyMsgTheme(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool) {
	if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
		// Show theme switcher dialog
		a.showThemeDialog = true
		// Theme list is dynamically loaded by the dialog component
		return (*a), a.themeDialog.Init(), true
	}
	return (*a), nil, true

}

func routeTerminalOrSelectionKey(a *appModel, cmd *tea.Cmd, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if !a.showPermissions && !a.showHelp && !a.showQuit && !a.showSessionDialog &&
		!a.showCommandDialog && !a.showModelDialog && !a.showInitDialog && !a.showFilepicker &&
		!a.showThemeDialog && !a.showMultiArgumentsDialog && !a.showProviderDialog && !a.showIntegrationDialog && !a.isCompacting {
		if focused, ok := a.pages[a.currentPage].(interface{ TerminalFocused() bool }); ok && focused.TerminalFocused() {
			a.pages[a.currentPage], (*cmd) = a.pages[a.currentPage].Update((*msg))
			return (*a), (*cmd), true, true
		}
		if selection, ok := a.pages[a.currentPage].(interface {
			SelectionKey(tea.KeyMsg) (tea.Cmd, bool)
		}); ok {
			if cmd, handled := selection.SelectionKey((*msg)); handled {
				return (*a), cmd, true, true
			}
		}
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func routeConnectionOrWorkspaceKey(a *appModel, cmd *tea.Cmd, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if msg.String() != "ctrl+c" && !a.showPermissions && !a.showHelp && !a.showQuit && !a.showSessionDialog && !a.showCommandDialog && !a.showModelDialog && !a.showInitDialog && !a.showFilepicker && !a.showThemeDialog && !a.showMultiArgumentsDialog {
		if a.showProviderDialog {
			d, c := a.providerDialog.Update((*msg))
			a.providerDialog = d.(dialog.ProviderDialog)
			return (*a), c, true, true
		}
		if a.showIntegrationDialog {
			d, c := a.integrationDialog.Update((*msg))
			a.integrationDialog = d.(dialog.IntegrationDialog)
			return (*a), c, true, true
		}
		if focused, ok := a.pages[a.currentPage].(interface{ WorkspaceFocused() bool }); ok && focused.WorkspaceFocused() {
			a.pages[a.currentPage], (*cmd) = a.pages[a.currentPage].Update((*msg))
			return (*a), (*cmd), true, true
		}
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func routeRemotePasteKey(a *appModel, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if msg.Type == tea.KeyCtrlV && clipboard.Remote() {
		terminal := false
		if p, ok := a.pages[a.currentPage].(interface{ TerminalFocused() bool }); ok {
			terminal = p.TerminalFocused()
		}
		_, modal := a.mouseModal()
		if !terminal || modal || a.showProviderDialog || a.showIntegrationDialog || a.showModelDialog || a.showSessionDialog || a.showPermissions || a.showHelp {
			return (*a), util.ReportInfo(config.Text("Use a colagem do terminal (por exemplo Ctrl+Shift+V). O clipboard do servidor SSH não será lido.", "Use terminal paste (for example Ctrl+Shift+V). The SSH server clipboard will not be read.")), true, true
		}
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func routeSessionOrModelKey(a *appModel, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if msg.String() != "ctrl+c" && !a.showPermissions && !a.showQuit && !a.showHelp {
		if a.showSessionDialog {
			updated, c := a.sessionDialog.Update((*msg))
			a.sessionDialog = updated.(dialog.SessionDialog)
			return (*a), c, true, true
		}
		if a.showModelDialog && msg.String() != config.Shortcut("models") {
			updated, c := a.modelDialog.Update((*msg))
			a.modelDialog = updated.(dialog.ModelDialog)
			return (*a), c, true, true
		}
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func routeFocusedHelpKey(a *appModel, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if a.showHelp && msg.String() != "ctrl+c" && msg.String() != "esc" && !key.Matches((*msg), helpEsc) && !key.Matches((*msg), keys.Help) {
		updated, c := a.help.Update((*msg))
		a.help = updated.(dialog.HelpCmp)
		return (*a), c, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func routeQuitOrPermissionKey(a *appModel, msg *tea.KeyMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if msg.String() != "ctrl+c" {
		if a.showQuit {
			updated, c := a.quit.Update((*msg))
			a.quit = updated.(dialog.QuitDialog)
			return (*a), c, true, true
		}
		if a.showPermissions {
			updated, c := a.permissions.Update((*msg))
			a.permissions = updated.(dialog.PermissionDialogCmp)
			return (*a), c, true, true
		}
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func updateAppModelMouseMsgMouseModal(a *appModel, msg *tea.MouseMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if m, ok := a.mouseModal(); ok && m != nil {
		if a.pages[a.currentPage] == nil || a.status == nil {
			return (*a), nil, true, true
		}
		x, y := a.dialogOrigin(m.View())
		msg.X -= x
		msg.Y -= y
		if msg.X < 0 || msg.Y < 0 || msg.X >= lipgloss.Width(m.View()) || msg.Y >= lipgloss.Height(m.View()) {
			return (*a), nil, true, true
		}
		updated, c := m.Update((*msg))
		switch {
		case a.showQuit:
			a.quit = updated.(dialog.QuitDialog)
		case a.showCommandDialog:
			a.commandDialog = updated.(dialog.CommandDialog)
		case a.showThemeDialog:
			a.themeDialog = updated.(dialog.ThemeDialog)
		case a.showFilepicker:
			a.filepicker = updated.(dialog.FilepickerCmp)
		case a.showMultiArgumentsDialog:
			a.multiArgumentsDialog = updated.(dialog.MultiArgumentsDialogCmp)
		case a.showInitDialog:
			a.initDialog = updated.(dialog.InitDialogCmp)
		}
		return (*a), c, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func newInitial(app *app.App, initial []session.Session, model *appModel) {
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
}

func viewShowHelp(a *appModel, appView *string) {
	if a.showHelp {
		bindings := layout.KeyMapToSlice(configuredKeys())
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
		col, row := a.dialogOrigin(overlay)
		(*appView) = layout.PlaceOverlay(
			col,
			row,
			overlay,
			(*appView),
			true,
		)
	}
}

func finishAgentEvent(a *appModel, payload *agent.AgentEvent) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if payload.Done && payload.Type == agent.AgentEventTypeSummarize {
		a.isCompacting = false
		return (*a), util.ReportInfo(config.Text("Resumo da conversa concluído", "Session summarization complete")), true, true
	} else if payload.Done && payload.Type == agent.AgentEventTypeResponse && a.selectedSession.ID != "" {
		model := a.app.CoderAgent.Model()
		contextWindow := model.ContextWindow
		tokens := a.selectedSession.CompletionTokens + a.selectedSession.PromptTokens
		if (tokens >= int64(float64(contextWindow)*0.95)) && config.Get().AutoCompact {
			return (*a), util.CmdHandler(startCompactSessionMsg{}), true, true
		}
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func updateAppModelMouseMsgShowPermissionsShowHelpShowQuit(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if a.showPermissions || a.showHelp || a.showQuit ||
		a.showCommandDialog || a.showInitDialog ||
		a.showFilepicker || a.showThemeDialog || a.showMultiArgumentsDialog || a.isCompacting {
		return (*a), nil, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func newCommandsBuiltins(model *appModel) {
	for _, entry := range commands.Builtins {
		id := strings.TrimSpace(entry.ID)
		if id == "mode" || id == "plan" || id == "resume" || id == "compact" || id == "skill" {
			continue
		}
		model.RegisterCommand(dialog.Command{ID: id, Title: entry.Title, Description: entry.Description, Handler: func(dialog.Command) tea.Cmd { return util.CmdHandler(page.WorkspaceCommandMsg{Command: id}) }})
	}
}

func updateAppModelKeyMsgMatchesFilepickerIsCWDFocused(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, extractedResult3 bool, handled bool) {
	if !a.filepicker.IsCWDFocused() {
		if a.showQuit {
			a.showQuit = !a.showQuit
			return (*a), nil, true, true, true
		}
		if a.showHelp {
			a.showHelp = !a.showHelp
			return (*a), nil, true, true, true
		}
		if a.showInitDialog {
			a.showInitDialog = false
			// Mark the project as initialized without running the command
			if err := config.MarkProjectInitialized(); err != nil {
				return (*a), util.ReportError(err), true, true, true
			}
			return (*a), nil, true, true, true
		}
		if a.showFilepicker {
			a.showFilepicker = false
			a.filepicker.ToggleFilepicker(a.showFilepicker)
			return (*a), nil, true, true, true
		}
		if a.currentPage == page.LogsPage {
			return (*a), a.moveToPage(page.ChatPage), true, true, true
		}
	}
	return extractedResult0, extractedResult1, extractedResult2, extractedResult3, false
}

func updateAppModelMouseMsgButtonTeaMouseButtonLeft(a *appModel, msg *tea.MouseMsg) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if a.currentPage == page.ChatPage && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft &&
		msg.Y == lipgloss.Height(a.pages[a.currentPage].View()) {
		cmd := a.footerClick(msg.X)
		return (*a), cmd, true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}

func routeSessionCaseChatSessionClearedMsg(a *appModel) (extractedResult0 tea.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if current, ok := a.pages[page.ChatPage].(interface{ PrepareSessionDraft(string) error }); ok {
		if err := current.PrepareSessionDraft(""); err != nil {
			return *a, util.ReportError(err), true, true
		}
	}
	if a.selectedSession.ID != "" && a.app.Permissions != nil {
		a.app.Permissions.RevokeSession(a.selectedSession.ID)
	}
	a.selectedSession = session.Session{}
	a.sessionDialog.SetSelectedSession("")
	return extractedResult0, extractedResult1, extractedResult2, false
}
