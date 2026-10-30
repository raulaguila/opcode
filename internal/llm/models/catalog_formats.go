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
	for range 3 {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			break
		}
		if raw[0] == '[' {
			return catalogArrayPage(raw, page, "array")
		}
		object, err := catalogEnvelope(raw, &page)
		if err != nil {
			return page, err
		}
		if object == nil {
			break
		}
		if array := catalogEnvelopeValue(object, []string{"data", "models", "items", "result"}, '['); array != nil {
			return catalogArrayPage(array, page, "entries")
		}
		raw = catalogEnvelopeValue(object, []string{"data", "result"}, '{')
		if raw == nil {
			break
		}
	}
	return page, fmt.Errorf("unrecognized model catalog envelope")
}

func normalizeCatalogPage(page catalogPage) catalogPage {
	result := make([]json.RawMessage, 0, min(len(page.Data), 5000))
	for _, raw := range page.Data {
		if len(result) >= 5000 {
			break
		}
		if item, ok := normalizeCatalogItem(raw); ok {
			result = append(result, item)
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

func normalizeCatalogItem(raw json.RawMessage) (json.RawMessage, bool) {
	var item map[string]json.RawMessage
	if json.Unmarshal(raw, &item) != nil || item == nil {
		var id string
		if json.Unmarshal(raw, &id) != nil {
			return nil, false
		}
		item = map[string]json.RawMessage{"id": raw}
	}
	objects := catalogObjects(item)
	id := catalogText(item, "id", "model", "model_id", "model_name", "name")
	if !validCatalogID(id) {
		return nil, false
	}
	item["id"], _ = json.Marshal(id)
	item["display_name"], _ = json.Marshal(catalogText(item, "display_name", "displayName"))
	item["name"], _ = json.Marshal(catalogText(item, "name"))
	contextWindow := catalogLimits(objects, "loaded_context_length", "context_length", "context_window", "max_model_len", "max_context_length", "contextLength", "contextWindow", "maxModelLen", "maxContextLength", "context_size", "contextSize", "n_ctx", "max_seq_len", "max_sequence_length", "max_context_tokens")
	input := catalogLimits(objects, "max_input_tokens", "input_token_limit", "inputTokenLimit", "maxInputTokens")
	if contextWindow == 0 {
		contextWindow = input
	}
	output := catalogLimits(objects, "max_output_tokens", "output_token_limit", "outputTokenLimit", "maxOutputTokens", "max_completion_tokens", "max_tokens")
	for key, value := range map[string]int64{
		"context_length": contextWindow, "max_input_tokens": input, "max_tokens": output,
		"max_context_length":    catalogTokenLimit(item["max_context_length"]),
		"loaded_context_length": catalogTokenLimit(item["loaded_context_length"]),
	} {
		item[key], _ = json.Marshal(value)
	}

	canonical, err := json.Marshal(item)
	return canonical, err == nil
}
func catalogText(item map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var value string
		if json.Unmarshal(item[key], &value) == nil && value != "" {
			return value
		}
	}
	return ""
}
func catalogLimits(objects []map[string]json.RawMessage, keys ...string) int64 {
	for _, object := range objects {
		for _, key := range keys {
			if value := catalogTokenLimit(object[key]); value > 0 {
				return value
			}
		}
	}
	return 0
}

func catalogObjects(item map[string]json.RawMessage) []map[string]json.RawMessage {
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

	return objects
}

func catalogEnvelope(raw []byte, page *catalogPage) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, nil
	}
	var cursor struct {
		HasMore bool   `json:"has_more"`
		LastID  string `json:"last_id"`
		Next    string `json:"nextPageToken"`
	}
	if json.Unmarshal(raw, &cursor) != nil {
		return nil, fmt.Errorf("invalid catalog pagination")
	}
	if cursor.HasMore || cursor.LastID != "" || cursor.Next != "" {
		page.HasMore, page.LastID, page.Next = cursor.HasMore, cursor.LastID, cursor.Next
	}
	return object, nil
}
func catalogEnvelopeValue(object map[string]json.RawMessage, keys []string, prefix byte) json.RawMessage {
	for _, key := range keys {
		value := bytes.TrimSpace(object[key])
		if len(value) > 0 && value[0] == prefix {
			return value
		}
	}
	return nil
}
func catalogArrayPage(raw []byte, page catalogPage, label string) (catalogPage, error) {
	if json.Unmarshal(raw, &page.Data) != nil {
		return page, fmt.Errorf("invalid catalog %s", label)
	}
	return normalizeCatalogPage(page), nil
}
