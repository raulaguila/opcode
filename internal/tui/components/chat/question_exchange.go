package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

func questionAnswer(item message.Message, owners map[string]toolOwner, history []message.Message) string {
	if item.Role != message.Tool {
		return ""
	}
	var exchanges []string
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
		exchanges = append(exchanges, formatQuestionExchange(question, decoded.Response))
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(tools.RedactCredentials(strings.Join(exchanges, "\n\n"))))
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

func formatQuestionExchange(question interaction.Question, text string) string {
	if len(question.Questions) == 0 && strings.TrimSpace(question.Question) != "" {
		return questionExchangePair(question.Question, text)
	}
	var batch interaction.AnswerBatch
	if json.Unmarshal([]byte(text), &batch) == nil && len(batch.Answers) > 0 {
		return formatBatchQuestionExchange(question, batch.Answers)
	}
	return questionExchangePair(question.Question, text)
}

func formatBatchQuestionExchange(question interaction.Question, answers []interaction.AnswerItem) string {
	var pairs []string
	used := map[int]bool{}
	for i, item := range question.Questions {
		id := item.ID
		if id == "" {
			id = fmt.Sprintf("q%d", i+1)
		}
		for j, answer := range answers {
			if answer.QuestionID == id && !used[j] {
				pairs = append(pairs, questionExchangePair(item.Question, answerItemText(answer)))
				used[j] = true
				break
			}
		}
	}
	for i, answer := range answers {
		if !used[i] {
			pairs = append(pairs, questionExchangePair("", answerItemText(answer)))
		}
	}
	return strings.Join(pairs, "\n\n")
}

func answerItemText(answer interaction.AnswerItem) string {
	if len(answer.Choices) > 0 {
		return strings.Join(answer.Choices, ", ")
	}
	return answer.Text
}

func questionExchangePair(question, answer string) string {
	text := config.Text("Você respondeu: ", "You answered: ") + answer
	if strings.TrimSpace(question) != "" {
		text = config.Text("Pergunta do agente", "Agent question") + "\n\n" + question + "\n\n" + text
	}
	return text
}
