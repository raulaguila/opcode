package chat

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func TestStreamingPresentationKeepsRawResponseAndCitationForCopy(t *testing.T) {
	m := renderFixture(2)
	last := m.messages[len(m.messages)-1]
	const raw = "Read README.md:1-2.\n\n**Recomendação"
	last.Parts = []message.ContentPart{message.TextContent{Text: raw}}
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	require.Contains(t, timelineText(m), "Recomendação")
	require.NotContains(t, timelineText(m), "**Recomendação")
	require.Equal(t, raw, m.LatestResponse())
	require.Equal(t, raw, m.messages[len(m.messages)-1].Content().String())
	last.Parts = append(last.Parts, message.Finish{Reason: message.FinishReasonEndTurn, Time: 1})
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	require.Contains(t, timelineText(m), "**Recomendação", "finished invalid Markdown remains literal")
	require.Equal(t, raw, m.LatestResponse())
}

func TestAnsweredQuestionIsCompactLiteralAndEmphasizesActualAnswer(t *testing.T) {
	m := exchangeFixture(`{"question":"Proceed with *literal* text?"}`, "**Not now**")
	var view string
	for _, item := range m.uiMessages {
		if item.ID == "answer:answer" {
			view = item.content
		}
	}
	require.Equal(t, 2, lipgloss.Height(view))
	require.Contains(t, ansi.Strip(view), "Proceed with *literal* text?")
	require.Contains(t, ansi.Strip(view), "You answered: **Not now**")
	screen := vt10x.New(vt10x.WithSize(80, 4))
	_, err := screen.Write([]byte(strings.ReplaceAll(view, "\n", "\r\n")))
	require.NoError(t, err)
	require.Zero(t, screen.Cell(1, 0).Mode&4, "question remains restrained")
	require.NotZero(t, screen.Cell(1, 1).Mode&4, "actual chosen answer is emphasized")
}

func TestCompletedReasoningDoesNotClaimItIsStillWorking(t *testing.T) {
	m := renderFixture(0)
	m.app.CoderAgent = revisionTestAgent{busy: true}
	item := message.Message{ID: "reply", SessionID: m.session.ID, Role: message.Assistant, Parts: []message.ContentPart{message.ReasoningContent{Thinking: "Private reasoning"}, message.TextContent{Text: "Public response"}}}
	m.messages = []message.Message{item}
	m.renderView()
	require.Contains(t, timelineText(m), config.Text("Detalhes", "Details"))
	require.NotContains(t, timelineText(m), config.Text("em andamento", "working"))
	item.Parts = append(item.Parts, message.Finish{Reason: message.FinishReasonEndTurn, Time: 1})
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: item})
	require.Contains(t, timelineText(m), config.Text("Detalhes", "Details"))
	require.NotContains(t, timelineText(m), config.Text("em andamento", "working"))
	require.Contains(t, timelineText(m), "Public response")
	require.NotContains(t, timelineText(m), "Private reasoning")
}
