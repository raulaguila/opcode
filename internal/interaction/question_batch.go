package interaction

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// QuestionItem describes a decision, never an authorization. IDs are local to a batch.
type QuestionItem struct {
	ID          string   `json:"id,omitempty"`
	Question    string   `json:"question"`
	Explanation string   `json:"explanation,omitempty"`
	Type        string   `json:"type"`
	Choices     []string `json:"choices,omitempty"`
}
type AnswerItem struct {
	QuestionID string   `json:"question_id"`
	Choices    []string `json:"choices,omitempty"`
	Text       string   `json:"text,omitempty"`
}
type AnswerBatch struct {
	Answers []AnswerItem `json:"answers"`
}

func cloneQuestion(q Question) Question {
	q.Choices = slices.Clone(q.Choices)
	q.Questions = slices.Clone(q.Questions)
	for i := range q.Questions {
		q.Questions[i].Choices = slices.Clone(q.Questions[i].Choices)
	}
	return q
}

func normalizeBatch(q Question) (Question, error) {
	if len(q.Questions) > 5 || len(q.Question) > 500 || len(q.Explanation) > 1000 || len(q.Choices) > 0 {
		return q, fmt.Errorf("a batch accepts at most five questions; top-level choices cannot be mixed with a batch")
	}
	q = cloneQuestion(q)
	seen := map[string]bool{}
	for i := range q.Questions {
		item := &q.Questions[i]
		if item.ID == "" {
			item.ID = fmt.Sprintf("q%d", i+1)
		}
		if strings.TrimSpace(item.ID) == "" || len(item.ID) > 64 || strings.ContainsAny(item.ID, "\r\n\t") || seen[item.ID] {
			return q, fmt.Errorf("question IDs must be unique and up to 64 bytes")
		}
		seen[item.ID] = true
		if err := normalizeQuestionItem(item); err != nil {
			return q, err
		}
	}
	return q, nil
}

// ValidateAnswers binds every response to its actual question and declared choices.
// A free-text alternative is allowed for choice questions; it grants no permissions.
func ValidateAnswers(q Question, answers []AnswerItem) error {
	if len(answers) != len(q.Questions) || len(answers) == 0 {
		return fmt.Errorf("answer every question before sending")
	}
	byID := map[string]QuestionItem{}
	for _, item := range q.Questions {
		byID[item.ID] = item
	}
	seen := map[string]bool{}
	for _, answer := range answers {
		item, ok := byID[answer.QuestionID]
		if !ok || seen[answer.QuestionID] {
			return fmt.Errorf("unknown or repeated question ID")
		}
		seen[answer.QuestionID] = true
		if err := validateAnswerItem(item, answer); err != nil {
			return err
		}
	}
	return nil
}

func (s *Questions) AnswerBatch(session, id string, answers []AnswerItem) error {
	raw, err := json.Marshal(AnswerBatch{Answers: answers})
	if err != nil {
		return err
	}
	return s.Answer(session, id, string(raw))
}

func parseAnswerBatch(text string) (AnswerBatch, error) {
	var batch AnswerBatch
	decoder := json.NewDecoder(bytes.NewBufferString(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&batch); err != nil {
		return batch, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return batch, fmt.Errorf("trailing answer data")
	}
	return batch, nil
}

func normalizeQuestionItem(item *QuestionItem) error {
	if strings.TrimSpace(item.Question) == "" || len(item.Question) > 500 || len(item.Explanation) > 1000 {
		return fmt.Errorf("question text is required (up to 500 bytes); explanation up to 1000 bytes")
	}
	switch item.Type {
	case "free_text":
		if len(item.Choices) > 0 {
			return fmt.Errorf("free_text does not accept choices")
		}
	case "yes_no":
		if len(item.Choices) > 0 {
			return fmt.Errorf("yes_no uses fixed yes/no choices")
		}
		item.Choices = []string{"yes", "no"}
	case "single_choice", "multi_choice":
		if len(item.Choices) < 2 || len(item.Choices) > 5 {
			return fmt.Errorf("choice questions require two to five choices")
		}
	default:
		return fmt.Errorf("unsupported question type")
	}
	choices := map[string]bool{}
	for _, c := range item.Choices {
		if strings.TrimSpace(c) == "" || len(c) > 300 || choices[c] {
			return fmt.Errorf("choices must be distinct and contain up to 300 bytes")
		}
		choices[c] = true
	}
	return nil
}

func validateAnswerItem(item QuestionItem, answer AnswerItem) error {
	if len(answer.Text) > 4000 || (strings.TrimSpace(answer.Text) == "" && len(answer.Choices) == 0) {
		return fmt.Errorf("each answer requires a selection or up to 4000 bytes of text")
	}
	if len(answer.Choices) > 0 && strings.TrimSpace(answer.Text) != "" {
		return fmt.Errorf("choose options or enter a free-text alternative")
	}
	if len(answer.Choices) > 5 || (item.Type != "multi_choice" && len(answer.Choices) > 1) {
		return fmt.Errorf("too many selected choices")
	}
	if item.Type == "free_text" && len(answer.Choices) > 0 {
		return fmt.Errorf("free_text requires text")
	}
	selected := map[string]bool{}
	for _, choice := range answer.Choices {
		if !slices.Contains(item.Choices, choice) || selected[choice] {
			return fmt.Errorf("unknown or repeated choice")
		}
		selected[choice] = true
	}
	return nil
}
