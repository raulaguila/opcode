package page

import (
	"context"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/completions"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/history"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/raulaguila/opcode/internal/session"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/util"
)

var ChatPage PageID = "chat"

type chatPage struct {
	inputEnabled         bool
	app                  *app.App
	editor               layout.Container
	messages             layout.Container
	layout               layout.SplitPaneLayout
	session              session.Session
	completionDialog     dialog.CompletionDialog
	showCompletionDialog bool
	terminal             terminalPanel
	width, height        int
	workflow             workflowPanel
	composer             chat.SlashCompleter
	transcript           chat.TranscriptControl
	workspace            workspacePanel
	recovery             recoveryState
	lastRequest          chat.SendMsg
	sidebar              layout.Container
	sidebarVisible       bool
	progress             string
	outcome              string
}

type ChatKeyMap struct {
	ShowCompletionDialog key.Binding
	NewSession           key.Binding
	Cancel               key.Binding
	Terminal             key.Binding
}

var keyMap = ChatKeyMap{
	Terminal: key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "terminal / chat")),
	ShowCompletionDialog: key.NewBinding(
		key.WithKeys("@"),
		key.WithHelp("@", "Complete"),
	),
	NewSession: key.NewBinding(
		key.WithKeys("ctrl+n"),
		key.WithHelp("ctrl+n", "new session"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
}

func (p *chatPage) Init() tea.Cmd {
	cmds := []tea.Cmd{
		p.layout.Init(),
		p.completionDialog.Init(),
		terminalTick(),
	}
	return tea.Batch(cmds...)
}

func (p *chatPage) Update(msg tea.Msg) (updated tea.Model, result tea.Cmd) {
	defer func() {
		if focus := p.syncInputFocus(); focus != nil {
			result = tea.Batch(result, focus)
		}
	}()
	var cmds []tea.Cmd
	if !p.sidebarVisible && p.sidebar != nil {
		switch msg.(type) {
		case pubsub.Event[history.File], pubsub.Event[session.Session], chat.SessionSelectedMsg:
			updated, c := p.sidebar.Update(msg)
			p.sidebar = updated.(layout.Container)
			cmds = append(cmds, c)
		}
	}
	switch msg := msg.(type) {
	case WorkspaceCommandMsg:
		return p, p.workspaceCommand(msg.Command, msg.Value)
	case GitWorkspaceMsg:
		return p, p.gitWorkspaceResult(msg)
	case WorkspaceResultMsg:
		return p, p.workspaceResult(msg)
	case chat.ResponseActionsMsg:
		return p, p.openResponseActions(msg.Text)
	case ActivityMsg:
		if msg.SessionID == p.session.ID {
			p.progress = msg.State
		}
		return p, nil
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == p.session.ID {
			p.session = msg.Payload
		}
	case chat.ProcessPreferencesChangedMsg:
		return p, tea.Batch(p.syncSidebar(), p.resizeContent())
	case AgentOutcomeMsg:
		if msg.SessionID == p.session.ID {
			p.outcome = msg.Outcome
			p.progress = ""
			if msg.Error != nil {
				p.recovery = recoveryState{err: msg.Error}
			} else if msg.Done && p.recovery.err != nil {
				p.recovery.resolved = true
			}
		}
		return p, p.resizeContent()
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		cmd := tea.Batch(p.syncSidebar(), p.resizeContent())
		return p, cmd
	case TerminalTickMsg:
		p.refreshWorkflow()
		p.refreshTerminal()
		return p, tea.Batch(p.resizeContent(), terminalTick())
	case TerminalStartedMsg:
		if msg.sessionID != p.session.ID {
			return p, nil
		}
		p.terminal.starting = false
		p.terminal.startCancel = nil
		if msg.err != nil {
			return p, util.ReportError(msg.err)
		}
		p.terminal.selected = msg.id
		p.refreshTerminal()
		return p, p.resizeContent()
	case tea.MouseMsg:
		if p.WorkspaceFocused() {
			return p, p.workspaceMouse(msg)
		}
		if p.slashMenuOpen() {
			return p, nil
		}
		if p.terminal.focused {
			return p, nil
		}
		if p.height >= 12 {
			if msg.Y == 0 {
				return p, nil
			}
			msg.Y--
		}
		// Mouse coordinates are relative to the terminal. The message panel
		// starts at the top left; don't scroll it over the editor or sidebar.
		panelStart := 0
		if p.layout != nil && p.editor != nil {
			_, layoutHeight := p.layout.GetSize()
			_, editorHeight := p.editor.GetSize()
			panelStart = layoutHeight - editorHeight
		}
		if msg.Y >= panelStart && msg.Y < panelStart+p.composerPanelHeight() {
			if msg.Y >= panelStart+p.workflowHeight() {
				return p, p.recoveryMouse(msg)
			}
			if p.workflow.expanded && p.workflow.question.ID == "" {
				if msg.Button == tea.MouseButtonWheelUp {
					p.workflow.offset = max(0, p.planOffset()-1)
				}
				if msg.Button == tea.MouseButtonWheelDown {
					p.workflow.offset = min(max(0, len(p.planRows())-1), p.planOffset()+1)
				}
			}
			return p, nil
		}
		width, height := p.messages.GetSize()
		if !p.showCompletionDialog && msg.X >= 0 && msg.X < width && msg.Y >= 0 && msg.Y < height {
			updated, cmd := p.messages.Update(msg)
			p.messages = updated.(layout.Container)
			return p, cmd
		}
		return p, nil
	case dialog.CompletionDialogCloseMsg:
		p.showCompletionDialog = false
	case WorkflowCommandMsg:
		if p.session.ID == "" {
			session, err := p.app.Sessions.Create(context.Background(), "New Session")
			if err != nil {
				return p, util.ReportError(err)
			}
			p.session = session
			return p, tea.Batch(p.setSidebar(), util.CmdHandler(chat.SessionSelectedMsg(session)), p.workflowCommand(msg.Command, msg.Value))
		}
		return p, p.workflowCommand(msg.Command, msg.Value)
	case chat.SendMsg:
		cmd := p.sendMessage(msg.Text, msg.Attachments)
		if cmd != nil {
			return p, cmd
		}
	case dialog.CommandRunCustomMsg:
		// Check if the agent is busy before executing custom commands
		if p.app.CoderAgent.IsBusy() {
			return p, util.ReportWarn("Agent is busy, please wait before executing a command...")
		}

		// Process the command content with arguments if any
		content := msg.Content
		if msg.Args != nil {
			// Replace all named arguments with their values
			for name, value := range msg.Args {
				placeholder := "$" + name
				content = strings.ReplaceAll(content, placeholder, value)
			}
		}

		// Handle custom command execution
		cmd := p.sendMessage(content, nil)
		if cmd != nil {
			return p, cmd
		}
	case chat.SessionClearedMsg:
		if err := p.PrepareSessionDraft(""); err != nil {
			return p, util.ReportError(err)
		}
		p.workflow = workflowPanel{}
		p.closeWorkspace()
		p.recovery = recoveryState{}
		p.lastRequest = chat.SendMsg{}
		p.progress = ""
		p.outcome = ""
		p.resetTerminal()
		p.session = session.Session{}
		cmds = append(cmds, p.syncSidebar())
	case chat.SessionSelectedMsg:
		if msg.ID != p.session.ID {
			if err := p.PrepareSessionDraft(msg.ID); err != nil {
				return p, util.ReportError(err)
			}
			p.workflow = workflowPanel{}
			p.closeWorkspace()
			p.recovery = recoveryState{}
			p.lastRequest = chat.SendMsg{}
			p.progress = ""
			p.outcome = ""
			p.resetTerminal()
		}
		if p.session.ID == "" {
			cmd := p.setSidebar()
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		p.session = msg
		cmds = append(cmds, p.syncSidebar())
		p.refreshWorkflow()
	case tea.KeyMsg:
		if p.WorkspaceFocused() {
			return p, p.workspaceKey(msg)
		}
		if p.workflow.question.ID != "" {
			return p, p.questionKey(msg)
		}
		if p.slashMenuOpen() {
			switch msg.String() {
			case "esc", "up", "down", "tab", "enter":
				updated, cmd := p.editor.Update(msg)
				p.editor = updated.(layout.Container)
				return p, cmd
			}
		}
		if p.workflow.expanded && (msg.String() == "alt+up" || msg.String() == "alt+down") {
			step := 1
			if msg.String() == "alt+up" {
				step = -1
			}
			p.workflow.offset = max(0, p.planOffset()+step)
			return p, nil
		}
		if msg.String() == config.Shortcut("sidebar") {
			if config.Get() != nil {
				next := "hidden"
				if !p.sidebarVisible {
					next = "shown"
				}
				if err := config.SaveUserSetting("tui.sidebar", next, false); err != nil {
					return p, util.ReportError(err)
				}
			}
			return p, tea.Batch(p.syncSidebar(), p.resizeContent())
		}
		if msg.String() == config.Shortcut("terminal") {
			return p, p.toggleTerminal()
		}
		if p.terminal.focused {
			return p, p.terminalKey(msg)
		}
		switch {
		case key.Matches(msg, keyMap.ShowCompletionDialog):
			p.showCompletionDialog = true
			// Continue sending keys to layout->chat
		case key.Matches(msg, keyMap.NewSession):
			return p, p.newConversation()
		case key.Matches(msg, keyMap.Cancel):
			if p.session.ID != "" {
				// Cancel the current session's generation process
				// This allows users to interrupt long-running operations
				p.app.CoderAgent.Cancel(p.session.ID)
				return p, nil
			}
		}
	}
	if p.showCompletionDialog {
		context, contextCmd := p.completionDialog.Update(msg)
		p.completionDialog = context.(dialog.CompletionDialog)
		cmds = append(cmds, contextCmd)

		// Doesn't forward event if enter key is pressed
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if keyMsg.String() == "enter" {
				return p, tea.Batch(cmds...)
			}
		}
	}

	u, cmd := p.layout.Update(msg)
	cmds = append(cmds, cmd)
	p.layout = u.(layout.SplitPaneLayout)

	cmds = append(cmds, p.resizeContent())
	return p, tea.Batch(cmds...)
}

func (p *chatPage) setSidebar() tea.Cmd { return p.syncSidebar() }

func (p *chatPage) newConversation() tea.Cmd {
	if err := p.PrepareSessionDraft(""); err != nil {
		return util.ReportError(err)
	}
	p.resetTerminal()
	p.session = session.Session{}
	p.workflow = workflowPanel{}
	p.progress, p.outcome = "", ""
	p.recovery = recoveryState{}
	return tea.Batch(p.clearSidebar(), util.CmdHandler(chat.SessionClearedMsg{}))
}
func (p *chatPage) syncSidebar() tea.Cmd {
	visible := p.session.ID != "" && config.Get() != nil && config.Get().TUI.SidebarVisible(p.width)
	if !visible {
		p.sidebarVisible = false
		return p.layout.ClearRightPanel()
	}
	var init tea.Cmd
	if p.sidebar == nil {
		p.sidebar = layout.NewContainer(chat.NewSidebarCmp(p.session, p.app.History), layout.WithPadding(1, 1, 1, 1))
		init = p.sidebar.Init()
	}
	p.sidebarVisible = true
	return tea.Batch(p.layout.SetRightPanel(p.sidebar), init)
}
func (p *chatPage) clearSidebar() tea.Cmd {
	p.sidebarVisible = false
	return p.layout.ClearRightPanel()
}

func (p *chatPage) sendMessage(text string, attachments []message.Attachment) tea.Cmd {
	if cmd, handled := p.handleWorkspaceMessage(text, attachments); handled {
		return cmd
	}
	var cmds []tea.Cmd
	failure := func(err error) tea.Cmd {
		return tea.Batch(util.ReportError(err), util.CmdHandler(chat.SubmissionResultMsg{Text: text, Error: err}))
	}
	if p.session.ID == "" {
		session, err := p.app.Sessions.Create(context.Background(), "New Session")
		if err != nil {
			return failure(err)
		}

		p.session = session
		cmd := p.setSidebar()
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, util.CmdHandler(chat.SessionSelectedMsg(session)))
	}

	if cmd, handled := p.handleWorkflowMessage(text); handled {
		return tea.Batch(append(cmds, cmd)...)
	}
	return tea.Batch(append(cmds, p.submitAgentMessage(text, attachments, text))...)
}
func (p *chatPage) submitAgentMessage(text string, attachments []message.Attachment, draft string) tea.Cmd {
	if p.app.CoderAgent == nil {
		return tea.Batch(util.ReportWarn("Configure um provedor em /settings"), util.CmdHandler(chat.SubmissionResultMsg{Text: draft, Error: fmt.Errorf("provedor não configurado")}))
	}
	_, queued, err := p.app.CoderAgent.Submit(context.Background(), p.session.ID, text, attachments...)
	if err != nil {
		return tea.Batch(util.ReportError(err), util.CmdHandler(chat.SubmissionResultMsg{Text: draft, Error: err}))
	}
	p.outcome = ""
	p.progress = config.Text("Investigando", "Investigating")
	p.lastRequest = chat.SendMsg{Text: text, Attachments: cloneUserAttachments(attachments)}
	cmds := []tea.Cmd{util.CmdHandler(chat.SubmissionResultMsg{Text: draft})}
	if queued {
		cmds = append(cmds, util.ReportInfo("Instrução pendente; será incorporada após a operação atual"))
	}
	return tea.Batch(cmds...)
}

func (p *chatPage) SetSize(width, height int) tea.Cmd {
	p.width, p.height = width, height
	return tea.Batch(p.syncSidebar(), p.resizeContent())
}

func (p *chatPage) GetSize() (int, int) {
	return p.width, p.height
}

func (p *chatPage) View() string {
	if p.WorkspaceFocused() {
		return p.workspaceView()
	}

	if p.terminal.focused {
		return p.terminalView(true)
	}
	layoutView := p.insertComposerPanel(p.layout.View())
	if p.terminalVisible() {
		layoutView = lipgloss.JoinVertical(lipgloss.Left, layoutView, p.terminalView(false))
	}

	if p.showCompletionDialog {
		_, layoutHeight := p.layout.GetSize()
		editorWidth, editorHeight := p.editor.GetSize()

		p.completionDialog.SetWidth(editorWidth)
		overlay := p.completionDialog.View()

		layoutView = layout.PlaceOverlay(
			0,
			layoutHeight-editorHeight+p.composerPanelHeight()-lipgloss.Height(overlay),
			overlay,
			layoutView,
			false,
		)
	}
	if p.slashMenuOpen() {
		_, layoutHeight := p.layout.GetSize()
		editorWidth, editorHeight := p.editor.GetSize()
		menu := p.composer.SlashMenuView(editorWidth, max(1, layoutHeight-editorHeight+p.composerPanelHeight()))
		if menu != "" {
			layoutView = layout.PlaceOverlay(0, max(0, layoutHeight-editorHeight+p.composerPanelHeight()-lipgloss.Height(menu)), menu, layoutView, false)
		}
	}

	if p.height >= 12 {
		project := "Opcode"
		if config.Get() != nil {
			project += " · " + filepath.Base(config.WorkingDirectory())
		}
		layoutView = lipgloss.JoinVertical(lipgloss.Left, ansi.Truncate(project, max(1, p.width), "…"), layoutView)
	}
	return layoutView
}

func (p *chatPage) BindingKeys() []key.Binding {
	localKeys := keyMap
	localKeys.Terminal = key.NewBinding(key.WithKeys(config.Shortcut("terminal")), key.WithHelp(config.Shortcut("terminal"), config.Text("terminal / conversa", "terminal / chat")))
	bindings := layout.KeyMapToSlice(localKeys)
	bindings = append(bindings, p.messages.BindingKeys()...)
	bindings = append(bindings, p.editor.BindingKeys()...)
	return bindings
}

func NewChatPage(app *app.App) tea.Model {
	cg := completions.NewFileAndFolderContextGroup()
	completionDialog := dialog.NewCompletionDialogCmp(cg)

	transcript := chat.NewMessagesCmp(app)
	messagesContainer := layout.NewContainer(
		transcript,
		layout.WithPadding(1, 1, 0, 1),
	)
	composer := chat.NewEditorCmp(app)
	editorContainer := layout.NewContainer(
		composer,
		layout.WithBorder(true, false, false, false),
	)
	return &chatPage{
		inputEnabled:     true,
		app:              app,
		composer:         composer.(chat.SlashCompleter),
		transcript:       transcript.(chat.TranscriptControl),
		editor:           editorContainer,
		messages:         messagesContainer,
		completionDialog: completionDialog,
		layout: layout.NewSplitPane(
			layout.WithLeftPanel(messagesContainer),
			layout.WithBottomPanel(editorContainer),
		),
	}
}

func (p *chatPage) slashMenuOpen() bool {
	return p.composer != nil && !p.terminal.focused && p.workflow.question.ID == "" && !p.showCompletionDialog && p.composer.SlashMenuOpen()
}
