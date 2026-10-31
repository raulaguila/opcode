package dialog

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	baseStyle := dialogBody()

	descStyle := baseStyle.Width(width).Foreground(t.TextMuted())
	itemStyle := baseStyle.Width(width).
		Foreground(t.Text()).
		Background(t.Background())

	if selected {
		itemStyle = itemStyle.
			Background(t.BorderDim()).
			Foreground(t.Text()).
			Bold(true)
		descStyle = descStyle.
			Background(t.BorderDim()).
			Foreground(t.Text())
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
	util.Model
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

func (c *commandDialogCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if result0, result1, handled := updateCaseTeaKeyMsgCase(c, &msg); handled {
			return result0, result1
		}
	case tea.MouseMsg:
		return updateCommandDialogCmpMouseMsg(c, msg)
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

type commandGeometry struct {
	width, start, count, firstRow, padding int
	details                                []string
}

func (c *commandDialogCmp) geometry() commandGeometry {
	g := commandGeometry{width: 64, padding: 1}
	if c.width > 0 {
		g.width = max(8, min(g.width, c.width-8))
	}
	headers := 3
	if c.activeID != "" {
		headers = 1
	}
	if c.height > 0 && c.height < 10 {
		g.padding = 0
		if headers == 3 {
			headers = 2
		}
	}
	g.firstRow = 1 + g.padding + headers
	g.details = c.detailLines(g.width)
	available := 8 + len(g.details)
	if c.height > 0 {
		available = max(1, c.height-(2+2*g.padding)-headers-2)
	}
	g.details = g.details[:min(len(g.details), max(0, available-1))]
	g.count = max(1, min(8, available-len(g.details)))
	_, selected := c.listView.GetSelectedItem()
	g.start = max(0, selected-g.count+1)
	return g
}

func (c *commandDialogCmp) detailLines(width int) []string {
	item, index := c.listView.GetSelectedItem()
	if index < 0 {
		return nil
	}
	_, description := commands.Presentation(item.ID, item.Title, item.Description)
	if c.activeID != "" {
		description = modeConsequence(item.ID)
	} else {
		description = "/" + item.ID + " · " + description
	}
	if c.activeID == "" {
		return []string{ansi.Truncate(description, width, "…")}
	}
	return strings.Split(ansi.Wrap(description, width, ""), "\n")
}

func modeConsequence(id string) string {
	switch id {
	case "mode-ask":
		return config.Text("Lê e explica o projeto, sem alterar arquivos.", "Reads and explains the project without changing files.")
	case "mode-plan":
		return config.Text("Prepara um plano, sem comandos ou edições.", "Prepares a plan without commands or edits.")
	case "mode-execute":
		return config.Text("Permite alterações e solicita as permissões necessárias.", "Allows changes and requests the required permissions.")
	}
	return ""
}

func (c *commandDialogCmp) View() string {
	g := c.geometry()
	base := dialogBody()
	c.search.SetWidth(g.width - 2)
	rows := []string{styles.PanelTitle().Width(g.width).Render(c.dialogTitle())}
	if c.activeID == "" {
		rows = append(rows, base.Width(g.width).Render(c.search.View()))
		if g.padding > 0 {
			rows = append(rows, "")
		}
	}
	items := c.listView.GetItems()
	_, selected := c.listView.GetSelectedItem()
	for i := g.start; i < min(len(items), g.start+g.count); i++ {
		rows = append(rows, c.commandRow(items[i], i == selected, g.width))
	}
	if len(items) == 0 {
		rows = append(rows, config.Text("Nenhum comando encontrado", "No commands found"))
	}
	for _, detail := range g.details {
		rows = append(rows, base.Foreground(theme.CurrentTheme().TextMuted()).Render(detail))
	}
	hint := config.Text("↑↓ escolher · Enter abrir · Esc voltar", "↑↓ select · Enter open · Esc back")
	if c.activeID != "" {
		hint = config.Text("↑↓ escolher · Enter aplicar modo · Esc voltar", "↑↓ select · Enter apply mode · Esc back")
	}
	rows = append(rows, styles.PanelDivider(g.width), ansi.Truncate(hint, g.width, "…"))
	return styles.Panel().Padding(g.padding, 2).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (c *commandDialogCmp) commandRow(item Command, selected bool, width int) string {
	name, _ := commands.Presentation(item.ID, item.Title, item.Description)
	if localized := modeActionTitle(item.ID); localized != "" {
		name = localized
	}
	if item.ID == c.activeID {
		name = "✓ " + name
	}
	mark := "  "
	style := dialogBody()
	if selected {
		mark = "› "
		style = styles.SelectedRow().Bold(true)
	}
	if c.activeID != "" || width < 48 {
		return style.Width(width).Render(ansi.Truncate(mark+name, width, "…"))
	}
	category := commands.Category(item.ID)
	titleWidth := max(1, width-ansi.StringWidth(category)-3)
	title := ansi.Truncate(mark+name, titleWidth, "…")
	title += strings.Repeat(" ", max(0, width-ansi.StringWidth(title)-ansi.StringWidth(category)))
	quiet := style.Foreground(theme.CurrentTheme().TextMuted()).Bold(false).Render(category)
	return style.Width(width).Render(title + quiet)
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
			for range i {
				c.listView.Update(tea.KeyPressMsg{Code: tea.KeyDown})
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
	search := newDialogInput()
	search.Prompt = "/ "
	search.Placeholder = config.Text("Buscar comando", "Search commands")
	search.CharLimit = 128
	return &commandDialogCmp{
		listView: listView,
		search:   search,
	}
}

func updateCommandDialogCmpMouseMsg(c *commandDialogCmp, msg tea.MouseMsg) (util.Model, tea.Cmd) {
	if !leftClick(msg) {
		return c, nil
	}
	g := c.geometry()
	row := msg.Mouse().Y - g.firstRow
	items := c.listView.GetItems()
	if msg.Mouse().X >= 3 && msg.Mouse().X < 3+g.width && row >= 0 && row < g.count && g.start+row < len(items) {
		return c, util.CmdHandler(CommandSelectedMsg{Command: items[g.start+row]})
	}
	return c, nil
}

func updateCaseTeaKeyMsgCase(c *commandDialogCmp, msg *tea.KeyPressMsg) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	switch {
	case key.Matches((*msg), commandKeys.Enter):
		selectedItem, idx := c.listView.GetSelectedItem()
		if idx != -1 {
			return c, util.CmdHandler(CommandSelectedMsg{
				Command: selectedItem,
			}), true
		}
	case key.Matches((*msg), commandKeys.Escape):
		return c, util.CmdHandler(CloseCommandDialogMsg{}), true
	}
	if c.activeID != "" && msg.String() != "up" && msg.String() != "down" {
		return c, nil, true
	}
	if msg.String() != "up" && msg.String() != "down" {
		var cmd tea.Cmd
		c.search, cmd = c.search.Update((*msg))
		c.filter()
		return c, cmd, true
	}
	return extractedResult0, extractedResult1, false
}

func modeActionTitle(id string) string {
	switch id {
	case "mode-ask":
		return config.Text("Perguntar", "Ask")
	case "mode-plan":
		return config.Text("Planejar", "Plan")
	case "mode-execute":
		return config.Text("Executar", "Execute")
	}
	return ""
}
