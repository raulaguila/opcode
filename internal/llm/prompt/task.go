package prompt

import "github.com/raulaguila/opcode/internal/llm/models"

func TaskPrompt(provider models.ModelProvider) string { return CoderPrompt(provider) }
