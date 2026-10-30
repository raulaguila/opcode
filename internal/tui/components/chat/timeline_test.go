package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func timelineText(m *messagesCmp) string { return ansi.Strip(strings.Join(m.transcriptLines, "\n")) }

func TestTimelineInterleavesMessagesAndIndependentToolGroups(t *testing.T) {
	m := NewMessagesCmp(&app.App{}).(*messagesCmp)
	m.SetSize(100, 60)
	m.messages = []message.Message{
		{ID: "u", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Inspect project"}}},
		{ID: "read", Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "read-call", Name: "view", Input: `{"file_path":"README.md"}`, Finished: true}}},
		{ID: "diagnosis", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Public diagnosis"}, message.ToolCall{ID: "ask-call", Name: "ask_user_question", Input: `{"question":"Apply changes?"}`, Finished: true}}},
		{ID: "repair", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Private correction", Origin: "application"}}},
		{ID: "confirmation", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Changes can wait"}}},
	}
	m.renderView()
	view := timelineText(m)
	require.Contains(t, view, "Public diagnosis")
	require.Contains(t, view, "Changes can wait")
	require.NotContains(t, view, "Private correction")
	var hits []transcriptHit
	for _, hit := range m.hits {
		if hit.kind == "process" {
			hits = append(hits, hit)
		}
	}
	require.Len(t, hits, 2)
	require.NotEqual(t, hits[0].id, hits[1].id)
	require.Less(t, strings.Index(view, "Public diagnosis"), strings.LastIndex(view, "Process"))
	m.toggleProcess(hits[1].id)
	view = timelineText(m)
	require.Contains(t, view, "Private correction")
	require.Equal(t, 1, strings.Count(view, "Public diagnosis"))
	m.toggleProcess(hits[1].id)
	require.NotContains(t, timelineText(m), "Private correction")
	require.Contains(t, timelineText(m), "Public diagnosis")
}

func TestCitationRepairKeepsPublicResponseVisible(t *testing.T) {
	for _, state := range []string{message.CitationPending, message.CitationInvalid, message.CitationSuperseded} {
		t.Run(state, func(t *testing.T) {
			m := NewMessagesCmp(&app.App{}).(*messagesCmp)
			m.SetSize(80, 30)
			m.messages = []message.Message{{ID: "a", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Diagnostic stays visible", CitationStatus: state}}}}
			m.renderView()
			require.Contains(t, timelineText(m), "Diagnostic stays visible")
			require.Contains(t, timelineText(m), citationStatusLabel(state))
		})
	}
}

func TestQuestionAnswersRemainVisibleBetweenCollapsedCallsAndAcknowledgment(t *testing.T) {
	for _, result := range []message.ToolResult{
		{Name: "ask_user_question", ToolCallID: "question", Content: `{"user_response":"Agora não"}`},
		{Name: "ask_user_questions", ToolCallID: "question", Content: `{"user_response":"{\"answers\":[{\"question_id\":\"q1\",\"choices\":[\"Agora não\"]}]}"}`},
	} {
		m := NewMessagesCmp(&app.App{}).(*messagesCmp)
		m.SetSize(80, 40)
		input := `{"question":"Proceed?"}`
		if result.Name == "ask_user_questions" {
			input = `{"questions":[{"question":"Proceed?","type":"single_choice","choices":["Sim","Agora não"]}]}`
		}
		m.messages = []message.Message{
			{ID: "diagnosis", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Public diagnosis"}, message.ToolCall{ID: "question", Name: result.Name, Input: input, Finished: true}}},
			{ID: "answer", Role: message.Tool, Parts: []message.ContentPart{result}},
			{ID: "ack", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Perfeito, fica o diagnóstico."}}},
		}
		// Stored tool results historically omit Name; resolve via the original call.
		result.Name = ""
		m.messages[1].Parts = []message.ContentPart{result}
		m.renderView()
		view := timelineText(m)
		require.Contains(t, view, "Agora não")
		require.NotContains(t, view, "user_response")
		require.Less(t, strings.Index(view, "Public diagnosis"), strings.Index(view, "Agora não"))
		require.Less(t, strings.Index(view, "Agora não"), strings.Index(view, "Perfeito"))
		m.Update(ToggleProcessMsg{})
		require.Equal(t, 1, strings.Count(timelineText(m), "Public diagnosis"))
	}
}

func TestProcessFirstToggleUsesEachRenderedBlockState(t *testing.T) {
	for _, expanded := range []bool{false, true} {
		m := renderFixture(0)
		id := "u\x00process:second"
		m.hits = []transcriptHit{{kind: "process", id: id, expanded: expanded}}
		m.toggleProcess(id)
		require.Equal(t, !expanded, m.expanded[id], "first click must invert what was actually displayed")
	}
}
