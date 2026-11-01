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
