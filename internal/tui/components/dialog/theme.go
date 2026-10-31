package dialog

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/layout"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

// ThemeChangedMsg is sent when the theme is changed
type ThemeChangedMsg struct {
	ThemeName string
	Preview   bool
}

// CloseThemeDialogMsg is sent when the theme dialog is closed
type CloseThemeDialogMsg struct{ RestoreTheme string }

// ThemeDialog interface for the theme switching dialog
type ThemeDialog interface {
	util.Model
	layout.Bindings
}

type themeDialogCmp struct {
	editor       *themeEditor
	themes       []string
	selectedIdx  int
	width        int
	height       int
	currentTheme string
}

type themeKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Escape key.Binding
	J      key.Binding
	K      key.Binding
}

var themeKeys = themeKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("↑", "previous theme"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("↓", "next theme"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select theme"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
	J: key.NewBinding(
		key.WithKeys("j"),
		key.WithHelp("j", "next theme"),
	),
	K: key.NewBinding(
		key.WithKeys("k"),
		key.WithHelp("k", "previous theme"),
	),
}

func (t *themeDialogCmp) Init() tea.Cmd {
	// Load available themes and update selectedIdx based on current theme
	t.themes = theme.AvailableThemes()
	t.currentTheme = theme.CurrentThemeName()

	// Find the current theme in the list
	for i, name := range t.themes {
		if name == t.currentTheme {
			t.selectedIdx = i
			break
		}
	}

	return nil
}

func (t *themeDialogCmp) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	if t.editor != nil {
		return t.editorUpdate(msg)
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if result0, result1, handled := updateThemeDialogCmpKeyMsg(t, &msg); handled {
			return result0, result1
		}
	case tea.MouseMsg:
		count := len(t.themes)
		if t.height > 0 {
			count = max(1, min(count, t.height-9))
		}
		start := max(0, t.selectedIdx-count+1)
		row := msg.Mouse().Y - 4
		if leftClick(msg) && msg.Mouse().X >= 3 && row >= 0 && row < count && start+row < len(t.themes) {
			t.selectedIdx = start + row
			return t, t.preview()
		}
	case tea.WindowSizeMsg:
		t.width = msg.Width
		t.height = msg.Height
	}
	return t, nil
}

func (t *themeDialogCmp) View() string {
	if t.editor != nil {
		return t.editorView()
	}
	currentTheme := theme.CurrentTheme()
	baseStyle := dialogBody()

	if len(t.themes) == 0 {
		return baseStyle.Padding(1, 2).
			Border(lipgloss.NormalBorder()).
			BorderBackground(currentTheme.BackgroundSecondary()).
			BorderForeground(currentTheme.TextMuted()).
			Width(40).
			Render(config.Text("Nenhum tema disponível", "No themes available"))
	}

	// Calculate max width needed for theme names
	maxWidth := 40 // Minimum width
	for _, themeName := range t.themes {
		if len(themeName) > maxWidth-4 { // Account for padding
			maxWidth = len(themeName) + 4
		}
	}

	if t.width > 0 {
		maxWidth = max(1, min(maxWidth, t.width-8))
	} // Limit width to avoid overflow

	// Build the theme list
	themeItems := make([]string, 0, len(t.themes))
	count := len(t.themes)
	if t.height > 0 {
		count = max(1, min(count, t.height-9))
	}
	start := max(0, t.selectedIdx-count+1)
	for i := start; i < min(len(t.themes), start+count); i++ {
		themeName := t.themes[i]
		itemStyle := baseStyle.Width(maxWidth)

		if i == t.selectedIdx {
			itemStyle = itemStyle.
				Background(currentTheme.BorderDim()).
				Foreground(currentTheme.Text()).
				Bold(true)
		}

		themeItems = append(themeItems, itemStyle.Render(ansi.Truncate(selectionMark(i == t.selectedIdx)+themeName, maxWidth, "…")))
	}

	title := baseStyle.
		Foreground(currentTheme.Primary()).
		Bold(true).
		Width(maxWidth).
		Padding(0, 1).
		Render(config.Text("Escolher tema", "Select Theme"))

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(maxWidth).Render(""),
		baseStyle.Width(maxWidth).Render(lipgloss.JoinVertical(lipgloss.Left, themeItems...)),
		styles.PanelDivider(maxWidth),
		dialogHint(config.Text("↑↓ prévia · Ctrl+E edita · Enter · Esc", "↑↓ preview · Ctrl+E edit · Enter · Esc"), maxWidth),
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.NormalBorder()).
		BorderBackground(currentTheme.BackgroundSecondary()).
		BorderForeground(currentTheme.TextMuted()).
		Render(content)
}

func (t *themeDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(themeKeys)
}

// NewThemeDialogCmp creates a new theme switching dialog
func NewThemeDialogCmp() ThemeDialog {
	return &themeDialogCmp{
		themes:       []string{},
		selectedIdx:  0,
		currentTheme: "",
	}
}

func (t *themeDialogCmp) preview() tea.Cmd {
	if len(t.themes) == 0 {
		return nil
	}
	name := t.themes[t.selectedIdx]
	if err := theme.PreviewTheme(name); err != nil {
		return util.ReportError(err)
	}
	return util.CmdHandler(ThemeChangedMsg{ThemeName: name, Preview: true})
}

func updateThemeDialogCmpKeyMsg(t *themeDialogCmp, msg *tea.KeyPressMsg) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	if msg.String() == "ctrl+e" {
		return t, t.openEditor(), true
	}
	switch {
	case key.Matches((*msg), themeKeys.Up) || key.Matches((*msg), themeKeys.K):
		if t.selectedIdx > 0 {
			t.selectedIdx--
		}
		return t, t.preview(), true
	case key.Matches((*msg), themeKeys.Down) || key.Matches((*msg), themeKeys.J):
		if t.selectedIdx < len(t.themes)-1 {
			t.selectedIdx++
		}
		return t, t.preview(), true
	case key.Matches((*msg), themeKeys.Enter):
		if result0, result1, result2, handled := updateThemeDialogCmpKeyMsgMatchesThemeKeysEnter(t); handled {
			return result0, result1, result2
		}
	case key.Matches((*msg), themeKeys.Escape):
		return t, util.CmdHandler(CloseThemeDialogMsg{RestoreTheme: t.currentTheme}), true
	}
	return extractedResult0, extractedResult1, false
}

func updateThemeDialogCmpKeyMsgMatchesThemeKeysEnter(t *themeDialogCmp) (extractedResult0 util.Model, extractedResult1 tea.Cmd, extractedResult2 bool, handled bool) {
	if len(t.themes) > 0 {
		previousTheme := t.currentTheme
		selectedTheme := t.themes[t.selectedIdx]
		if previousTheme == selectedTheme {
			return t, util.CmdHandler(CloseThemeDialogMsg{}), true, true
		}
		if err := theme.SetTheme(selectedTheme); err != nil {
			return t, util.ReportError(err), true, true
		}
		return t, util.CmdHandler(ThemeChangedMsg{
			ThemeName: selectedTheme,
		}), true, true
	}
	return extractedResult0, extractedResult1, extractedResult2, false
}
