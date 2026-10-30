package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/theme"
	"github.com/stretchr/testify/require"
)

func exchangeFixture(input, response string) *messagesCmp {
	m := NewMessagesCmp(&app.App{}).(*messagesCmp)
	m.SetSize(80, 40)
	raw, _ := json.Marshal(map[string]string{"user_response": response})
	m.messages = []message.Message{
		{ID: "diagnosis", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Diagnosis stays visible"}, message.ToolCall{ID: "question", Name: "ask_user_question", Input: input, Finished: true}}},
		{ID: "answer", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "question", Content: string(raw)}}},
		{ID: "ack", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Changes can wait"}}},
	}
	m.renderView()
	return m
}

func TestQuestionExchangeKeepsQuestionAndAnswerTogetherWhenCollapsedAndReloaded(t *testing.T) {
	m := exchangeFixture(`{"question":"Implement the improvements?","choices":"[\"Yes\",\"Not now\"]"}`, "Not now")
	for range 2 {
		view := timelineText(m)
		require.Contains(t, view, "Agent question")
		require.Contains(t, view, "Implement the improvements?")
		require.Contains(t, view, "You answered: Not now")
		require.Less(t, strings.Index(view, "Implement the improvements?"), strings.Index(view, "You answered:"))
		require.Less(t, strings.Index(view, "You answered:"), strings.Index(view, "Changes can wait"))
		require.Len(t, m.messages, 3, "presentation must not append stored messages")
		m.resetRenderCache()
		m.renderView()
	}
	m.Update(ToggleProcessMsg{})
	require.Contains(t, timelineText(m), "You answered: Not now")
}

func TestQuestionExchangeMapsBatchByIDIncludingGeneratedIDs(t *testing.T) {
	input := `{"questions":[{"question":"Which area?","type":"single_choice","choices":["UI","Context"]},{"id":"reason","question":"Why wait?","type":"free_text"}]}`
	response := `{"answers":[{"question_id":"reason","text":"Need a review"},{"question_id":"q1","choices":["UI"]}]}`
	m := exchangeFixture(input, response)
	view := timelineText(m)
	require.Contains(t, view, "Which area?")
	require.Contains(t, view, "You answered: UI")
	require.Contains(t, view, "Why wait?")
	require.Contains(t, view, "You answered: Need a review")
	require.Less(t, strings.Index(view, "Which area?"), strings.Index(view, "You answered: UI"))
	require.Less(t, strings.Index(view, "You answered: UI"), strings.Index(view, "Why wait?"))
	require.Less(t, strings.Index(view, "Why wait?"), strings.Index(view, "You answered: Need a review"))
}

func TestQuestionExchangeKeepsJSONFreeTextAsTheActualAnswer(t *testing.T) {
	response := `{"answers":[{"question_id":"example","text":"literal JSON"}]}`
	m := exchangeFixture(`{"question":"Which JSON should I use?"}`, response)
	view := timelineText(m)
	require.Contains(t, view, "Which JSON should I use?")
	require.Contains(t, questionAnswer(m.messages[1], m.renderCache.toolOwners, m.messages), response)
}

func TestQuestionExchangeMissingOrMalformedSourceDoesNotInventQuestion(t *testing.T) {
	for _, input := range []string{"{invalid", `{}`, `{"questions":[]}`} {
		m := exchangeFixture(input, "My custom answer")
		require.Contains(t, timelineText(m), "You answered: My custom answer")
		require.NotContains(t, timelineText(m), "Agent question")
	}
	m := exchangeFixture(`{"question":"Unavailable old question?"}`, "Old answer")
	result := m.messages[1].ToolResults()[0]
	result.Name = "ask_user_question"
	m.messages = []message.Message{m.messages[1]}
	m.messages[0].Parts = []message.ContentPart{result}
	m.resetRenderCache()
	m.renderView()
	require.Contains(t, timelineText(m), "You answered: Old answer")
	require.NotContains(t, timelineText(m), "Unavailable old question?")
}

func TestQuestionExchangeFailedResultsDoNotInventAnsweredQuestion(t *testing.T) {
	m := exchangeFixture(`{"question":"Proceed?"}`, "Not now")
	result := m.messages[1].ToolResults()[0]
	result.IsError = true
	m.messages[1].Parts = []message.ContentPart{result}
	m.resetRenderCache()
	m.renderView()
	require.NotContains(t, timelineText(m), "You answered:")
	require.NotContains(t, timelineText(m), "Agent question")
}

func TestQuestionExchangeStripsTerminalControlsFromHistoricalQuestionAndAnswer(t *testing.T) {
	m := exchangeFixture(`{"question":"Proceed?\u001b[2J\u0007"}`, "Not now\x1b[2J\x07")
	view := strings.Join(m.transcriptLines, "\n")
	require.NotContains(t, view, "\x1b[2J")
	require.NotContains(t, view, "\x07")
	require.Contains(t, ansi.Strip(view), "Proceed?")
	require.Contains(t, ansi.Strip(view), "You answered: Not now")
}

func TestQuestionExchangeUsesDistinctPipeAndKeepsLabelsWithoutColor(t *testing.T) {
	previous, previousDark, previousTheme := lipgloss.ColorProfile(), lipgloss.HasDarkBackground(), theme.CurrentThemeName()
	t.Cleanup(func() {
		lipgloss.SetColorProfile(previous)
		lipgloss.SetHasDarkBackground(previousDark)
		_ = theme.PreviewTheme(previousTheme)
	})
	for _, dark := range []bool{false, true} {
		lipgloss.SetHasDarkBackground(dark)
		for _, name := range []string{"catppuccin", "gruvbox"} {
			require.NoError(t, theme.PreviewTheme(name))
			lipgloss.SetColorProfile(termenv.TrueColor)
			m := exchangeFixture(`{"question":"A question?"}`, "A free answer")
			var exchange uiMessage
			for _, item := range m.uiMessages {
				if item.ID == "answer:answer" {
					exchange = item
				}
			}
			require.NotEmpty(t, exchange.content)
			body := "Agent question\n\nA question?\n\nYou answered: A free answer"
			require.NotEqual(t, renderMessage(body, true, false, 80), exchange.content)
			require.NotEqual(t, renderMessage(body, false, false, 80), exchange.content)
			lipgloss.SetColorProfile(termenv.Ascii)
			m.resetRenderCache()
			m.renderView()
			require.Contains(t, timelineText(m), "Agent question")
			require.Contains(t, timelineText(m), "You answered: A free answer")
			for _, line := range m.transcriptLines {
				require.LessOrEqual(t, ansi.StringWidth(line), 80)
			}
		}
	}
}
