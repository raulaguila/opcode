package dialog

import (
	"fmt"
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
	listView   utilComponents.SimpleList[Command]
	commands   []Command
	search     textinput.Model
	width      int
	height     int
	title      string
	activeID   string
	approvalID string
	// featured: the leading, hand-ordered commands shown first as a group.
	featured int
}

const featuredCommands = 6

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
	// The title and legend live in the frame's borders.
	headers := 2
	if c.activeID != "" {
		headers = 0
	}
	if c.height > 0 && c.height < 12 {
		g.padding = 0
		if headers == 2 {
			headers = 1
		}
	}
	g.firstRow = 1 + g.padding + headers
	g.details = c.detailLines(g.width)
	available := 8 + len(g.details)
	if c.height > 0 {
		available = max(1, c.height-(2+2*g.padding)-headers)
	}
	if c.approvalID != "" {
		available = max(1, available-1)
	}
	g.details = g.details[:min(len(g.details), max(0, available-1))]
	room := max(1, min(8, available-len(g.details)))
	_, selected := c.listView.GetSelectedItem()
	items := c.listView.GetItems()
	// Category headings take rows too: shrink the window until both fit.
	for g.count = room; ; g.count-- {
		g.start = max(0, selected-g.count+1)
		headings := 0
		for i := g.start; i < min(len(items), g.start+g.count); i++ {
			if c.categoryHeading(i, g.start, items) {
				headings++
			}
		}
		if g.count <= 1 || g.count+headings <= room {
			break
		}
	}
	return g
}

// categoryHeading groups the unfiltered palette by category, like a menu.
func (c *commandDialogCmp) categoryHeading(i, start int, items []Command) bool {
	if c.activeID != "" || strings.TrimSpace(c.search.Value()) != "" || i >= len(items) {
		return false
	}
	return i == start || c.commandGroup(i-1, items) != c.commandGroup(i, items)
}

func (c *commandDialogCmp) commandGroup(i int, items []Command) string {
	if i < c.featured {
		return config.Text("Principais", "Main")
	}
	return commands.Category(items[i].ID)
}

func (c *commandDialogCmp) detailLines(width int) []string {
	item, index := c.listView.GetSelectedItem()
	if index < 0 {
		return nil
	}
	_, description := commands.Presentation(item.ID, item.Title, item.Description)
	if c.activeID != "" {
		description = modeConsequence(item.ID)
		if strings.HasPrefix(item.ID, "approval-") && c.activeID != "mode-execute" {
			description = config.Text("Somente leitura neste modo; edições só em Executar. ", "Read only in this mode; edits only apply in Execute. ") + description
		}
	} else {
		// Rows already carry the description inline.
		return nil
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
	case "approval-prompt":
		return config.Text("Solicita aprovação para alterações e comandos. Padrão da sessão.", "Requests approval for changes and commands. Session default.")
	case "approval-project_edits":
		return config.Text("Em Executar, permite criar e editar arquivos comuns do projeto. Exclusões, movimentações, comandos, rede e caminhos sensíveis ou externos ainda pedem aprovação. Vale só nesta sessão.", "In Execute, allows creating and editing ordinary project files. Deletion, moves, commands, network and sensitive or external paths still require approval. Applies only to this session.")
	}
	return ""
}

func (c *commandDialogCmp) View() string {
	g := c.geometry()
	base := dialogBody()
	c.search.SetWidth(g.width - 2)
	var rows []string
	if c.activeID == "" {
		rows = append(rows, base.Width(g.width).Render(c.search.View()))
		if g.padding > 0 {
			rows = append(rows, "")
		}
	}
	items := c.listView.GetItems()
	_, selected := c.listView.GetSelectedItem()
	for i := g.start; i < min(len(items), g.start+g.count); i++ {
		if c.approvalHeading(i, g.start, items) {
			rows = append(rows, styles.PanelTitle().Render(config.Text("Aprovação", "Approval")))
		}
		if c.categoryHeading(i, g.start, items) {
			rows = append(rows, styles.PanelHint().Render(strings.ToUpper(c.commandGroup(i, items))))
		}
		rows = append(rows, c.commandRow(items[i], i == selected, g.width))
	}
	if len(items) == 0 {
		rows = append(rows, config.Text("Nenhum comando encontrado", "No commands found"))
	}
	for _, detail := range g.details {
		rows = append(rows, styles.PanelHint().Render(detail))
	}
	meta := ""
	if c.activeID == "" {
		meta = c.positionLabel(g, selected, len(items))
	}
	hint := config.Text("↑↓ escolher · Enter abrir · Esc voltar", "↑↓ select · Enter open · Esc back")
	if c.activeID != "" {
		hint = config.Text("↑↓ escolher · Enter aplicar · Esc voltar", "↑↓ select · Enter apply · Esc back")
	}
	return styles.Frame(lipgloss.JoinVertical(lipgloss.Left, rows...), g.width, 2, g.padding, c.dialogTitle(), meta, hint)
}

// positionLabel is the corner count: every command, or the matches.
func (c *commandDialogCmp) positionLabel(g commandGeometry, selected, total int) string {
	if strings.TrimSpace(c.search.Value()) == "" {
		return fmt.Sprint(total)
	}
	if total == 1 {
		return config.Text("1 resultado", "1 result")
	}
	return fmt.Sprintf(config.Text("%d resultados", "%d results"), total)
}

func (c *commandDialogCmp) commandRow(item Command, selected bool, width int) string {
	name, _ := commands.Presentation(item.ID, item.Title, item.Description)
	if localized := modeActionTitle(item.ID); localized != "" {
		name = localized
	}
	if item.ID == c.activeID || item.ID == c.approvalID {
		name = "✓ " + name
	}
	// The matched part of the name is underlined while searching.
	if query := strings.TrimSpace(c.search.Value()); query != "" && !strings.HasPrefix(name, "✓ ") {
		if at := strings.Index(strings.ToLower(name), strings.ToLower(query)); at >= 0 {
			name = name[:at] + ansi.Style{}.Underline(true).Styled(name[at:at+len(query)]) + name[at+len(query):]
		}
	}
	mark := "  "
	style := dialogBody()
	if selected {
		mark = "▌ "
		style = styles.SelectedRow().Bold(true)
	}
	if c.activeID != "" || width < 48 {
		return style.Width(width).Render(ansi.Truncate(mark+name, width, "…"))
	}
	// Title, a muted description and the shortcut as a key chip; commands
	// without a shortcut show their slash form.
	t := theme.CurrentTheme()
	_, description := commands.Presentation(item.ID, item.Title, item.Description)
	key := "/" + item.ID
	chip := style.Foreground(t.TextMuted()).Bold(false).Render(key)
	if shortcut := commandShortcut(item.ID); shortcut != "" {
		key = " " + keyLabel(shortcut) + " "
		chip = styles.BaseStyle().Background(t.BorderDim()).Foreground(styles.ReadableForeground(t.Text(), t.BorderDim())).Render(key)
	}
	room := max(1, width-ansi.StringWidth(key)-2)
	title := ansi.Truncate(mark+name, room, "…")
	if gap := room - ansi.StringWidth(title) - 2; gap > 4 && description != "" {
		title += "  " + style.Foreground(styles.ReadableForeground(t.TextMuted(), styles.PanelHint().GetBackground())).Bold(false).Render(ansi.Truncate(description, gap, "…"))
	}
	title += style.Render(strings.Repeat(" ", max(0, width-ansi.StringWidth(title)-ansi.StringWidth(key))))
	return style.Width(width).Render(title + chip)
}

func (c *commandDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(commandKeys)
}

func (c *commandDialogCmp) SetCommands(commands []Command) {
	c.title, c.activeID, c.approvalID = "", "", ""
	c.commands = commands
	c.search.SetValue("")
	c.search.Focus()
	c.filter()
}

// SetContext distinguishes a focused action from the mode already in use.
func (c *commandDialogCmp) SetContext(title, activeID string) {
	c.title, c.activeID = title, activeID
	c.FocusCommand(activeID)
}

func (c *commandDialogCmp) FocusCommand(id string) {
	_, selected := c.listView.GetSelectedItem()
	for i, item := range c.listView.GetItems() {
		if item.ID == id {
			key := tea.KeyDown
			if i < selected {
				key = tea.KeyUp
			}
			for range max(i-selected, selected-i) {
				c.listView.Update(tea.KeyPressMsg{Code: key})
			}
			break
		}
	}
}

func (c *commandDialogCmp) SetApprovalContext(activeID string) { c.approvalID = activeID }

func (c *commandDialogCmp) approvalHeading(i, start int, items []Command) bool {
	return c.approvalID != "" && strings.HasPrefix(items[i].ID, "approval-") && (i == start || !strings.HasPrefix(items[i-1].ID, "approval-"))
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
	c.featured = 0
	if query == "" && c.activeID == "" {
		// Unfiltered, the palette reads as a menu: the hand-ordered leading
		// commands first, then one group per category.
		c.featured = min(featuredCommands, len(matches))
		rest := matches[c.featured:]
		order := map[string]int{}
		for _, item := range rest {
			if _, ok := order[commands.Category(item.ID)]; !ok {
				order[commands.Category(item.ID)] = len(order)
			}
		}
		slices.SortStableFunc(rest, func(a, b Command) int {
			return order[commands.Category(a.ID)] - order[commands.Category(b.ID)]
		})
	}
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
	items := c.listView.GetItems()
	row := g.firstRow
	for i := g.start; i < min(len(items), g.start+g.count); i++ {
		if c.approvalHeading(i, g.start, items) {
			row++
		}
		if c.categoryHeading(i, g.start, items) {
			row++
		}
		if msg.Mouse().X >= 3 && msg.Mouse().X < 3+g.width && msg.Mouse().Y == row {
			return c, util.CmdHandler(CommandSelectedMsg{Command: items[i]})
		}
		row++
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
	case "approval-prompt":
		return config.Text("Pedir aprovação", "Ask for approval")
	case "approval-project_edits":
		return config.Text("Permitir edições no projeto", "Allow project edits")
	}
	return ""
}

func commandShortcut(id string) string {
	if id == "new" {
		return "ctrl+n"
	}
	return config.Shortcut(id)
}
