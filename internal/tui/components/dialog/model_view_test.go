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
	"github.com/stretchr/testify/require"
)

func populatedModelDialog(t *testing.T) *modelDialogCmp {
	t.Helper()
	m := catalogDialog(t)
	m.Open()
	m.loading = false
	m.models = nil
	for i := range 16 {
		id := fmt.Sprintf("model-%02d", i)
		m.models = append(m.models, models.Model{ID: models.ModelID(id), APIModel: id, Name: "Model " + fmt.Sprint(i), Provider: models.ProviderLocal})
	}
	config.Get().Preferences.FavoriteModels = nil
	config.Get().Preferences.RecentModels = nil
	config.Get().Agents[config.AgentCoder] = config.Agent{Model: "model-00"}
	m.filter()
	return m
}

func TestModelSearchHasIndependentFocusAndEditableCursor(t *testing.T) {
	m := populatedModelDialog(t)
	require.True(t, m.search.Focused())
	m.Update(tea.KeyPressMsg{Text: "mdel"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Text: "o"})
	require.Equal(t, "model", m.query)
	require.Len(t, m.filtered, 16)
	require.Equal(t, models.ProviderLocal, m.provider, "arrows edit the search rather than switching provider")
	m.Close()
	require.False(t, m.search.Focused())
}

func TestModelCurrentMarkerDoesNotFollowHighlightAndListScrolls(t *testing.T) {
	m := populatedModelDialog(t)
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	view := ansi.Strip(m.View())
	require.Contains(t, view, "✓ "+m.modelLabel(m.filtered[0]))
	require.Contains(t, view, "›   Model 1")
	require.Contains(t, view, "2 of 16")
	require.Contains(t, view, "↓")
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	require.Greater(t, m.scrollOffset, 0)
	require.Contains(t, ansi.Strip(m.View()), "12 of 16")
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	require.Equal(t, 12, m.selectedIdx)
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	require.Equal(t, 2, m.selectedIdx)
}

func TestModelLayoutAndMouseShareCompactGeometry(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 12}, {Width: 80, Height: 24}, {Width: 180, Height: 40}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			m := populatedModelDialog(t)
			m.models[0].Name = strings.Repeat("模型é", 60)
			m.filter()
			m.Update(size)
			for _, details := range []bool{false, true} {
				m.details = details
				m.keepVisible()
				view := m.View()
				require.LessOrEqual(t, lipgloss.Width(view), size.Width)
				require.LessOrEqual(t, lipgloss.Height(view), size.Height)
				require.Contains(t, ansi.Strip(view), "Enter")
			}
			m.details = false
			g := m.geometry()
			rows := strings.Split(ansi.Strip(m.View()), "\n")
			require.Contains(t, rows[g.listY+1], m.modelLabel(m.filtered[1]))
			_, cmd := m.Update(tea.MouseClickMsg{X: g.contentX, Y: g.listY + 1, Button: tea.MouseLeft})
			require.NotNil(t, cmd)
			require.Equal(t, m.filtered[1].ID, cmd().(ModelSelectedMsg).Model.ID)
		})
	}
}

func TestModelDetailsUnknownCapabilitiesAndConsistentLanguage(t *testing.T) {
	m := populatedModelDialog(t)
	m.details = true
	m.models[0].Name = "Llama 3.1 8B"
	m.models[0].APIModel = "llama3.1:8b"
	m.filter()
	require.Equal(t, "Llama 3.1 8B", m.modelLabel(m.models[0]), "do not repeat the equivalent API ID")
	config.Get().TUI.Language = "pt"
	view := ansi.Strip(m.View())
	require.Contains(t, view, "Escolher modelo")
	require.Contains(t, view, "Buscar:")
	require.Contains(t, view, "Capacidades: não informadas")
	require.NotContains(t, view, "Search")
	config.Get().TUI.Language = "en"
	require.Contains(t, ansi.Strip(m.View()), "Capabilities: not reported")
}

func TestSelectingModelCannotBeChangedUntilCancelled(t *testing.T) {
	m := populatedModelDialog(t)
	m.selecting = true
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Nil(t, cmd)
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	require.Equal(t, 0, m.selectedIdx)
	m.Update(tea.KeyPressMsg{Text: "ignored"})
	require.Empty(t, m.query)
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	require.IsType(t, CloseModelDialogMsg{}, cmd())
	require.False(t, m.selecting)
}

func TestModelPreferencesAndRoleSelectionRemainIndependent(t *testing.T) {
	m := populatedModelDialog(t)
	config.Get().Preferences.FavoriteModels = []models.ModelID{"model-08"}
	config.Get().Preferences.RecentModels = []models.ModelID{"model-09"}
	m.filter()
	require.Equal(t, models.ModelID("model-08"), m.filtered[1].ID)
	require.Equal(t, models.ModelID("model-09"), m.filtered[2].ID)
	require.Equal(t, models.ModelID("model-00"), m.filtered[m.selectedIdx].ID)
	config.Get().Agents[config.AgentTask] = config.Agent{Model: "model-09"}
	m.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	require.Equal(t, config.AgentTask, m.target)
	require.Equal(t, models.ModelID("model-09"), m.filtered[m.selectedIdx].ID)
	require.Equal(t, models.ModelID("model-00"), config.Get().Agents[config.AgentCoder].Model)
	view := ansi.Strip(m.View())
	require.Contains(t, view, "Task model")
	require.Contains(t, view, "› ✓ ↺ Model 9")
}
