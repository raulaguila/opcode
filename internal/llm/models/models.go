package models

import "maps"

type (
	ModelID       string
	ModelProvider string
)

type Model struct {
	API                 string        `json:"api,omitempty" mapstructure:"api"`
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
	ContextWindow       int64         `json:"context_window" mapstructure:"context_window"`
	DefaultMaxTokens    int64         `json:"default_max_tokens" mapstructure:"default_max_tokens"`
	CanReason           bool          `json:"can_reason" mapstructure:"can_reason"`
	SupportsAttachments bool          `json:"supports_attachments" mapstructure:"supports_attachments"`
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
}
