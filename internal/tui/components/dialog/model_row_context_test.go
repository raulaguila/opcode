package dialog

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func TestModelRowsShowKnownUnknownAndEstimatedContext(t *testing.T) {
	m := populatedModelDialog(t)
	m.models[0].ContextWindow = 65536
	m.models[0].Hosting = "local"
	m.models[1].ContextWindow = 131072
	m.models[1].ContextEstimated = true
	m.models[1].Hosting = "cloud"
	config.Get().Preferences.FavoriteModels = []models.ModelID{m.models[1].ID}
	config.Get().Preferences.RecentModels = []models.ModelID{m.models[2].ID}
	m.filter()
	m.selectedIdx = 1
	before := append([]models.Model(nil), m.filtered...)
	for _, width := range []int{36, 60, 88} {
		rows := renderedModelContextRows(t, m, width, 3)
		require.Contains(t, rows[0], "✓ model-00 · Local")
		require.Contains(t, rows[0], "64k")
		require.Contains(t, rows[0], config.Text("Em uso", "In use"))
		require.Contains(t, rows[1], "▌   ★ Model 1 · ")
		require.Contains(t, rows[1], "~128k")
		require.Contains(t, rows[2], "↺ Model 2")
		require.Contains(t, rows[2], "—")
		if width >= 48 {
			require.Contains(t, rows[1], config.Text("Favoritos", "Favorites"))
			require.Contains(t, rows[2], config.Text("Recentes", "Recent"))
		}
	}
	require.Equal(t, before, m.filtered, "drawing context never changes catalog metadata")
	require.Equal(t, 1, m.selectedIdx)
}

func TestModelContextRowsPreserveUnicodeIdentityAndAdvertisedLimit(t *testing.T) {
	m := populatedModelDialog(t)
	m.models[0].Name = "模型 ação " + strings.Repeat("模型é", 40)
	m.models[0].ContextWindow = 65536
	m.filter()
	m.activeContextModel = m.models[0].ID
	m.activeContextWindow = 4096
	m.activeContextSource = "ollama:loaded"
	m.target = config.AgentCoder
	for _, width := range []int{36, 60, 88} {
		rows := renderedModelContextRows(t, m, width, 1)
		require.Contains(t, rows[0], "✓ 模型 ação")
		require.Contains(t, rows[0], "64k", "row describes the catalog model, not the active request window")
		require.NotRegexp(t, `(?:^|\s)4k(?:\s|$)`, rows[0])
	}
	require.Contains(t, strings.Join(m.modelDetailLines(), "\n"), config.Text("Efetivo: 4k", "Effective: 4k"))
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 20}, {Width: 60, Height: 24}, {Width: 88, Height: 28}} {
		m.Update(size)
		require.LessOrEqual(t, lipgloss.Width(m.View()), size.Width)
		require.LessOrEqual(t, lipgloss.Height(m.View()), size.Height)
	}
}

func renderedModelContextRows(t *testing.T, m *modelDialogCmp, width, count int) []string {
	t.Helper()
	base := dialogBody().Width(width).MaxWidth(width)
	var rows []string
	g := modelGeometry{width: width, rows: count}
	catalogViewScrollOffset(m, &g, theme.CurrentTheme(), &base, &rows, m.activeModelID())
	require.Len(t, rows, count)
	for i, row := range rows {
		rows[i] = ansi.Strip(row)
		require.Equal(t, width, ansi.StringWidth(rows[i]))
		require.Equal(t, 1, lipgloss.Height(rows[i]))
	}
	return rows
}

func TestCompactContextDoesNotInventUnknownMetadata(t *testing.T) {
	for _, test := range []struct {
		model models.Model
		want  string
	}{
		{models.Model{}, "—"},
		{models.Model{ContextWindow: -1, ContextEstimated: true}, "—"},
		{models.Model{ContextWindow: 65536}, "64k"},
		{models.Model{ContextWindow: 131072, ContextEstimated: true}, "~128k"},
		{models.Model{ContextWindow: 1048576}, "1.0M"},
	} {
		t.Run(fmt.Sprint(test.model.ContextWindow), func(t *testing.T) {
			require.Equal(t, test.want, modelRowContext(test.model))
		})
	}
}
