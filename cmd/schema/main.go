package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
)

// JSONSchemaType represents a JSON Schema type
type JSONSchemaType struct {
	Type                 string           `json:"type,omitempty"`
	Description          string           `json:"description,omitempty"`
	Properties           map[string]any   `json:"properties,omitempty"`
	Required             []string         `json:"required,omitempty"`
	AdditionalProperties any              `json:"additionalProperties,omitempty"`
	Enum                 []any            `json:"enum,omitempty"`
	Items                map[string]any   `json:"items,omitempty"`
	OneOf                []map[string]any `json:"oneOf,omitempty"`
	AnyOf                []map[string]any `json:"anyOf,omitempty"`
	Default              any              `json:"default,omitempty"`
}

func main() {
	schema := generateSchema()

	// Pretty print the schema
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(schema); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding schema: %v\n", err)
		os.Exit(1)
	}
}

func generateSchema() map[string]any {
	schema := map[string]any{
		"$schema":     "http://json-schema.org/draft-07/schema#",
		"title":       "Opcode Configuration",
		"description": "Configuration schema for the Opcode application",
		"type":        "object",
		"properties":  map[string]any{},
	}

	schema["properties"].(map[string]any)["schemaVersion"] = map[string]any{"type": "integer", "minimum": 0, "maximum": config.SchemaVersion, "description": "Configuration format version; omission preserves legacy compatibility"}

	limits := (config.IndexConfig{}).Defaults()
	indexProperties := map[string]any{"background": map[string]any{"type": "boolean", "default": true}, "embeddings": map[string]any{"type": "object", "description": "Explicit user-global local embedding service. Never downloads weights.", "required": []string{"endpoint", "model"}, "properties": map[string]any{"endpoint": map[string]any{"type": "string", "description": "HTTP loopback IP URL ending /api/embed; redirects/auth/query are rejected."}, "model": map[string]any{"type": "string", "minLength": 1, "maxLength": 160}}}}
	for _, field := range []struct {
		name          string
		min, max, def int64
	}{{"maxFiles", 1, 20000, int64(limits.MaxFiles)}, {"maxFileBytes", 1024, 262144, limits.MaxFileBytes}, {"maxTextBytes", 1024, 20971520, limits.MaxTextBytes}, {"memoryMiB", 4, 128, int64(limits.MemoryMiB)}, {"updateSeconds", 1, 30, int64(limits.UpdateSeconds)}} {
		indexProperties[field.name] = map[string]any{"type": "integer", "anyOf": []map[string]any{{"const": 0}, {"minimum": field.min, "maximum": field.max}}, "default": field.def}
	}
	schema["properties"].(map[string]any)["index"] = map[string]any{"type": "object", "properties": indexProperties, "description": "Derived index resource limits. Embeddings only come from trusted user-global configuration."}

	// Add Data configuration
	schema["properties"].(map[string]any)["data"] = map[string]any{
		"type":        "object",
		"description": "Storage configuration",
		"properties": map[string]any{
			"directory": map[string]any{
				"type":        "string",
				"description": "Directory where application data is stored",
				"default":     ".opcode",
			},
		},
		"required": []string{"directory"},
	}

	// Add working directory
	schema["properties"].(map[string]any)["wd"] = map[string]any{
		"type":        "string",
		"description": "Working directory for the application",
	}

	// Add debug flags
	schema["properties"].(map[string]any)["debug"] = map[string]any{
		"type":        "boolean",
		"description": "Enable debug mode",
		"default":     false,
	}

	schema["properties"].(map[string]any)["debugLSP"] = map[string]any{
		"type":        "boolean",
		"description": "Enable LSP debug mode",
		"default":     false,
	}

	schema["properties"].(map[string]any)["contextPaths"] = map[string]any{
		"type":        "array",
		"description": "Context paths for the application",
		"items": map[string]any{
			"type": "string",
		},
		"default": []string{
			".github/copilot-instructions.md",
			".cursorrules",
			".cursor/rules/",
			"CLAUDE.md",
			"CLAUDE.local.md",
			"opcode.md",
			"opcode.local.md",
			"Opcode.md",
			"Opcode.local.md",
			"OPCODE.md",
			"OPCODE.local.md",
		},
	}

	schema["properties"].(map[string]any)["tui"] = map[string]any{
		"type":        "object",
		"description": "Terminal User Interface configuration",
		"properties": map[string]any{
			"sidebar":       map[string]any{"type": "string", "enum": []string{"auto", "shown", "hidden"}, "default": "hidden"},
			"language":      map[string]any{"type": "string", "enum": []string{"pt", "en"}, "default": "en"},
			"proseWidth":    map[string]any{"type": "integer", "minimum": 0, "maximum": 240, "default": 0, "description": "Prose width in columns; 0 uses 72. Fenced code and tables retain terminal width."},
			"notifications": map[string]any{"type": "string", "enum": []string{"off", "bell", "auto", "native", "osc"}, "default": "off"},
			"keybindings":   map[string]any{"type": "object", "additionalProperties": false, "properties": keybindingSchema()},
			"theme": map[string]any{
				"type":        "string",
				"description": "TUI theme name",
				"default":     "opcode",
				"enum": []string{
					"opcode",
					"catppuccin",
					"dracula",
					"flexoki",
					"gruvbox",
					"monokai",
					"onedark",
					"tokyonight",
					"tron",
				},
			},
		},
	}

	// Add MCP servers
	schema["properties"].(map[string]any)["mcpServers"] = map[string]any{
		"type":        "object",
		"description": "Model Control Protocol server configurations",
		"additionalProperties": map[string]any{
			"type":        "object",
			"description": "MCP server configuration",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Command to execute for the MCP server",
				},
				"env": map[string]any{
					"type":        "array",
					"description": "Environment variables for the MCP server",
					"items": map[string]any{
						"type": "string",
					},
				},
				"args": map[string]any{
					"type":        "array",
					"description": "Command arguments for the MCP server",
					"items": map[string]any{
						"type": "string",
					},
				},
				"type": map[string]any{
					"type":        "string",
					"description": "Type of MCP server",
					"enum":        []string{"stdio", "sse", "http"},
					"default":     "stdio",
				},
				"url": map[string]any{
					"type":        "string",
					"description": "URL for SSE or Streamable HTTP MCP servers",
				},
				"headersEnv": map[string]any{"type": "object", "description": "Header name to environment variable name. Values resolve only in memory.", "additionalProperties": map[string]any{"type": "string"}},
				"envVars":    map[string]any{"type": "object", "description": "Child environment variable to source environment variable name.", "additionalProperties": map[string]any{"type": "string"}},
				"disabled":   map[string]any{"type": "boolean", "default": false},
				"oauth": map[string]any{"type": "object", "description": "Explicit MCP HTTP/SSE OAuth. Tokens are stored separately in a private user auth directory.", "additionalProperties": false, "properties": map[string]any{
					"clientID":        map[string]any{"type": "string", "maxLength": 2048},
					"clientSecretEnv": map[string]any{"type": "string", "description": "Environment variable name, never a resolved client secret"},
					"scopes":          map[string]any{"type": "array", "maxItems": 32, "items": map[string]any{"type": "string", "maxLength": 256}},
					"redirectPort":    map[string]any{"type": "integer", "minimum": 0, "maximum": 65535, "default": 0},
				}},
				"headers": map[string]any{
					"type":        "object",
					"description": "HTTP headers for remote MCP servers",
					"additionalProperties": map[string]any{
						"type": "string",
					},
				},
			},
			"anyOf": []map[string]any{
				{"required": []string{"command"}, "properties": map[string]any{"type": map[string]any{"enum": []string{"stdio"}}}},
				{"required": []string{"type", "url"}, "properties": map[string]any{"type": map[string]any{"enum": []string{"sse", "http"}}}},
			},
		},
	}

	schema["properties"].(map[string]any)["security"] = map[string]any{
		"type": "object", "description": "Execution policy from user-global configuration only; repository configuration cannot override it.",
		"properties": map[string]any{
			"sandbox":             map[string]any{"type": "string", "enum": []string{"bubblewrap", "none"}, "default": "bubblewrap"},
			"allowNetwork":        map[string]any{"type": "boolean", "default": false},
			"allowPrivateNetwork": map[string]any{"type": "boolean", "default": false},
			"envAllow":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"readOnlyPaths":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"denyPaths":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}}
	schema["properties"].(map[string]any)["checks"] = map[string]any{"type": "object", "description": "Named verification commands; every execution still requires command permission.", "additionalProperties": map[string]any{"type": "object", "required": []string{"command"}, "properties": map[string]any{
		"command": map[string]any{"type": "string", "minLength": 1},
		"timeout": map[string]any{"type": "integer", "minimum": 1, "maximum": 600000, "default": 60000},
	}}}
	// Add providers
	runtimeDefaults := (config.RuntimeConfig{}).Defaults()
	runtimeProperties := map[string]any{"mode": map[string]any{"type": "string", "enum": []string{"ask", "plan", "execute"}, "default": "execute"}}
	runtimeProperties["webSearch"] = map[string]any{"type": "boolean", "default": false, "description": "Opt-in external web_search tool; network approval, private-address policy and output bounds still apply."}
	for _, limit := range []struct {
		name          string
		min, max, def int
	}{
		{"maxSteps", 1, 200, runtimeDefaults.MaxSteps},
		{"maxSeconds", 1, 7200, runtimeDefaults.MaxSeconds},
		{"toolBudget", 4096, 1048576, runtimeDefaults.ToolBudget},
		{"firstTokenTimeout", 1, 1800, runtimeDefaults.FirstTokenTimeout},
		{"idleTimeout", 1, 600, runtimeDefaults.IdleTimeout},
	} {
		runtimeProperties[limit.name] = map[string]any{"type": "integer", "anyOf": []map[string]any{{"const": 0}, {"minimum": limit.min, "maximum": limit.max}}, "default": limit.def, "description": "Zero uses the default. Durations are seconds; toolBudget counts bytes of tool content and metadata."}
	}
	schema["properties"].(map[string]any)["runtime"] = map[string]any{"type": "object", "additionalProperties": false, "properties": runtimeProperties}

	providerSchema := map[string]any{
		"type":        "object",
		"description": "LLM provider configurations",
		"additionalProperties": map[string]any{
			"type":        "object",
			"description": "Provider configuration",
			"properties": map[string]any{
				"apiKey": map[string]any{
					"type":        "string",
					"description": "API key for the provider",
				},
				"apiKeyRef":        map[string]any{"type": "string", "pattern": "^keyring:provider-[A-Za-z0-9_-]{20,80}$", "description": "Optional native vault reference generated by explicit vault save. No plaintext or environment fallback. Do not combine with apiKey/apiKeyEnv."},
				"apiKeyEnv":        map[string]any{"type": "string", "description": "Environment variable containing the API key. Only the variable name is saved."},
				"tlsInsecure":      map[string]any{"type": "boolean", "default": false, "description": "Disable certificate and server identity verification only for this explicit HTTPS local/openai OpenAI-compatible connection. Applies to discovery and inference; never follows redirects."},
				"api":              map[string]any{"type": "string", "enum": []string{"", "chat_completions", "responses", "ollama"}, "description": "Default transport for discovered models. ollama is available only for local."},
				"contextWindow":    map[string]any{"type": "integer", "minimum": 0, "description": "Default context limit for discovered models; zero keeps server/catalog metadata."},
				"defaultMaxTokens": map[string]any{"type": "integer", "minimum": 0, "description": "Default output limit for discovered models; zero keeps server/catalog metadata."},
				"baseURL": map[string]any{
					"type":        "string",
					"description": "API base URL for local, OpenAI, Anthropic, Gemini, Groq, OpenRouter or xAI. Used for discovery and inference. LOCAL_ENDPOINT overrides local.",
				},
				"disabled": map[string]any{
					"type":        "boolean",
					"description": "Whether the provider is disabled",
					"default":     false,
				},
			},
		},
	}

	// Add known providers
	knownProviders := []string{
		string(models.ProviderLocal),
		string(models.ProviderCopilot),
		string(models.ProviderXAI),
		string(models.ProviderAnthropic),
		string(models.ProviderOpenAI),
		string(models.ProviderGemini),
		string(models.ProviderGROQ),
		string(models.ProviderOpenRouter),
		string(models.ProviderBedrock),
		string(models.ProviderAzure),
		string(models.ProviderVertexAI),
	}

	providerSchema["additionalProperties"].(map[string]any)["properties"].(map[string]any)["provider"] = map[string]any{
		"type":        "string",
		"description": "Provider type",
		"enum":        knownProviders,
	}

	schema["properties"].(map[string]any)["providers"] = providerSchema
	profileProperties := map[string]any{}
	for key, value := range providerSchema["additionalProperties"].(map[string]any)["properties"].(map[string]any) {
		if key != "provider" {
			profileProperties[key] = value
		}
	}
	profileProperties["backend"] = map[string]any{"type": "string", "enum": knownProviders, "description": "Protocol/backend, independent of the user-chosen profile ID."}
	profileProperties["name"] = map[string]any{"type": "string", "maxLength": 128}
	schema["properties"].(map[string]any)["providerProfiles"] = map[string]any{"type": "object", "description": "Named connections: multiple endpoints may use the same backend.", "propertyNames": map[string]any{"pattern": "^[a-z0-9_-]{1,64}$"}, "additionalProperties": map[string]any{"type": "object", "required": []string{"backend"}, "properties": profileProperties}}
	schema["properties"].(map[string]any)["preferences"] = map[string]any{"type": "object", "properties": map[string]any{
		"resumeLast":     map[string]any{"type": "boolean", "default": false},
		"showProcess":    map[string]any{"type": "boolean", "default": false},
		"favoriteModels": map[string]any{"type": "array", "maxItems": 100, "uniqueItems": true, "items": map[string]any{"type": "string"}},
		"recentModels":   map[string]any{"type": "array", "maxItems": 20, "uniqueItems": true, "items": map[string]any{"type": "string"}},
	}}
	schema["properties"].(map[string]any)["extensions"] = map[string]any{"type": "object", "description": "Trusted user-global configuration only. Repository configuration cannot enable skills or hooks.", "properties": map[string]any{
		"skills": map[string]any{"type": "array", "maxItems": 16, "items": map[string]any{"type": "string", "description": "Absolute skill directory."}},
		"hooks": map[string]any{"type": "array", "maxItems": 16, "items": map[string]any{"type": "object", "required": []string{"event", "command"}, "properties": map[string]any{
			"event":           map[string]any{"type": "string", "enum": []string{"before_tool", "after_tool"}},
			"tool":            map[string]any{"type": "string", "maxLength": 100},
			"command":         map[string]any{"type": "string", "minLength": 1, "maxLength": 4096},
			"timeout_seconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 30},
		}}},
	}}

	// Add agents
	agentSchema := map[string]any{
		"type":        "object",
		"description": "Agent configurations",
		"additionalProperties": map[string]any{
			"type":        "object",
			"description": "Agent configuration",
			"properties": map[string]any{
				"model": map[string]any{
					"type":        "string",
					"description": "Model ID for the agent",
				},
				"maxTokens": map[string]any{
					"type":        "integer",
					"description": "Maximum tokens for the agent",
					"minimum":     1,
				},
				"reasoningEffort": map[string]any{
					"type":        "string",
					"description": "Reasoning effort for models that support it (OpenAI, Anthropic)",
					"enum":        []string{"none", "minimal", "low", "medium", "high", "xhigh"},
				},
			},
			"required": []string{"model"},
		},
	}

	// Model IDs may also refer to the user's custom registry.
	modelProperties := map[string]any{
		"provider":             map[string]any{"type": "string"},
		"provider_profile":     map[string]any{"type": "string", "description": "Optional named provider connection; backend must match provider."},
		"context_estimated":    map[string]any{"type": "boolean", "description": "Context is a conservative fallback rather than server or explicit model metadata."},
		"context_auto":         map[string]any{"type": "boolean", "description": "Compatibility flag for older automatic snapshots. Omit context_window to discover provider context without persisting metadata."},
		"context_source":       map[string]any{"type": "string", "description": "Legacy context metadata; new automatic selections keep the source in memory only."},
		"api_model":            map[string]any{"type": "string", "minLength": 1},
		"name":                 map[string]any{"type": "string"},
		"api":                  map[string]any{"type": "string", "enum": []string{"chat_completions", "responses", "ollama"}},
		"thinking_mode":        map[string]any{"type": "string", "enum": []string{"enabled", "adaptive"}},
		"context_window":       map[string]any{"type": "integer", "minimum": 0, "description": "Optional manual override. Omit or use zero to discover context from the provider."},
		"default_max_tokens":   map[string]any{"type": "integer", "minimum": 1},
		"can_reason":           map[string]any{"type": "boolean"},
		"reasoning_levels":     map[string]any{"type": "array", "maxItems": 6, "uniqueItems": true, "items": map[string]any{"type": "string", "enum": []string{"none", "minimal", "low", "medium", "high", "xhigh"}}, "description": "Explicit supported reasoning efforts; absent means no UI control."},
		"supports_attachments": map[string]any{"type": "boolean"},
		"supports_tools":       map[string]any{"type": "boolean", "description": "Optional provider-reported tool capability; omitted means unknown. Does not grant tool permissions."},
	}
	for _, field := range []string{"cost_per_1m_in", "cost_per_1m_out", "cost_per_1m_in_cached", "cost_per_1m_out_cached"} {
		modelProperties[field] = map[string]any{"type": "number", "minimum": 0}
	}
	schema["properties"].(map[string]any)["models"] = map[string]any{
		"type":        "object",
		"description": "Model references with automatic context discovery, or explicit manual overrides of model metadata",
		"additionalProperties": map[string]any{
			"type": "object", "properties": modelProperties,
			"required": []string{"provider", "api_model"},
		},
	}

	// Add specific agent properties
	agentProperties := map[string]any{}
	knownAgents := []string{
		string(config.AgentCoder),
		string(config.AgentTask),
		string(config.AgentTitle),
	}

	for _, agentName := range knownAgents {
		agentProperties[agentName] = map[string]any{
			"$ref": "#/definitions/agent",
		}
	}

	// Create a combined schema that allows both specific agents and additional ones
	combinedAgentSchema := map[string]any{
		"type":                 "object",
		"description":          "Agent configurations",
		"properties":           agentProperties,
		"additionalProperties": agentSchema["additionalProperties"],
	}

	schema["properties"].(map[string]any)["agents"] = combinedAgentSchema
	schema["definitions"] = map[string]any{
		"agent": agentSchema["additionalProperties"],
	}

	// Add LSP configuration
	schema["properties"].(map[string]any)["lsp"] = map[string]any{
		"type":        "object",
		"description": "Language Server Protocol configurations",
		"additionalProperties": map[string]any{
			"type":        "object",
			"description": "LSP configuration for a language",
			"properties": map[string]any{
				"disabled": map[string]any{
					"type":        "boolean",
					"description": "Whether the LSP is disabled",
					"default":     false,
				},
				"command": map[string]any{
					"type":        "string",
					"description": "Command to execute for the LSP server",
				},
				"args": map[string]any{
					"type":        "array",
					"description": "Command arguments for the LSP server",
					"items": map[string]any{
						"type": "string",
					},
				},
				"fileTypes":   map[string]any{"type": "array", "maxItems": 32, "items": map[string]any{"type": "string", "maxLength": 80}, "description": "Language IDs or extensions for lazy activation; known commands have presets."},
				"rootMarkers": map[string]any{"type": "array", "maxItems": 16, "items": map[string]any{"type": "string", "maxLength": 160}, "description": "Relative marker filenames between source directory and workspace root."},
				"options": map[string]any{
					"type":        "object",
					"description": "Additional options for the LSP server",
				},
			},
			"anyOf": []map[string]any{
				{"required": []string{"command"}, "properties": map[string]any{"type": map[string]any{"enum": []string{"stdio"}}}},
				{"required": []string{"type", "url"}, "properties": map[string]any{"type": map[string]any{"enum": []string{"sse", "http"}}}},
			},
		},
	}

	return schema
}

func keybindingSchema() map[string]any {
	properties := map[string]any{}
	for name, value := range config.DefaultKeybindings {
		properties[name] = map[string]any{"type": "string", "pattern": "^(ctrl|alt)\\+[a-z]$", "default": value}
	}
	return properties
}
