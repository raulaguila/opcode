package transcript

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/message"
)

type Decision struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// AnsweredQuestions projects existing tool records; it never adds messages or grants consent.
func AnsweredQuestions(call message.ToolCall, result message.ToolResult) []Decision {
	if call.Name != "ask_user_question" && call.Name != "ask_user_questions" || result.IsError || result.ToolCallID != call.ID {
		return nil
	}
	q, err := interaction.DecodeQuestion(call.Input)
	if err != nil {
		return nil
	}
	var response struct {
		UserResponse string `json:"user_response"`
	}
	if json.Unmarshal([]byte(result.Content), &response) != nil || strings.TrimSpace(response.UserResponse) == "" {
		return nil
	}
	if len(q.Questions) == 0 {
		if strings.TrimSpace(q.Question) == "" {
			return nil
		}
		return []Decision{{q.Question, response.UserResponse}}
	}
	// Ask assigns IDs and yes/no choices before saving the answer.
	for i := range q.Questions {
		if q.Questions[i].ID == "" {
			q.Questions[i].ID = fmt.Sprintf("q%d", i+1)
		}
		if q.Questions[i].Type == "yes_no" {
			q.Questions[i].Choices = []string{"yes", "no"}
		}
	}
	var batch interaction.AnswerBatch
	if json.Unmarshal([]byte(response.UserResponse), &batch) != nil || interaction.ValidateAnswers(q, batch.Answers) != nil {
		return nil
	}
	answers := map[string]string{}
	for _, answer := range batch.Answers {
		parts := append([]string{}, answer.Choices...)
		if answer.Text != "" {
			parts = append(parts, answer.Text)
		}
		answers[answer.QuestionID] = strings.Join(parts, "; ")
	}
	var out []Decision
	for _, item := range q.Questions {
		out = append(out, Decision{item.Question, answers[item.ID]})
	}
	return out
}
