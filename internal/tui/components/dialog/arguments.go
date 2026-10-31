package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"

	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type argumentsDialogKeyMap struct {
	Enter  key.Binding
	Escape key.Binding
}

// ShortHelp implements key.Map.
func (k argumentsDialogKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
		key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	}
}

// FullHelp implements key.Map.
func (k argumentsDialogKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

// ShowMultiArgumentsDialogMsg is a message that is sent to show the multi-arguments dialog.
type ShowMultiArgumentsDialogMsg struct {
	CommandID string
	Content   string
	ArgNames  []string
}

// CloseMultiArgumentsDialogMsg is a message that is sent when the multi-arguments dialog is closed.
type CloseMultiArgumentsDialogMsg struct {
	Submit    bool
	CommandID string
	Content   string
	Args      map[string]string
}

// MultiArgumentsDialogCmp is a component that asks the user for multiple command arguments.
type MultiArgumentsDialogCmp struct {
	width, height int
	inputs        []textinput.Model
	focusIndex    int
	keys          argumentsDialogKeyMap
	commandID     string
	content       string
	argNames      []string
}

// NewMultiArgumentsDialogCmp creates a new MultiArgumentsDialogCmp.
func NewMultiArgumentsDialogCmp(commandID, content string, argNames []string) MultiArgumentsDialogCmp {
	inputs := make([]textinput.Model, len(argNames))

	for i, name := range argNames {
		ti := newDialogInput()
		ti.Placeholder = fmt.Sprintf("Enter value for %s...", name)
		ti.SetWidth(40)
		ti.Prompt = ""

		// Only focus the first input initially
		if i == 0 {
			ti.Focus()
		} else {
			ti.Blur()
		}

		inputs[i] = ti
	}

	return MultiArgumentsDialogCmp{
		inputs:     inputs,
		keys:       argumentsDialogKeyMap{},
		commandID:  commandID,
		content:    content,
		argNames:   argNames,
		focusIndex: 0,
	}
}

// Init implements util.Model.
func (m MultiArgumentsDialogCmp) Init() tea.Cmd {
	// Make sure only the first input is focused
	for i := range m.inputs {
		if i == m.focusIndex {
			m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}

	return textinput.Blink
}

// Update implements util.Model.
func (m MultiArgumentsDialogCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if len(m.inputs) == 0 {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			return m, util.CmdHandler(CloseMultiArgumentsDialogMsg{
				Submit:    false,
				CommandID: m.commandID,
				Content:   m.content,
				Args:      nil,
			})
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			// If we're on the last input, submit the form
			if m.focusIndex == len(m.inputs)-1 {
				args := make(map[string]string)
				for i, name := range m.argNames {
					args[name] = m.inputs[i].Value()
				}
				return m, util.CmdHandler(CloseMultiArgumentsDialogMsg{
					Submit:    true,
					CommandID: m.commandID,
					Content:   m.content,
					Args:      args,
				})
			}
			// Otherwise, move to the next input
			m.inputs[m.focusIndex].Blur()
			m.focusIndex++
			m.inputs[m.focusIndex].Focus()
		case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
			// Move to the next input
			m.inputs[m.focusIndex].Blur()
			m.focusIndex = (m.focusIndex + 1) % len(m.inputs)
			m.inputs[m.focusIndex].Focus()
		case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab"))):
			// Move to the previous input
			m.inputs[m.focusIndex].Blur()
			m.focusIndex = (m.focusIndex - 1 + len(m.inputs)) % len(m.inputs)
			m.inputs[m.focusIndex].Focus()
		}
	case tea.MouseMsg:
		rows := max(1, (m.height-9)/2)
		start := max(0, m.focusIndex-rows+1)
		i := start + (msg.Mouse().Y-3)/2
		if leftClick(msg) && msg.Mouse().X >= 3 && msg.Mouse().Y >= 3 && msg.Mouse().Y < 3+rows*2 && i < len(m.inputs) {
			m.inputs[m.focusIndex].Blur()
			m.focusIndex = i
			return m, m.inputs[i].Focus()
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	// Update the focused input
	var cmd tea.Cmd
	m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View implements util.Model.
func (m MultiArgumentsDialogCmp) View() string {
	width := max(1, min(60, m.width-6))
	if m.width <= 0 {
		width = 60
	}
	height := m.height
	if height <= 0 {
		height = 24
	}
	rows := max(1, (height-9)/2)
	start := max(0, m.focusIndex-rows+1)
	lines := []string{config.Text("Argumentos do comando", "Command arguments")}
	for i := start; i < min(len(m.inputs), start+rows); i++ {
		mark := "  "
		if i == m.focusIndex {
			mark = "› "
		}
		m.inputs[i].SetWidth(max(1, width-2))
		lines = append(lines, mark+m.argNames[i], m.inputs[i].View())
	}
	lines = append(lines, styles.PanelDivider(width), config.Text("Tab campo · Enter próxima/envia · Esc cancela", "Tab field · Enter next/send · Esc cancel"))
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	return styles.Panel().Padding(1, 2).Border(lipgloss.NormalBorder()).Render(strings.Join(lines, "\n"))
}

// SetSize sets the size of the component.
func (m *MultiArgumentsDialogCmp) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Bindings implements layout.Bindings.
func (m MultiArgumentsDialogCmp) Bindings() []key.Binding {
	return m.keys.ShortHelp()
}
