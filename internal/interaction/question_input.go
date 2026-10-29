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
	normalize := func(object map[string]json.RawMessage) error {
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
	if err := normalize(object); err != nil {
		return q, err
	}
	if rawItems, ok := object["questions"]; ok {
		var items []map[string]json.RawMessage
		if json.Unmarshal(rawItems, &items) != nil || len(items) > 5 {
			return q, fmt.Errorf("invalid question: questions must be an array of at most five objects")
		}
		for _, item := range items {
			if item == nil {
				return q, fmt.Errorf("invalid question: each question must be an object")
			}
			if err := normalize(item); err != nil {
				return q, err
			}
		}
		object["questions"], _ = json.Marshal(items)
	}
	normalized, _ := json.Marshal(object)
	if json.Unmarshal(normalized, &q) != nil {
		return q, fmt.Errorf("invalid question: question/explanation must be strings; choices must be an array of strings")
	}
	return q, nil
}
