package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/raulaguila/opcode/internal/tui/util"
)

type themeEditor struct {
	palette theme.CustomPalette
	restore string
	fields  []textinput.Model
	focus   int
	note    string
}

func (t *themeDialogCmp) openEditor() tea.Cmd {
	if len(t.themes) == 0 {
		return nil
	}
	name := t.themes[t.selectedIdx]
	palette, err := theme.PaletteFor(name)
	if err != nil {
		return util.ReportError(err)
	}
	e := &themeEditor{palette: palette, restore: name}
	for _, color := range []theme.AdaptiveColor{palette.Primary, palette.Text, palette.Muted, palette.Background} {
		for _, value := range []string{color.Light, color.Dark} {
			f := newDialogInput()
			f.Prompt = ""
			f.CharLimit = 7
			f.SetValue(value)
			e.fields = append(e.fields, f)
		}
	}
	e.fields[0].Focus()
	t.editor = e
	return textinput.Blink
}
func (e *themeEditor) candidate() theme.CustomPalette {
	p := e.palette
	colors := []*theme.AdaptiveColor{&p.Primary, &p.Text, &p.Muted, &p.Background}
	for i, c := range colors {
		c.Light = e.fields[2*i].Value()
		c.Dark = e.fields[2*i+1].Value()
	}
	return p
}
func (t *themeDialogCmp) editorUpdate(msg tea.Msg) (util.Model, tea.Cmd) {
	e := t.editor
	if result0, result1, handled := editorUpdateTeaKeyMsg(t, msg, e); handled {
		return result0, result1
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		t.width, t.height = size.Width, size.Height
		return t, nil
	}
	if result0, result1, handled := editorUpdateTeaMouseMsg(t, msg, e); handled {
		return result0, result1
	}
	var cmd tea.Cmd
	e.fields[e.focus], cmd = e.fields[e.focus].Update(msg)
	if err := theme.PreviewCustomPalette(e.candidate()); err != nil {
		e.note = err.Error()
		return t, cmd
	}
	e.note = config.Text("Prévia · alterações ainda não salvas", "Preview · changes are not saved")
	return t, tea.Batch(cmd, util.CmdHandler(ThemeChangedMsg{ThemeName: theme.CurrentThemeName(), Preview: true}))
}
func (t *themeDialogCmp) editorRows() (rows, start int) {
	height := t.height
	if height <= 0 {
		height = 24
	}
	rows = max(1, min(8, (height-11)/2))
	start = max(0, t.editor.focus-rows+1)
	return
}
func (t *themeDialogCmp) editorWidth() int {
	if t.width <= 0 {
		return 58
	}
	return max(1, min(58, t.width-8))
}
func (t *themeDialogCmp) editorView() string {
	e := t.editor
	width := t.editorWidth()
	line := func(s string) string { return dialogBody().Width(width).Render(ansi.Truncate(s, width, "…")) }
	rows, start := t.editorRows()
	lines := []string{line(config.Text("Editar tema · #RRGGBB", "Edit theme · #RRGGBB")), line(config.Text("Base: ", "Base: ") + e.palette.Base)}
	names := []string{config.Text("Destaque", "Primary"), config.Text("Texto", "Text"), config.Text("Texto secundário", "Muted text"), config.Text("Fundo", "Background")}
	for i := start; i < min(len(e.fields), start+rows); i++ {
		mode := config.Text("claro", "light")
		if i%2 == 1 {
			mode = config.Text("escuro", "dark")
		}
		label := fmt.Sprintf("%s%s · %s", selectionMark(i == e.focus), names[i/2], mode)
		e.fields[i].SetWidth(max(1, width-2))
		lines = append(lines, line(label), line(e.fields[i].View()))
	}
	lines = append(lines, styles.PanelDivider(width), line(config.Text("[Salvar] Ctrl+S · [Cancelar] Esc · Tab campo", "[Save] Ctrl+S · [Cancel] Esc · Tab field")), line(e.note))
	return styles.Panel().Padding(1, 2).Border(lipgloss.NormalBorder()).Render(strings.Join(lines, "\n"))
}

func editorUpdateThemeDialogCmpCtrlS(t *themeDialogCmp, e *themeEditor) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	if err := theme.SaveCustomPalette(e.candidate()); err != nil {
		e.note = err.Error()
		return t, nil, true
	}
	if err := theme.DiscardCustomPreview(theme.CustomThemeName); err != nil {
		e.note = err.Error()
		return t, nil, true
	}
	t.editor = nil
	t.currentTheme = theme.CustomThemeName
	t.themes = theme.AvailableThemes()
	for i, name := range t.themes {
		if name == theme.CustomThemeName {
			t.selectedIdx = i
		}
	}
	return t, util.CmdHandler(ThemeChangedMsg{ThemeName: theme.CustomThemeName}), true

}

func editorUpdateTeaMouseMsg(t *themeDialogCmp, msg tea.Msg, e *themeEditor) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	if mouse, ok := msg.(tea.MouseMsg); ok {
		if clickedLabel(t.editorView(), config.Text("[Salvar]", "[Save]"), mouse) {
			returned1Value0, returned1Value1 := t.editorUpdate(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
			return returned1Value0, returned1Value1, true
		}
		if clickedLabel(t.editorView(), config.Text("[Cancelar]", "[Cancel]"), mouse) {
			returned2Value0, returned2Value1 := t.editorUpdate(tea.KeyPressMsg{Code: tea.KeyEsc})
			return returned2Value0, returned2Value1, true
		}
		rows, start := t.editorRows()
		i := start + (mouse.Mouse().Y-4)/2
		if leftClick(mouse) && mouse.Mouse().Y >= 4 && mouse.Mouse().Y < 4+rows*2 && i < len(e.fields) && mouse.Mouse().X >= 3 && mouse.Mouse().X < t.editorWidth()+3 {
			e.fields[e.focus].Blur()
			e.focus = i
			return t, e.fields[i].Focus(), true
		}
		return t, nil, true
	}
	return extractedResult0, extractedResult1, false
}

func editorUpdateTeaKeyMsg(t *themeDialogCmp, msg tea.Msg, e *themeEditor) (extractedResult0 util.Model, extractedResult1 tea.Cmd, handled bool) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			if err := theme.DiscardCustomPreview(e.restore); err != nil {
				return t, util.ReportError(err), true
			}
			t.editor = nil
			return t, util.CmdHandler(ThemeChangedMsg{ThemeName: e.restore, Preview: true}), true
		case "ctrl+s":
			if result0, result1, handled := editorUpdateThemeDialogCmpCtrlS(t, e); handled {
				return result0, result1, true
			}
		case "tab", "enter", "down", "shift+tab", "up":
			step := 1
			if k.String() == "up" || k.String() == "shift+tab" {
				step = -1
			}
			e.fields[e.focus].Blur()
			e.focus = (e.focus + step + len(e.fields)) % len(e.fields)
			return t, e.fields[e.focus].Focus(), true
		}
	}
	return extractedResult0, extractedResult1, false
}
