package commands

import (
	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestKnownCommandPresentationAcceptsBothStoredLocales(t *testing.T) {
	// No configuration changes: a known stored English pair must use the
	// current locale, while a user-defined pair is always literal.
	entry := Command{ID: "settings", Title: "Configurações", Description: "Provedores e preferências"}
	localized := Localized(entry)
	title, description := Presentation("settings", "Settings", "Providers and preferences")
	require.Equal(t, localized.Title, title)
	require.Equal(t, localized.Description, description)
	title, description = Presentation("settings", "My Settings", "My provider notes")
	require.Equal(t, "My Settings", title)
	require.Equal(t, "My provider notes", description)
	require.Equal(t, config.Text("Configurações", "Settings"), localized.Title)
}
