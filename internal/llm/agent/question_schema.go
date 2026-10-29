package agent

func questionParameters() map[string]any {
	text := map[string]any{"type": "string", "maxLength": 500}
	explanation := map[string]any{"type": "string", "maxLength": 1000}
	choices := map[string]any{"type": "array", "maxItems": 5, "items": map[string]any{"type": "string", "maxLength": 300}}
	return map[string]any{
		"question": text, "explanation": explanation, "choices": choices,
	}
}

func questionBatchParameters() map[string]any {
	base := questionParameters()
	text, explanation, choices := base["question"], base["explanation"], base["choices"]
	return map[string]any{
		"questions": map[string]any{"type": "array", "minItems": 1, "maxItems": 5, "items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"question", "type"},
			"properties": map[string]any{"id": map[string]any{"type": "string", "maxLength": 64}, "question": text, "explanation": explanation, "choices": choices,
				"type": map[string]any{"type": "string", "enum": []string{"free_text", "yes_no", "single_choice", "multi_choice"}},
			},
		}},
	}
}
