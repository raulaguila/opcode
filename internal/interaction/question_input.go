package interaction

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// DecodeQuestion accepts only one narrow compatibility repair: choices may be a
// string containing a JSON array of strings. It never interprets prose, CSV,
// nested encodings, other fields, commands, or answers as authorization.
func DecodeQuestion(raw string) (Question, error) {
	var q Question
	if len(raw) > 32<<10 {
		return q, fmt.Errorf("invalid question: input exceeds 32 KiB")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &object) != nil || object == nil {
		return q, fmt.Errorf("invalid question: expected a JSON object with question and optional choices")
	}

	if err := normalizeQuestionChoices(object); err != nil {
		return q, err
	}
	if err := normalizeQuestionNotes(object); err != nil {
		return q, err
	}
	if err := normalizeQuestionBatch(object); err != nil {
		return q, err
	}
	normalized, _ := json.Marshal(object)
	if json.Unmarshal(normalized, &q) != nil {
		return q, fmt.Errorf("invalid question: question/explanation must be strings; choices must be an array of strings")
	}
	return q, nil
}

// normalizeQuestionNotes accepts at most one short note per choice.
func normalizeQuestionNotes(object map[string]json.RawMessage) error {
	value, ok := object["notes"]
	if !ok {
		return nil
	}
	var notes, choices []string
	_ = json.Unmarshal(object["choices"], &choices)
	if json.Unmarshal(value, &notes) != nil || len(notes) > len(choices) {
		return fmt.Errorf("invalid question: notes must be an array of strings, at most one per choice")
	}
	for _, note := range notes {
		if len(note) > 160 {
			return fmt.Errorf("invalid question: each note must contain at most 160 bytes")
		}
	}
	return nil
}

func normalizeQuestionChoices(object map[string]json.RawMessage) error {
	value, ok := object["choices"]
	if !ok {
		return nil
	}
	value = bytes.TrimSpace(value)
	if len(value) > 0 && value[0] == '"' {
		var encoded string
		if json.Unmarshal(value, &encoded) != nil {
			return fmt.Errorf("invalid question: choices must be an array of strings, e.g. [\"Yes\",\"No\"]")
		}
		value = bytes.TrimSpace([]byte(encoded))
		if len(value) == 0 || value[0] != '[' {
			return fmt.Errorf("invalid question: choices must be an array of strings, e.g. [\"Yes\",\"No\"]")
		}
	}
	var choices []string
	if json.Unmarshal(value, &choices) != nil || len(choices) > 5 {
		return fmt.Errorf("invalid question: choices must contain at most five strings, e.g. [\"Yes\",\"No\"]")
	}
	for _, choice := range choices {
		if len(bytes.TrimSpace([]byte(choice))) == 0 || len(choice) > 300 {
			return fmt.Errorf("invalid question: each choice must contain 1–300 bytes")
		}
	}
	object["choices"], _ = json.Marshal(choices)
	return nil
}

func normalizeQuestionBatch(object map[string]json.RawMessage) error {
	if rawItems, ok := object["questions"]; ok {
		var items []map[string]json.RawMessage
		if json.Unmarshal(rawItems, &items) != nil || len(items) > 5 {
			return fmt.Errorf("invalid question: questions must be an array of at most five objects")
		}
		for _, item := range items {
			if item == nil {
				return fmt.Errorf("invalid question: each question must be an object")
			}
			if err := normalizeQuestionChoices(item); err != nil {
				return err
			}
			if err := normalizeQuestionNotes(item); err != nil {
				return err
			}
		}
		object["questions"], _ = json.Marshal(items)
	}
	return nil
}
