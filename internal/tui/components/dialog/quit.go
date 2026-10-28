package dialog

import (
	"github.com/raulaguila/opcode/internal/config"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

const question = "Are you sure you want to quit?"

type CloseQuitMsg struct{}
type ConfirmQuitMsg struct{}

type QuitDialog interface {
	tea.Model
	layout.Bindings
}

type quitDialogCmp struct {
	selectedNo bool
}

type helpMapping struct {
	LeftRight  key.Binding
	EnterSpace key.Binding
	Yes        key.Binding
	No         key.Binding
	Tab        key.Binding
}

var helpKeys = helpMapping{
	LeftRight: key.NewBinding(
		key.WithKeys("left", "right"),
		key.WithHelp("←/→", "switch options"),
	),
	EnterSpace: key.NewBinding(
		key.WithKeys("enter", " "),
		key.WithHelp("enter/space", "confirm"),
	),
	Yes: key.NewBinding(
		key.WithKeys("y", "Y"),
		key.WithHelp("y/Y", "yes"),
	),
	No: key.NewBinding(
		key.WithKeys("n", "N"),
		key.WithHelp("n/N", "no"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch options"),
	),
}

func (q *quitDialogCmp) Init() tea.Cmd {
	return nil
}

func (q *quitDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, helpKeys.LeftRight) || key.Matches(msg, helpKeys.Tab):
			q.selectedNo = !q.selectedNo
			return q, nil
		case key.Matches(msg, helpKeys.EnterSpace):
			if !q.selectedNo {
				return q, util.CmdHandler(ConfirmQuitMsg{})
			}
			return q, util.CmdHandler(CloseQuitMsg{})
		case key.Matches(msg, helpKeys.Yes):
			return q, util.CmdHandler(ConfirmQuitMsg{})
		case key.Matches(msg, helpKeys.No):
			return q, util.CmdHandler(CloseQuitMsg{})
		}
	}
	return q, nil
}

func (q *quitDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	yesStyle := baseStyle
	noStyle := baseStyle
	spacerStyle := baseStyle.Background(t.Background())

	if q.selectedNo {
		noStyle = noStyle.Background(t.Primary()).Foreground(t.Background())
		yesStyle = yesStyle.Background(t.Background()).Foreground(t.Primary())
	} else {
		yesStyle = yesStyle.Background(t.Primary()).Foreground(t.Background())
		noStyle = noStyle.Background(t.Background()).Foreground(t.Primary())
	}

	yesButton := yesStyle.Padding(0, 1).Render(config.Text("Sim", "Yes"))
	noButton := noStyle.Padding(0, 1).Render(config.Text("Não", "No"))

	buttons := lipgloss.JoinHorizontal(lipgloss.Left, yesButton, spacerStyle.Render("  "), noButton)

	question := config.Text("Deseja sair?", question)
	width := lipgloss.Width(question)
	remainingWidth := width - lipgloss.Width(buttons)
	if remainingWidth > 0 {
		buttons = spacerStyle.Render(strings.Repeat(" ", remainingWidth)) + buttons
	}

	content := baseStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Center,
			question,
			"",
			buttons,
		),
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func (q *quitDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(helpKeys)
}

func NewQuitCmp() QuitDialog {
	return &quitDialogCmp{
		selectedNo: true,
	}
}
