package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type catalogPage struct {
	Data    []json.RawMessage
	HasMore bool   `json:"has_more"`
	LastID  string `json:"last_id"`
	Next    string `json:"nextPageToken"`
}

// decodeCatalogPage accepts explicit catalog envelopes, with bounded wrapper
// depth. It does not recursively search arbitrary provider data for numbers.
func decodeCatalogPage(raw []byte) (catalogPage, error) {
	var page catalogPage
	for depth := 0; depth < 3; depth++ {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			break
		}
		if raw[0] == '[' {
			if err := json.Unmarshal(raw, &page.Data); err != nil {
				return page, fmt.Errorf("invalid catalog array")
			}
			return normalizeCatalogPage(page), nil
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil || object == nil {
			break
		}
		var cursor struct {
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
			Next    string `json:"nextPageToken"`
		}
		if json.Unmarshal(raw, &cursor) != nil {
			return page, fmt.Errorf("invalid catalog pagination")
		}
		if cursor.HasMore || cursor.LastID != "" || cursor.Next != "" {
			page.HasMore, page.LastID, page.Next = cursor.HasMore, cursor.LastID, cursor.Next
		}
		for _, key := range []string{"data", "models", "items", "result"} {
			value := bytes.TrimSpace(object[key])
			if len(value) > 0 && value[0] == '[' {
				if json.Unmarshal(value, &page.Data) != nil {
					return page, fmt.Errorf("invalid catalog entries")
				}
				return normalizeCatalogPage(page), nil
			}
		}
		var wrapped json.RawMessage
		for _, key := range []string{"data", "result"} {
			value := bytes.TrimSpace(object[key])
			if len(value) > 0 && value[0] == '{' {
				wrapped = value
				break
			}
		}
		if wrapped == nil {
			break
		}
		raw = wrapped
	}
	return page, fmt.Errorf("unrecognized model catalog envelope")
}

func normalizeCatalogPage(page catalogPage) catalogPage {
	result := make([]json.RawMessage, 0, min(len(page.Data), 5000))
	for _, raw := range page.Data {
		if len(result) >= 5000 {
			break
		}
		var item map[string]json.RawMessage
		if json.Unmarshal(raw, &item) != nil || item == nil {
			var id string
			if json.Unmarshal(raw, &id) != nil {
				continue
			}
			item = map[string]json.RawMessage{"id": raw}
		}
		objects := []map[string]json.RawMessage{item}
		for _, key := range []string{"openai", "limits", "metadata", "model_info"} {
			var object map[string]json.RawMessage
			if json.Unmarshal(item[key], &object) == nil && object != nil {
				objects = append(objects, object)
			}
		}
		var info map[string]json.RawMessage
		if json.Unmarshal(item["info"], &info) == nil && info != nil {
			objects = append(objects, info)
			var meta map[string]json.RawMessage
			if json.Unmarshal(info["meta"], &meta) == nil && meta != nil {
				objects = append(objects, meta)
			}
		}
		text := func(keys ...string) string {
			for _, key := range keys {
				var value string
				if json.Unmarshal(item[key], &value) == nil && value != "" {
					return value
				}
			}
			return ""
		}
		id := text("id", "model", "model_id", "model_name", "name")
		if !validCatalogID(id) {
			continue
		}
		item["id"], _ = json.Marshal(id)
		item["display_name"], _ = json.Marshal(text("display_name", "displayName"))
		item["name"], _ = json.Marshal(text("name"))
		limits := func(keys ...string) int64 {
			for _, object := range objects {
				for _, key := range keys {
					if value := catalogTokenLimit(object[key]); value > 0 {
						return value
					}
				}
			}
			return 0
		}
		contextWindow := limits("loaded_context_length", "context_length", "context_window", "max_model_len", "max_context_length", "contextLength", "contextWindow", "maxModelLen", "maxContextLength", "context_size", "contextSize", "n_ctx", "max_seq_len", "max_sequence_length", "max_context_tokens")
		input := limits("max_input_tokens", "input_token_limit", "inputTokenLimit", "maxInputTokens")
		if contextWindow == 0 {
			contextWindow = input
		}
		output := limits("max_output_tokens", "output_token_limit", "outputTokenLimit", "maxOutputTokens", "max_completion_tokens", "max_tokens")
		for key, value := range map[string]int64{
			"context_length": contextWindow, "max_input_tokens": input, "max_tokens": output,
			"max_context_length":    catalogTokenLimit(item["max_context_length"]),
			"loaded_context_length": catalogTokenLimit(item["loaded_context_length"]),
		} {
			item[key], _ = json.Marshal(value)
		}
		canonical, err := json.Marshal(item)
		if err == nil {
			result = append(result, canonical)
		}
	}
	page.Data = result
	return page
}

// Numeric strings must be exact positive integers. Units, fractions, booleans,
// negative values and overflow remain unknown rather than becoming guessed limits.
func catalogTokenLimit(raw json.RawMessage) int64 {
	value := strings.TrimSpace(string(raw))
	if strings.HasPrefix(value, `"`) {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return 0
		}
		value = strings.TrimSpace(text)
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number <= 0 {
		return 0
	}
	return number
}
