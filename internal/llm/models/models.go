package models

import (
	"maps"
)

type (
	ModelID       string
	ModelProvider string
)

type Model struct {
	API                 string        `json:"api,omitempty" mapstructure:"api"`
	ReasoningLevels     []string      `json:"reasoning_levels,omitempty" mapstructure:"reasoning_levels"`
	ThinkingMode        string        `json:"thinking_mode,omitempty" mapstructure:"thinking_mode"`
	ID                  ModelID       `json:"id" mapstructure:"id"`
	Name                string        `json:"name" mapstructure:"name"`
	Provider            ModelProvider `json:"provider" mapstructure:"provider"`
	ProviderProfile     string        `json:"provider_profile,omitempty" mapstructure:"provider_profile"`
	APIModel            string        `json:"api_model" mapstructure:"api_model"`
	CostPer1MIn         float64       `json:"cost_per_1m_in" mapstructure:"cost_per_1m_in"`
	CostPer1MOut        float64       `json:"cost_per_1m_out" mapstructure:"cost_per_1m_out"`
	CostPer1MInCached   float64       `json:"cost_per_1m_in_cached" mapstructure:"cost_per_1m_in_cached"`
	CostPer1MOutCached  float64       `json:"cost_per_1m_out_cached" mapstructure:"cost_per_1m_out_cached"`
	ContextEstimated    bool          `json:"context_estimated,omitempty" mapstructure:"context_estimated"`
	ContextAuto         bool          `json:"context_auto,omitempty" mapstructure:"context_auto"`
	ContextSource       string        `json:"context_source,omitempty" mapstructure:"context_source"`
	ContextWindow       int64         `json:"context_window,omitempty" mapstructure:"context_window"`
	DefaultMaxTokens    int64         `json:"default_max_tokens" mapstructure:"default_max_tokens"`
	CanReason           bool          `json:"can_reason" mapstructure:"can_reason"`
	SupportsAttachments bool          `json:"supports_attachments" mapstructure:"supports_attachments"`
	// Nil means the provider did not report tool capability. Metadata is not an
	// inference test and never changes tool authorization.
	SupportsTools *bool `json:"supports_tools,omitempty" mapstructure:"supports_tools"`
}

// A missing context is a request for provider metadata. ContextAuto also accepts
// snapshots written by older versions, which included the discovered window.
func (m Model) UsesAutomaticContext() bool { return m.ContextAuto || m.ContextWindow == 0 }

var builtinModels map[ModelID]Model

func BuiltinMetadata(reference Model) (Model, bool) {
	if model, ok := builtinModels[reference.ID]; ok && model.Provider == reference.Provider && model.APIModel == reference.APIModel {
		return model, true
	}
	for _, model := range builtinModels {
		if model.Provider == reference.Provider && model.APIModel == reference.APIModel {
			return model, true
		}
	}
	return Model{}, false
}

// Model IDs
const ( // GEMINI
	// Bedrock
	BedrockClaude37Sonnet ModelID = "bedrock.claude-3.7-sonnet"
)

const (
	ProviderBedrock ModelProvider = "bedrock"
	// ForTests
	ProviderMock ModelProvider = "__mock"
)

// Providers in order of popularity
var ProviderPopularity = map[ModelProvider]int{
	ProviderCopilot:    1,
	ProviderAnthropic:  2,
	ProviderOpenAI:     3,
	ProviderGemini:     4,
	ProviderGROQ:       5,
	ProviderOpenRouter: 6,
	ProviderBedrock:    7,
	ProviderAzure:      8,
	ProviderVertexAI:   9,
}

var SupportedModels = map[ModelID]Model{
	//
	// // GEMINI
	// GEMINI25: {
	// 	ID:                 GEMINI25,
	// 	Name:               "Gemini 2.5 Pro",
	// 	Provider:           ProviderGemini,
	// 	APIModel:           "gemini-2.5-pro-exp-03-25",
	// 	CostPer1MIn:        0,
	// 	CostPer1MInCached:  0,
	// 	CostPer1MOutCached: 0,
	// 	CostPer1MOut:       0,
	// },
	//
	// GRMINI20Flash: {
	// 	ID:                 GRMINI20Flash,
	// 	Name:               "Gemini 2.0 Flash",
	// 	Provider:           ProviderGemini,
	// 	APIModel:           "gemini-2.0-flash",
	// 	CostPer1MIn:        0.1,
	// 	CostPer1MInCached:  0,
	// 	CostPer1MOutCached: 0.025,
	// 	CostPer1MOut:       0.4,
	// },
	//
	// // Bedrock
	BedrockClaude37Sonnet: {
		ID:                 BedrockClaude37Sonnet,
		Name:               "Bedrock: Claude 3.7 Sonnet",
		Provider:           ProviderBedrock,
		APIModel:           "anthropic.claude-3-7-sonnet-20250219-v1:0",
		CostPer1MIn:        3.0,
		CostPer1MInCached:  3.75,
		CostPer1MOutCached: 0.30,
		CostPer1MOut:       15.0,
	},
}

func init() {
	maps.Copy(SupportedModels, AnthropicModels)
	maps.Copy(SupportedModels, OpenAIModels)
	maps.Copy(SupportedModels, GeminiModels)
	maps.Copy(SupportedModels, GroqModels)
	maps.Copy(SupportedModels, AzureModels)
	maps.Copy(SupportedModels, OpenRouterModels)
	maps.Copy(SupportedModels, XAIModels)
	maps.Copy(SupportedModels, VertexAIGeminiModels)
	maps.Copy(SupportedModels, CopilotModels)
	builtinModels = maps.Clone(SupportedModels)
}

// ReasoningChoices exposes only explicit, supported effort values for adapters
// which transmit the effort field. Capability names are never inferred from IDs.
func (m Model) ReasoningChoices() []string {
	if !m.CanReason || m.API != "" && m.API != "chat_completions" && m.API != "responses" {
		return nil
	}
	switch m.Provider {
	case ProviderOpenAI, ProviderLocal, ProviderAzure, ProviderGROQ, ProviderOpenRouter, ProviderXAI:
	default:
		return nil
	}
	choices := []string{}
	seen := map[string]bool{}
	for _, level := range m.ReasoningLevels {
		switch level {
		case "none", "minimal", "low", "medium", "high", "xhigh":
			if !seen[level] {
				choices = append(choices, level)
				seen[level] = true
			}
		}
	}
	return choices
}
