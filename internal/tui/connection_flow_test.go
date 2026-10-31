package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/stretchr/testify/require"
	"testing"
)

type catalogRouteProbe struct {
	dialog.ModelDialog
	destination models.ModelProvider
	resumed     bool
}

func (p *catalogRouteProbe) Open() tea.Cmd { p.destination = "default"; return nil }
func (p *catalogRouteProbe) Close()        {}
func (p *catalogRouteProbe) OpenForProvider(id models.ModelProvider) tea.Cmd {
	p.destination = id
	return nil
}
func (p *catalogRouteProbe) Resume() tea.Cmd { p.resumed = true; return nil }
func TestSavingConnectionRoutesToItsCatalogInsteadOfOldActiveProvider(t *testing.T) {
	catalog := &catalogRouteProbe{}
	a := appModel{app: &app.App{}, modelDialog: catalog, providerDialog: dialog.NewProviderDialogCmp()}
	_, handled := a.productivityMessage(dialog.ProviderSettingsSavedMsg{ProviderChanged: true, Provider: "profile:office"})
	require.True(t, handled)
	require.True(t, a.showModelDialog)
	require.False(t, a.showProviderDialog)
	require.Equal(t, models.ModelProvider("profile:office"), catalog.destination)
	_, handled = a.productivityMessage(dialog.OpenProviderDialogMsg{Provider: "profile:office", ReturnToModels: true})
	require.True(t, handled)
	require.False(t, a.showModelDialog)
	require.True(t, a.showProviderDialog)
	_, handled = a.productivityMessage(dialog.CloseProviderDialogMsg{})
	require.True(t, handled)
	require.True(t, a.showModelDialog)
	require.True(t, catalog.resumed)
	require.False(t, a.providerReturnToModels)
}
