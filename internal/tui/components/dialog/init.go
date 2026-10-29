package dialog

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/config"

	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

// InitDialogCmp is a component that asks the user if they want to initialize the project.
type InitDialogCmp struct {
	width, height int
	selected      int
	keys          initDialogKeyMap
}

// NewInitDialogCmp creates a new InitDialogCmp.
func NewInitDialogCmp() InitDialogCmp {
	return InitDialogCmp{
		selected: 0,
		keys:     initDialogKeyMap{},
	}
}

type initDialogKeyMap struct {
	Tab    key.Binding
	Left   key.Binding
	Right  key.Binding
	Enter  key.Binding
	Escape key.Binding
	Y      key.Binding
	N      key.Binding
}

// ShortHelp implements key.Map.
func (k initDialogKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys("tab", "left", "right"),
			key.WithHelp("tab/←/→", "toggle selection"),
		),
		key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
		key.NewBinding(
			key.WithKeys("esc", "q"),
			key.WithHelp("esc/q", "cancel"),
		),
		key.NewBinding(
			key.WithKeys("y", "n"),
			key.WithHelp("y/n", "yes/no"),
		),
	}
}

// FullHelp implements key.Map.
func (k initDialogKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

// Init implements tea.Model.
func (m InitDialogCmp) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m InitDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if clickedLabel(m.View(), config.Text("Criar Opcode.md", "Create Opcode.md"), msg) {
			return m, util.CmdHandler(CloseInitDialogMsg{Initialize: true})
		}
		if clickedLabel(m.View(), config.Text("Agora não", "Not now"), msg) {
			return m, util.CmdHandler(CloseInitDialogMsg{Initialize: false})
		}
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			return m, util.CmdHandler(CloseInitDialogMsg{Initialize: false})
		case key.Matches(msg, key.NewBinding(key.WithKeys("tab", "left", "right", "h", "l"))):
			m.selected = (m.selected + 1) % 2
			return m, nil
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			return m, util.CmdHandler(CloseInitDialogMsg{Initialize: m.selected == 0})
		case key.Matches(msg, key.NewBinding(key.WithKeys("y"))):
			return m, util.CmdHandler(CloseInitDialogMsg{Initialize: true})
		case key.Matches(msg, key.NewBinding(key.WithKeys("n"))):
			return m, util.CmdHandler(CloseInitDialogMsg{Initialize: false})
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

// View implements tea.Model.
func (m InitDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	// Calculate width needed for content
	maxWidth := max(1, min(60, m.width-10)) // Wrap inside the available frame.

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(maxWidth).
		Padding(0, 1).
		Render(config.Text("Inicializar projeto", "Initialize Project"))

	explanation := baseStyle.
		Foreground(t.Text()).
		Width(maxWidth).
		Padding(0, 1).
		Render(config.Text("A inicialização cria Opcode.md com informações do projeto. Você pode editar esse arquivo para orientar o agente.", "Initialization generates a new Opcode.md file that contains information about your codebase, this file serves as memory for each project, you can freely add to it to help the agents be better at their job."))

	question := baseStyle.
		Foreground(t.Text()).
		Width(maxWidth).
		Padding(1, 1).
		Render(config.Text("Deseja inicializar este projeto?", "Would you like to initialize this project?"))

	yesStyle := baseStyle
	noStyle := baseStyle

	if m.selected == 0 {
		yesStyle = yesStyle.
			Background(t.Primary()).
			Foreground(t.Background()).
			Bold(true)
		noStyle = noStyle.
			Background(t.Background()).
			Foreground(t.Primary())
	} else {
		noStyle = noStyle.
			Background(t.Primary()).
			Foreground(t.Background()).
			Bold(true)
		yesStyle = yesStyle.
			Background(t.Background()).
			Foreground(t.Primary())
	}

	yesMark, noMark := "  ", "  "
	if m.selected == 0 {
		yesMark = "› "
	} else {
		noMark = "› "
	}
	yes := yesStyle.Padding(0, 2).Render(yesMark + config.Text("Criar Opcode.md", "Create Opcode.md"))
	no := noStyle.Padding(0, 2).Render(noMark + config.Text("Agora não", "Not now"))

	buttons := lipgloss.JoinHorizontal(lipgloss.Center, yes, baseStyle.Render("  "), no)
	buttons = baseStyle.
		Width(maxWidth).
		Padding(1, 0).
		Render(buttons)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(maxWidth).Render(""),
		explanation,
		question,
		buttons,
		baseStyle.Width(maxWidth).Render(""),
	)
	if m.height > 0 && m.height < 24 {
		// Keep the decision and both actions visible above the footer in a short
		// terminal. The longer onboarding prose is unnecessary at this size.
		explanation = baseStyle.Foreground(t.Text()).Width(maxWidth).Padding(0, 1).
			Render(config.Text("Cria Opcode.md com orientações do projeto. Você pode editá-lo depois.", "Creates Opcode.md with project guidance. You can edit it later."))
		buttons = baseStyle.Width(maxWidth).Render(lipgloss.JoinHorizontal(lipgloss.Center, yes, baseStyle.Render("  "), no))
		content = lipgloss.JoinVertical(lipgloss.Left, title, "", explanation, "", buttons)
	}

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

// SetSize sets the size of the component.
func (m *InitDialogCmp) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Bindings implements layout.Bindings.
func (m InitDialogCmp) Bindings() []key.Binding {
	return m.keys.ShortHelp()
}

// CloseInitDialogMsg is a message that is sent when the init dialog is closed.
type CloseInitDialogMsg struct {
	Initialize bool
}

// ShowInitDialogMsg is a message that is sent to show the init dialog.
type ShowInitDialogMsg struct {
	Show bool
}
