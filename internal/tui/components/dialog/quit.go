package dialog

import (
	"strings"

	"github.com/raulaguila/opcode/internal/config"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

const question = "Are you sure you want to quit?"

type CloseQuitMsg struct{}
type ConfirmQuitMsg struct{}
type OpenQuitMsg struct{}

type QuitDialog interface {
	util.Model
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
		key.WithKeys("enter", "space"),
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

func (q *quitDialogCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if clickedLabel(q.View(), config.Text("Sair", "Quit"), msg) {
			return q, util.CmdHandler(ConfirmQuitMsg{})
		}
		if clickedLabel(q.View(), config.Text("Continuar", "Continue"), msg) {
			return q, util.CmdHandler(CloseQuitMsg{})
		}
	case tea.KeyPressMsg:
		if msg.Mod & ^tea.ModShift != 0 {
			return q, nil
		}
		if msg.Code == tea.KeyEsc {
			return q, util.CmdHandler(CloseQuitMsg{})
		}
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
	baseStyle := dialogBody()

	yesStyle := baseStyle
	noStyle := baseStyle
	spacerStyle := baseStyle.Background(t.BackgroundSecondary())

	if q.selectedNo {
		noStyle = noStyle.Background(t.BorderDim()).Foreground(t.Text())
		yesStyle = yesStyle.Background(t.BackgroundSecondary()).Foreground(t.Primary())
	} else {
		yesStyle = yesStyle.Background(t.BorderDim()).Foreground(t.Text())
		noStyle = noStyle.Background(t.BackgroundSecondary()).Foreground(t.Primary())
	}

	yesButton := yesStyle.Padding(0, 1).Render(selectionMark(!q.selectedNo) + config.Text("Sair", "Quit"))
	noButton := noStyle.Padding(0, 1).Render(selectionMark(q.selectedNo) + config.Text("Continuar", "Continue"))

	buttons := lipgloss.JoinHorizontal(lipgloss.Left, yesButton, spacerStyle.Render("  "), noButton)

	question := config.Text("Deseja sair?", question)
	width := max(lipgloss.Width(question), lipgloss.Width(buttons))
	remainingWidth := width - lipgloss.Width(buttons)
	if remainingWidth > 0 {
		buttons = spacerStyle.Render(strings.Repeat(" ", remainingWidth)) + buttons
	}

	content := baseStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Center,
			question,
			styles.PanelDivider(width),
			buttons,
		),
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.NormalBorder()).
		BorderBackground(t.BackgroundSecondary()).
		BorderForeground(t.TextMuted()).
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
