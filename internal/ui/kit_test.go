package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestBoxDrawsTitleMetaLegendAndExactWidth(t *testing.T) {
	view := ansi.Strip(Box{Title: "Escolher modelo", Meta: "Ollama · 7 de 8", Legend: "Enter usar · Esc fechar", Width: 60, PadX: 2, PadY: 1}.Render("linha\numa linha longa demais " + strings.Repeat("x", 80)))
	lines := strings.Split(view, "\n")
	require.Len(t, lines, 6)
	for _, line := range lines {
		require.Equal(t, 60, ansi.StringWidth(line), line)
	}
	require.True(t, strings.HasPrefix(lines[0], "╭─ Escolher modelo "))
	require.True(t, strings.HasSuffix(lines[0], " Ollama · 7 de 8 ─╮"))
	require.True(t, strings.HasPrefix(lines[5], "╰─ Enter usar · Esc fechar "))
	require.Contains(t, lines[3], "…")
}

func TestBoxNeverWidensANarrowFrame(t *testing.T) {
	for _, line := range strings.Split(ansi.Strip(Box{Title: "Título bem longo", Meta: "meta", Legend: "legenda", Width: 8}.Render("x")), "\n") {
		require.Equal(t, 8, ansi.StringWidth(line), line)
	}
}

func TestSpreadAndMeter(t *testing.T) {
	row := ansi.Strip(Spread("esquerda", "direita", 30, P.Ground))
	require.Equal(t, 30, ansi.StringWidth(row))
	require.True(t, strings.HasSuffix(row, "direita"))
	require.Equal(t, "██░░░", ansi.Strip(Meter(0.45, P.Info, P.Ground)))
	require.Equal(t, "█████", ansi.Strip(Meter(2, P.Info, P.Ground)))
}

func TestListSearchRanksTitleMatchesFirst(t *testing.T) {
	l := List{Items: []Item{{Title: "Planejar", Desc: "prepara um plano"}, {Title: "Plano", Desc: "mostrar"}}}
	l.SetQuery("plano")
	require.Equal(t, "Plano", l.Visible()[0].Title)
	require.Len(t, l.Visible(), 2)
}

func TestKeyRowPushesTheLastPairRight(t *testing.T) {
	rows := KeyRow("Enter usar · Tab provedor · Esc fechar", 50, P.Surface)
	require.Len(t, rows, 2)
	row := ansi.Strip(rows[1])
	require.Equal(t, 50, ansi.StringWidth(row))
	require.True(t, strings.HasPrefix(row, "Enter usar   Tab provedor"))
	require.True(t, strings.HasSuffix(row, "Esc fechar"))
}

func TestDashedBoxKeepsWidthAndCorners(t *testing.T) {
	lines := strings.Split(ansi.Strip(Box{Width: 12, PadX: 1, Dashed: true}.Render("x")), "\n")
	require.Equal(t, "╭┄┄┄┄┄┄┄┄┄┄╮", lines[0])
	require.Equal(t, "┆ x        ┆", lines[1])
	for _, line := range lines {
		require.Equal(t, 12, ansi.StringWidth(line))
	}
}

func TestThinBoxAndOutlineButtonsKeepWidths(t *testing.T) {
	box := ThinBox([]string{"x"}, 10, P.Border, P.Card, P.Surface, false)
	require.Equal(t, " ▁▁▁▁▁▁▁▁ ", ansi.Strip(box[0]))
	require.Equal(t, "▕x       ▏", ansi.Strip(box[1]))
	require.Equal(t, " ▔▔▔▔▔▔▔▔ ", ansi.Strip(box[2]))
	dashed := ThinBox([]string{"x"}, 10, P.Border, P.Card, P.Surface, true)
	require.Equal(t, 10, ansi.StringWidth(ansi.Strip(dashed[0])))
	buttons := OutlineButtons([]string{"Editar", "Testar"}, P.Border, P.Text, P.Card)
	require.Equal(t, "▕ Editar ▏ ▕ Testar ▏", ansi.Strip(buttons[1]))
	require.Equal(t, ansi.StringWidth(ansi.Strip(buttons[1])), ansi.StringWidth(ansi.Strip(buttons[0])))
}
