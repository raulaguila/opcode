package dialog

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/commands"
	utilComponents "github.com/raulaguila/opcode/internal/tui/components/util"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

// Command represents a command that can be executed
type Command struct {
	ID          string
	Title       string
	Description string
	Handler     func(cmd Command) tea.Cmd
}

func (ci Command) Render(selected bool, width int) string {
	ci.Title, ci.Description = commands.Presentation(ci.ID, ci.Title, ci.Description)
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	descStyle := baseStyle.Width(width).Foreground(t.TextMuted())
	itemStyle := baseStyle.Width(width).
		Foreground(t.Text()).
		Background(t.Background())

	if selected {
		itemStyle = itemStyle.
			Background(t.Primary()).
			Foreground(t.Background()).
			Bold(true)
		descStyle = descStyle.
			Background(t.Primary()).
			Foreground(t.Background())
	}

	title := itemStyle.Padding(0, 1).Render(ci.Title)
	if ci.Description != "" {
		description := descStyle.Padding(0, 1).Render(ci.Description)
		return lipgloss.JoinVertical(lipgloss.Left, title, description)
	}
	return title
}

// CommandSelectedMsg is sent when a command is selected
type CommandSelectedMsg struct {
	Command Command
}

// CloseCommandDialogMsg is sent when the command dialog is closed
type CloseCommandDialogMsg struct{}

// CommandDialog interface for the command selection dialog
type CommandDialog interface {
	tea.Model
	layout.Bindings
	SetCommands(commands []Command)
}

type commandDialogCmp struct {
	listView utilComponents.SimpleList[Command]
	commands []Command
	search   textinput.Model
	width    int
	height   int
	title    string
	activeID string
}

type commandKeyMap struct {
	Enter  key.Binding
	Escape key.Binding
}

var commandKeys = commandKeyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select command"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
}

func (c *commandDialogCmp) Init() tea.Cmd {
	return c.listView.Init()
}

func (c *commandDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, commandKeys.Enter):
			selectedItem, idx := c.listView.GetSelectedItem()
			if idx != -1 {
				return c, util.CmdHandler(CommandSelectedMsg{
					Command: selectedItem,
				})
			}
		case key.Matches(msg, commandKeys.Escape):
			return c, util.CmdHandler(CloseCommandDialogMsg{})
		}
		if c.activeID != "" && msg.String() != "up" && msg.String() != "down" {
			return c, nil
		}
		if msg.String() != "up" && msg.String() != "down" {
			var cmd tea.Cmd
			c.search, cmd = c.search.Update(msg)
			c.filter()
			return c, cmd
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			items := c.listView.GetItems()
			_, selected := c.listView.GetSelectedItem()
			count := 8
			if c.height > 0 {
				count = max(1, min(8, c.height-10))
			}
			start := max(0, selected-count+1)
			row := msg.Y - 5
			if c.activeID != "" {
				row = msg.Y - 3
			}
			if msg.X >= 3 && row >= 0 && row < count && start+row < len(items) {
				return c, util.CmdHandler(CommandSelectedMsg{Command: items[start+row]})
			}
		}
		return c, nil
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
	default:
		if c.activeID != "" {
			return c, nil
		}
		before := c.search.Value()
		var cmd tea.Cmd
		c.search, cmd = c.search.Update(msg)
		if before != c.search.Value() {
			c.filter()
		}
		return c, cmd
	}

	u, cmd := c.listView.Update(msg)
	c.listView = u.(utilComponents.SimpleList[Command])
	cmds = append(cmds, cmd)

	return c, tea.Batch(cmds...)
}

func (c *commandDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	maxWidth := 64
	if c.width > 0 {
		maxWidth = max(8, min(maxWidth, c.width-8))
	}
	c.search.Width = maxWidth - 2

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(maxWidth).
		Padding(0, 1).
		Render(c.dialogTitle())

	rows := []string{title, baseStyle.Width(maxWidth).Render(c.search.View()), ""}
	if c.activeID != "" {
		rows = []string{title}
	}
	items := c.listView.GetItems()
	_, selected := c.listView.GetSelectedItem()
	count := 8
	if c.height > 0 {
		count = max(1, min(count, c.height-10))
	}
	start := max(0, selected-count+1)
	for i := start; i < min(len(items), start+count); i++ {
		item := items[i]
		name, _ := commands.Presentation(item.ID, item.Title, item.Description)
		if c.activeID == "" && maxWidth >= 48 {
			name = commands.Category(item.ID) + " · " + name
		}
		if item.ID == c.activeID {
			name = "✓ " + name
		}
		style := baseStyle.Width(maxWidth)
		mark := "  "
		if i == selected {
			mark = "› "
			style = style.Foreground(t.Background()).Background(t.Primary()).Bold(true)
		}
		rows = append(rows, style.Render(ansi.Truncate(mark+name, maxWidth, "…")))
	}
	if len(items) == 0 {
		rows = append(rows, config.Text("Nenhum comando encontrado", "No commands found"))
	}
	if selected >= 0 && selected < len(items) && (c.height == 0 || c.height >= 12) {
		item := items[selected]
		_, description := commands.Presentation(item.ID, item.Title, item.Description)
		rows = append(rows, baseStyle.Foreground(t.TextMuted()).Render(ansi.Truncate("/"+item.ID+" · "+description, maxWidth, "…")))
	}
	hint := config.Text("↑↓ escolher · Enter abrir · Esc voltar", "↑↓ select · Enter open · Esc back")
	if c.activeID != "" {
		hint = config.Text("↑↓ escolher · Enter aplicar modo · Esc voltar", "↑↓ select · Enter apply mode · Esc back")
	}
	rows = append(rows, ansi.Truncate(hint, maxWidth, "…"))
	content := lipgloss.JoinVertical(lipgloss.Left, rows...)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func (c *commandDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(commandKeys)
}

func (c *commandDialogCmp) SetCommands(commands []Command) {
	c.title, c.activeID = "", ""
	c.commands = commands
	c.search.SetValue("")
	c.search.Focus()
	c.filter()
}

// SetContext distinguishes a focused action from the mode already in use.
func (c *commandDialogCmp) SetContext(title, activeID string) {
	c.title, c.activeID = title, activeID
	for i, item := range c.listView.GetItems() {
		if item.ID == activeID {
			for n := 0; n < i; n++ {
				c.listView.Update(tea.KeyMsg{Type: tea.KeyDown})
			}
			break
		}
	}
}

func (c *commandDialogCmp) dialogTitle() string {
	if c.title != "" {
		return c.title
	}
	return config.Text("Comandos", "Commands")
}

func (c *commandDialogCmp) filter() {
	query := strings.ToLower(strings.TrimSpace(c.search.Value()))
	var matches []Command
	for _, item := range c.commands {
		title, description := commands.Presentation(item.ID, item.Title, item.Description)
		if commands.Match(query, item.ID, title+" "+description) {
			matches = append(matches, item)
		}
	}
	slices.SortStableFunc(matches, func(a, b Command) int { return commands.Rank(query, a.ID) - commands.Rank(query, b.ID) })
	c.listView.SetItems(matches)
}

// NewCommandDialogCmp creates a new command selection dialog
func NewCommandDialogCmp() CommandDialog {
	listView := utilComponents.NewSimpleList[Command](
		[]Command{},
		10,
		"No commands available",
		false,
	)
	search := textinput.New()
	search.Prompt = "/ "
	search.Placeholder = config.Text("Buscar comando", "Search commands")
	search.CharLimit = 128
	return &commandDialogCmp{
		listView: listView,
		search:   search,
	}
}
