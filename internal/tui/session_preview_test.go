package tui

import (
	"context"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/stretchr/testify/require"
	"testing"
)

type previewMessages struct {
	message.Service
	items []message.Message
}

func (p *previewMessages) List(context.Context, string) ([]message.Message, error) {
	return p.items, nil
}
func TestSessionPreviewUsesHumanTextAndHidesInternalRepairs(t *testing.T) {
	m := &appModel{app: &app.App{Messages: &previewMessages{items: []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Please explain this project"}}},
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Application validation: repair citation", Origin: "application"}}},
	}}}}
	preview := m.sessionPreview("selected")().(dialog.SessionPreviewLoadedMsg)
	require.Equal(t, "selected", preview.ID)
	require.Contains(t, preview.Text, "Please explain")
	require.NotContains(t, preview.Text, "Application validation")
}
