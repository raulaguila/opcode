package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/ui/dialog"
)

// formStater is the connection editor seen as data, so it can be drawn in
// the proposal's style; the editor keeps the keys and every write.
type formStater interface {
	FormState() (dialog.ProviderFormState, bool)
}

// connectionView is the proposal's connection form: labeled fields, radio
// choices, the catalog result and the actions as buttons. Presets come
// first for a new connection.
func connectionView(state dialog.ProviderFormState, screenWidth int) string {
	box := Box{Title: state.Title, Meta: state.Meta, Width: min(84, screenWidth-4), PadX: 2, PadY: 1}
	inner := box.Inner()
	var rows []string
	if len(state.Presets) > 0 {
		rows = append(rows, on(P.Surface, P.Muted).Render(state.Note), "")
		for i, name := range state.Presets {
			bg, fg, bar := P.Surface, P.Text, on(P.Surface, P.Surface).Render("  ")
			if i == state.Selected {
				bg, fg, bar = P.Selected, P.Strong, on(P.Surface, P.Ask).Render("▌ ")
			}
			rows = append(rows, bar+Line(on(bg, fg).Bold(i == state.Selected).Render(name), inner-2, bg))
		}
		rows = append(append(rows, ""), KeyRow(textFor("↑↓ escolher · Enter configurar · Esc voltar", "↑↓ choose · Enter configure · Esc back"), inner, P.Surface)...)
		return box.Render(strings.Join(rows, "\n"))
	}
	// Labels sit right-aligned in a column as wide as the longest one.
	labelWidth := 0
	for _, field := range state.Fields {
		labelWidth = max(labelWidth, ansi.StringWidth(field.Label))
	}
	labelWidth = min(labelWidth, inner/3)
	valueWidth := inner - labelWidth - 2
	for _, field := range state.Fields {
		name := ansi.Truncate(field.Label, labelWidth, "…")
		pad := on(P.Surface, P.Surface).Render(strings.Repeat(" ", labelWidth-ansi.StringWidth(name)))
		label := on(P.Surface, P.Muted).Render(name)
		if field.Focused {
			label = on(P.Surface, P.Ask).Bold(true).Render(name)
		}
		rows = append(rows, pad+label+on(P.Surface, P.Surface).Render("  ")+fieldValue(field, valueWidth), "")
	}
	switch {
	case state.Tested:
		rows = append(rows, Line(on(P.Added, P.Success).Render(" ✓ ")+on(P.Added, P.Text).Render(state.Note), inner, P.Added))
	case strings.HasSuffix(state.Note, "/error"):
		rows = append(rows, Line(on(P.DangerCard, P.Danger).Render(" ✗ "+state.Note), inner, P.DangerCard))
	case state.Note != "":
		for _, line := range strings.Split(ansi.Wrap(state.Note, inner, ""), "\n") {
			rows = append(rows, on(P.Surface, P.Muted).Render(line))
		}
	}
	for _, warning := range state.Warnings {
		rows = append(rows, on(P.Surface, P.Plan).Render("▲ "+warning))
	}
	advanced := textFor("› Avançado: janela de contexto, tokens de saída, TLS · Ctrl+A", "› Advanced: context window, output tokens, TLS · Ctrl+A")
	if state.Advanced {
		advanced = textFor("‹ Ocultar avançado · Ctrl+A", "‹ Hide advanced · Ctrl+A")
	}
	rows = append(rows, "", on(P.Surface, P.Muted).Render(advanced), "")
	gap := on(P.Surface, P.Surface).Render("  ")
	buttons := Button(textFor("Salvar · Ctrl+S", "Save · Ctrl+S"), true, P.Ask) + gap
	if state.CanEnable {
		buttons = Button(textFor("Salvar e ativar · Ctrl+G", "Save and enable · Ctrl+G"), true, P.Ask) + gap + Button(textFor("Salvar · Ctrl+S", "Save · Ctrl+S"), false, P.Ask) + gap
	}
	buttons += Button(textFor("Testar catálogo · Ctrl+T", "Test catalog · Ctrl+T"), false, P.Ask)
	rows = append(rows, Spread(buttons, on(P.Raised, P.Text).Render(" "+textFor("Cancelar · Esc", "Cancel · Esc")+" "), inner, P.Surface))
	rows = append(append(rows, ""), KeyRow(textFor("Tab campo · ←→ opção · Esc voltar", "Tab field · ←→ option · Esc back"), inner, P.Surface)...)
	return box.Render(strings.Join(rows, "\n"))
}

// fieldValue is a radio row for choices and a filled field for text.
func fieldValue(field dialog.ProviderFormField, width int) string {
	if len(field.Options) > 0 {
		var parts []string
		for i, option := range field.Options {
			if option == field.Value {
				parts = append(parts, on(P.Surface, P.Ask).Render("◉ ")+on(P.Surface, P.Strong).Render(field.OptionLabels[i]))
			} else {
				parts = append(parts, on(P.Surface, P.Faint).Render("○ ")+on(P.Surface, P.Text).Render(field.OptionLabels[i]))
			}
		}
		return Line(on(P.Surface, P.Surface).Render(" ")+strings.Join(parts, on(P.Surface, P.Surface).Render("   ")), width, P.Surface)
	}
	value := field.Value
	if field.Secret && value != "" {
		value = strings.Repeat("•", min(24, len([]rune(value))))
	}
	text := on(P.Field, P.Strong).Render(" " + ansi.TruncateLeft(value, max(0, ansi.StringWidth(value)-width+3), "…"))
	if value == "" {
		text = on(P.Field, P.Faint).Render(" " + field.Placeholder)
	}
	if field.Focused {
		text += on(P.Field, P.Ask).Render("▏")
	}
	return Line(text, width, P.Field)
}
