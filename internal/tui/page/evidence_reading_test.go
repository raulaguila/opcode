package page

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/components/chat"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

type evidenceTranscript struct {
	selectionProbe
	state string
}

func (e *evidenceTranscript) LatestResponseID() string { return "evidence-response" }

func (e *evidenceTranscript) ResponseCitationStatus(messageID string) string {
	if messageID == e.LatestResponseID() {
		return e.state
	}
	return ""
}

func TestSourceBrowserShowsRecordedValidationAndKeepsDeletedReferences(t *testing.T) {
	p, root := citationSourceWorkspace(t)
	text := "See a/main.go:1-2 and src/deleted.go:7."
	p.transcript = &evidenceTranscript{selectionProbe: selectionProbe{text: text}, state: message.CitationValid}
	p.openResponseActions(text)
	var action workspaceAction
	for _, item := range p.workspace.actions {
		if item.kind == "sources" {
			action = item
		}
	}
	require.Equal(t, message.CitationValid, action.citationStatus)
	p.openCitationSources(action.text, action.citationStatus)
	require.Contains(t, p.workspace.body, config.Text("ao gerar", "generated"))
	require.Contains(t, p.workspace.body, config.Text("não foram revalidados agora", "not been revalidated now"))
	require.Len(t, p.workspace.actions, 3)
	require.Contains(t, p.workspace.actions[1].label, config.Text("indisponível", "unavailable"))
	require.NoError(t, os.Remove(filepath.Join(root, "a/main.go")))
	p.openCitationSource(text, "a/main.go", action.citationStatus)
	require.Len(t, p.workspace.actions, 1, "a disappeared file retains copy action but no unusable preview")
	require.Equal(t, "copy", p.workspace.actions[0].kind)
	require.Contains(t, p.workspace.body, "a/main.go:1-2")
	require.Contains(t, p.workspace.body, config.Text("indisponível", "unavailable"))
	require.Empty(t, responseSources("Use `Run` with `something` or https://example.test/file.go:3"), "missing bare words and URLs are not file references")
}

func TestUnknownCitationStateDoesNotBecomeValidated(t *testing.T) {
	p, _ := citationSourceWorkspace(t)
	p.openCitationSources("a/main.go:1-2")
	require.Contains(t, p.workspace.body, config.Text("sem validação registrada", "no recorded validation"))
	p.openCitationSources("a/main.go:1-2", message.CitationInvalid)
	require.Contains(t, p.workspace.body, config.Text("não validadas", "not validated"))
}

func TestResponseReadingPresetsPersistOnlyWidthAndPreserveDraft(t *testing.T) {
	p, root := citationSourceWorkspace(t)
	cfg := config.Get()
	oldWidth := cfg.TUI.ProseWidth
	oldFile := viper.ConfigFileUsed()
	t.Cleanup(func() { cfg.TUI.ProseWidth = oldWidth; viper.SetConfigFile(oldFile) })
	path := filepath.Join(root, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"tui":{"proseWidth":0},"marker":"preserved"}`), 0600))
	viper.SetConfigFile(path)
	cfg.TUI.ProseWidth = 0
	p.editor.Update(chat.RestoreDraftMsg{Text: "my draft"})
	p.openResponseActions("Answer")
	for i, item := range p.workspace.actions {
		if item.kind == "reading-width" && item.text == "96" {
			p.workspace.selected = i
		}
	}
	cmd := p.workspaceChoose()
	require.NotNil(t, cmd)
	require.IsType(t, chat.ReadingWidthChangedMsg{}, cmd())
	require.Equal(t, 96, cfg.TUI.ProseWidth)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), `"proseWidth": 96`)
	require.Contains(t, string(data), `"marker": "preserved"`)
	draft, _ := p.composer.(chat.DraftControl).Draft()
	require.Equal(t, "my draft", draft)
	p.openResponseActions("Answer")
	require.True(t, strings.Contains(p.workspace.actions[len(p.workspace.actions)-1].label, "✓"))
	require.NotNil(t, p.setReadingWidth("0"))
	require.Zero(t, cfg.TUI.ProseWidth)
	t.Setenv("OPCODE_TUI_PROSEWIDTH", "80")
	cmd = p.setReadingWidth("96")
	require.NotNil(t, cmd)
	require.Zero(t, cfg.TUI.ProseWidth, "failed persistence must not change rendering configuration")
}
