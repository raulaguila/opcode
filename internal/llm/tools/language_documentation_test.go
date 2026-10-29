package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLanguageSupportDocumentationMatchesRuntimeInventory(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // Installed servers must never affect generated docs.
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "language-support.md"))
	require.NoError(t, err)
	require.Equal(t, string(data), LanguageSupportMarkdown(), "regenerate with go run ./scripts/language-docs")
	registered := RegisteredLanguageSupport()
	report, err := InspectLanguageSupport(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, report.Languages, len(registered))
	for i, entry := range registered {
		actual := report.Languages[i]
		require.Equal(t, entry.Language, actual.Language)
		require.Equal(t, entry.LocalParser, actual.LocalParser)
		require.Equal(t, entry.ParserFixture, actual.ParserFixture)
		require.Equal(t, entry.SemanticReferences, actual.SemanticReferences)
	}
	// Callers own their slices; modifying a snapshot must not change the registry.
	registered[0].Language = "modified"
	require.NotEqual(t, "modified", RegisteredLanguageSupport()[0].Language)
}
