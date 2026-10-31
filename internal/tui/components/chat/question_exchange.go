package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/tui/styles"
	"github.com/raulaguila/opcode/internal/tui/theme"
)

type answeredQuestion struct{ question, answer string }

func answeredQuestions(item message.Message, owners map[string]toolOwner, history []message.Message) []answeredQuestion {
	if item.Role != message.Tool {
		return nil
	}
	var exchanges []answeredQuestion
	for _, result := range item.ToolResults() {
		name := result.Name
		if name == "" {
			name = owners[result.ToolCallID].name
		}
		if (name != "ask_user_question" && name != "ask_user_questions") || result.IsError {
			continue
		}
		var decoded struct {
			Response string `json:"user_response"`
		}
		if json.Unmarshal([]byte(result.Content), &decoded) != nil || decoded.Response == "" {
			continue
		}
		question := originalQuestion(item.ID, result.ToolCallID, history)
		for _, pair := range questionExchangePairs(question, decoded.Response) {
			pair.question = cleanQuestionText(pair.question)
			pair.answer = cleanQuestionText(pair.answer)
			exchanges = append(exchanges, pair)
		}
	}
	return exchanges
}

func cleanQuestionText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(tools.RedactCredentials(text)))
}

// Resolve only a preceding call in the same human turn. Missing historical
// pages or malformed calls keep the real answer without inventing a question.
func originalQuestion(resultID, callID string, history []message.Message) interaction.Question {
	before := -1
	for i, item := range history {
		if item.ID == resultID {
			before = i
			break
		}
	}
	for i := before - 1; i >= 0; i-- {
		item := history[i]
		if item.Role == message.User && !item.IsApplication() {
			break
		}
		if question, ok := questionInMessage(item, callID); ok {
			return question
		}
	}
	return interaction.Question{}
}

func questionInMessage(item message.Message, callID string) (interaction.Question, bool) {
	if item.Role == message.Assistant {
		for _, call := range item.ToolCalls() {
			if call.ID == callID && (call.Name == "ask_user_question" || call.Name == "ask_user_questions") {
				question, err := interaction.DecodeQuestion(call.Input)
				return question, err == nil
			}
		}
	}
	return interaction.Question{}, false
}

func questionExchangePairs(question interaction.Question, text string) []answeredQuestion {
	if len(question.Questions) == 0 && strings.TrimSpace(question.Question) != "" {
		return []answeredQuestion{{question.Question, text}}
	}
	var batch interaction.AnswerBatch
	if json.Unmarshal([]byte(text), &batch) == nil && len(batch.Answers) > 0 {
		return formatBatchQuestionExchange(question, batch.Answers)
	}
	return []answeredQuestion{{question.Question, text}}
}

func formatBatchQuestionExchange(question interaction.Question, answers []interaction.AnswerItem) []answeredQuestion {
	var pairs []answeredQuestion
	used := map[int]bool{}
	for i, item := range question.Questions {
		id := item.ID
		if id == "" {
			id = fmt.Sprintf("q%d", i+1)
		}
		for j, answer := range answers {
			if answer.QuestionID == id && !used[j] {
				pairs = append(pairs, answeredQuestion{item.Question, answerItemText(answer)})
				used[j] = true
				break
			}
		}
	}
	for i, answer := range answers {
		if !used[i] {
			pairs = append(pairs, answeredQuestion{"", answerItemText(answer)})
		}
	}
	return pairs
}

func answerItemText(answer interaction.AnswerItem) string {
	if len(answer.Choices) > 0 {
		return strings.Join(answer.Choices, ", ")
	}
	return answer.Text
}

func renderAnsweredQuestions(pairs []answeredQuestion, width int) string {
	t := theme.CurrentTheme()
	frame := styles.BaseStyle().Width(max(1, width)).BorderLeft(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(t.Accent())
	inner := max(1, width-frame.GetHorizontalFrameSize())
	var rows []string
	for _, pair := range pairs {
		var lines []string
		if pair.question != "" {
			question := config.Text("Pergunta do agente · ", "Agent question · ") + pair.question
			lines = append(lines, styles.BaseStyle().Foreground(t.TextMuted()).Render(ansi.Wrap(question, inner, " ")))
		}
		answer := config.Text("Você respondeu: ", "You answered: ") + pair.answer
		lines = append(lines, styles.BaseStyle().Foreground(t.Text()).Bold(true).Render(ansi.Wrap(answer, inner, " ")))
		rows = append(rows, strings.Join(lines, "\n"))
	}
	return frame.Render(strings.Join(rows, "\n\n"))
}
