package chat

import (
	"slices"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/commands"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

type SlashCompleter interface {
	SlashMenuOpen() bool
	SlashMenuView(width, maxHeight int) string
}

type slashOption struct {
	text, description string
	group             bool
}
type slashState struct {
	selected  int
	dismissed string
}

type CommandContextMsg struct{ Priorities []string }

func slashOptions(value string) ([]slashOption, bool) { return slashOptionsWith(value, nil) }
func slashOptionsWith(value string, priorities []string) ([]slashOption, bool) {
	value = strings.TrimLeft(value, " \t")
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\r\n") || strings.Contains(value[1:], "/") {
		return nil, false
	}
	query := strings.ToLower(value)
	var options []slashOption
	if query == "/" {
		seen := map[string]bool{}
		for _, id := range append(append([]string{}, priorities...), "new", "models", "settings", "files", "terminal", "sessions", "commands") {
			if seen[id] || len(options) >= 7 {
				continue
			}
			seen[id] = true
			for _, cmd := range commands.Builtins {
				if cmd.ID == id {
					cmd = commands.Localized(cmd)
					options = append(options, slashOption{"/" + cmd.ID, cmd.Description, cmd.Group})
				}
			}
		}
		return options, true
	}
	for _, cmd := range commands.Builtins {
		cmd = commands.Localized(cmd)
		options = append(options, slashOption{"/" + cmd.ID, cmd.Description, cmd.Group})
	}
	if query == "/mode" || strings.HasPrefix(query, "/mode ") {
		options = []slashOption{
			{"/mode ask", config.Text("Perguntar · somente leitura", "Ask · read only"), false},
			{"/mode plan", config.Text("Planejar · sem comandos ou edições", "Plan · no commands or edits"), false},
			{"/mode execute", config.Text("Executar · com permissões", "Execute · with permissions"), false},
		}
	} else if query == "/task" || strings.HasPrefix(query, "/task ") {
		options = []slashOption{
			{"/task", config.Text("Ver tarefa e critérios de aceite", "Inspect task and acceptance criteria"), false},
			{"/task new", config.Text("Nova tarefa · limpa o plano, mantém arquivos", "New task · clear plan, keep files"), false},
		}
	} else if strings.ContainsAny(query, " \t") {
		return nil, false
	}
	var matches []slashOption
	for _, option := range options {
		if commands.Match(query, option.text, option.description) {
			matches = append(matches, option)
		}
	}
	slices.SortStableFunc(matches, func(a, b slashOption) int { return commands.Rank(query, a.text) - commands.Rank(query, b.text) })
	return matches, true
}

func (m *editorCmp) SlashMenuOpen() bool {
	if !m.textarea.Focused() || m.deleteMode || m.submitting || m.textarea.Value() == m.slash.dismissed {
		return false
	}
	if m.textarea.Line() != 0 {
		return false
	}
	info := m.textarea.LineInfo()
	if info.StartColumn+info.ColumnOffset != utf8.RuneCountInString(m.textarea.Value()) {
		return false
	}
	_, open := slashOptionsWith(m.textarea.Value(), m.commandPriorities)
	return open
}

func (m *editorCmp) slashKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if !m.SlashMenuOpen() {
		return nil, false
	}
	options, _ := slashOptionsWith(m.textarea.Value(), m.commandPriorities)
	switch msg.String() {
	case "esc":
		m.slash.dismissed = m.textarea.Value()
		return nil, true
	case "up", "down":
		if len(options) > 0 {
			step := 1
			if msg.String() == "up" {
				step = -1
			}
			m.slash.selected = (m.slash.selected + step + len(options)) % len(options)
		}
		return nil, true
	case "tab", "enter":
		if len(options) == 0 {
			return nil, msg.String() == "tab"
		}
		option := options[min(m.slash.selected, len(options)-1)]
		m.textarea.SetValue(option.text)
		m.textarea.CursorEnd()
		m.slash.selected = 0
		m.slash.dismissed = ""
		if option.group {
			return nil, true
		}
		m.slash.dismissed = option.text
		if msg.String() == "tab" {
			return nil, true
		}
		return m.send(), true
	}
	return nil, false
}

func (m *editorCmp) SlashMenuView(width, maxHeight int) string {
	if !m.SlashMenuOpen() || width < 1 || maxHeight < 1 {
		return ""
	}
	options, _ := slashOptionsWith(m.textarea.Value(), m.commandPriorities)
	width = min(width, 76)
	if len(options) == 0 {
		return styles.BaseStyle().Foreground(theme.CurrentTheme().TextMuted()).Render(ansi.Truncate(config.Text(" Nenhum comando encontrado · Esc fecha", " No commands found · Esc closes"), width, "…"))
	}
	selected := min(m.slash.selected, len(options)-1)
	if width < 20 || maxHeight < 4 {
		return styles.BaseStyle().Foreground(theme.CurrentTheme().Primary()).Render(ansi.Truncate("› "+strings.TrimSpace(options[selected].text), width, "…"))
	}
	t := theme.CurrentTheme()
	rowWidth := width - 2
	count := min(8, len(options), maxHeight-3)
	start := max(0, min(selected-count+1, len(options)-count))
	var rows []string
	for i := start; i < start+count; i++ {
		option := options[i]
		mark := " "
		if i == selected {
			mark = "›"
		}
		label := mark + " " + strings.TrimSpace(option.text) + "  " + option.description
		style := styles.BaseStyle().Width(rowWidth).Foreground(t.Text()).Background(t.Background())
		if i == selected {
			style = style.Background(t.Primary()).Foreground(t.Background()).Bold(true)
		}
		rows = append(rows, style.Render(ansi.Truncate(label, rowWidth, "…")))
	}
	rows = append(rows, styles.BaseStyle().Foreground(t.TextMuted()).Render(ansi.Truncate(config.Text(" ↑/↓ escolhe · Tab completa · Enter seleciona · Esc fecha", " ↑/↓ choose · Tab complete · Enter select · Esc close"), rowWidth, "…")))
	return styles.BaseStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.BorderNormal()).Width(rowWidth).Render(strings.Join(rows, "\n"))
}
