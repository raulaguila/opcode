// Package config manages application configuration from various sources.
package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/spf13/viper"
)

// MCPType defines the type of MCP (Model Control Protocol) server.
type MCPType string

// Supported MCP types
const (
	MCPStdio MCPType = "stdio"
	MCPSse   MCPType = "sse"
	MCPHTTP  MCPType = "http"
)

// MCPServer defines the configuration for a Model Control Protocol server.
type MCPServer struct {
	Command    string            `json:"command"`
	Env        []string          `json:"env"`
	Args       []string          `json:"args"`
	Type       MCPType           `json:"type"`
	URL        string            `json:"url"`
	Headers    map[string]string `json:"headers"`
	HeadersEnv map[string]string `json:"headersEnv,omitempty"`
	EnvVars    map[string]string `json:"envVars,omitempty"`
	Disabled   bool              `json:"disabled,omitempty"`
	OAuth      *MCPOAuthConfig   `json:"oauth,omitempty"`
}

type AgentName string

const (
	AgentCoder      AgentName = "coder"
	AgentSummarizer AgentName = "summarizer"
	AgentTask       AgentName = "task"
	AgentTitle      AgentName = "title"
)

// Agent defines configuration for different LLM models and their token limits.
type Agent struct {
	Model           models.ModelID `json:"model"`
	MaxTokens       int64          `json:"maxTokens"`
	ReasoningEffort string         `json:"reasoningEffort"` // For openai models low,medium,heigh
}

// Provider defines configuration for an LLM provider.
type Provider struct {
	APIKey           string `json:"apiKey"`
	APIKeyEnv        string `json:"apiKeyEnv,omitempty"`
	Disabled         bool   `json:"disabled"`
	BaseURL          string `json:"baseURL,omitempty"` // OpenAI-compatible local API endpoint
	API              string `json:"api,omitempty"`
	ContextWindow    int64  `json:"contextWindow,omitempty"`
	DefaultMaxTokens int64  `json:"defaultMaxTokens,omitempty"`
}

// Data defines storage configuration.
type Data struct {
	Directory string `json:"directory,omitempty"`
}

// LSPConfig defines configuration for Language Server Protocol integration.
type LSPConfig struct {
	FileTypes   []string `json:"fileTypes,omitempty"`
	RootMarkers []string `json:"rootMarkers,omitempty"`
	Disabled    bool     `json:"disabled"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	Options     any      `json:"options"`
}

// TUIConfig defines the configuration for the Terminal User Interface.
type TUIConfig struct {
	Theme         string            `json:"theme,omitempty"`
	Sidebar       string            `json:"sidebar,omitempty"`
	Language      string            `json:"language,omitempty"`
	Notifications string            `json:"notifications,omitempty"`
	Keybindings   map[string]string `json:"keybindings,omitempty"`
}

// Preferences are user choices; resolved credentials never belong here.
type Preferences struct {
	ResumeLast     bool             `json:"resumeLast,omitempty"`
	ShowProcess    *bool            `json:"showProcess,omitempty"`
	FavoriteModels []models.ModelID `json:"favoriteModels,omitempty"`
	RecentModels   []models.ModelID `json:"recentModels,omitempty"`
}

func (p Preferences) ProcessVisible() bool { return p.ShowProcess != nil && *p.ShowProcess }

// ShellConfig defines the configuration for the shell used by the bash tool.
type ShellConfig struct {
	Path string   `json:"path,omitempty"`
	Args []string `json:"args,omitempty"`
}

// Security settings are loaded only from the user's global configuration.
type SecurityConfig struct {
	Sandbox             string   `json:"sandbox,omitempty"`
	AllowNetwork        bool     `json:"allowNetwork,omitempty"`
	AllowPrivateNetwork bool     `json:"allowPrivateNetwork,omitempty"`
	EnvAllow            []string `json:"envAllow,omitempty"`
	ReadOnlyPaths       []string `json:"readOnlyPaths,omitempty"`
	DenyPaths           []string `json:"denyPaths,omitempty"`
}
type CheckConfig struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// Config is the main configuration structure for the application.
type Config struct {
	SchemaVersion    int                               `json:"schemaVersion,omitempty"`
	Data             Data                              `json:"data"`
	WorkingDir       string                            `json:"wd,omitempty"`
	MCPServers       map[string]MCPServer              `json:"mcpServers,omitempty"`
	Models           map[models.ModelID]models.Model   `json:"models,omitempty"`
	Providers        map[models.ModelProvider]Provider `json:"providers,omitempty"`
	ProviderProfiles map[string]ProviderProfile        `json:"providerProfiles,omitempty"`
	Preferences      Preferences                       `json:"preferences,omitempty"`
	LSP              map[string]LSPConfig              `json:"lsp,omitempty"`
	Agents           map[AgentName]Agent               `json:"agents,omitempty"`
	Debug            bool                              `json:"debug,omitempty"`
	DebugLSP         bool                              `json:"debugLSP,omitempty"`
	ContextPaths     []string                          `json:"contextPaths,omitempty"`
	TUI              TUIConfig                         `json:"tui"`
	Shell            ShellConfig                       `json:"shell,omitempty"`
	Security         SecurityConfig                    `json:"security,omitempty"`
	Checks           map[string]CheckConfig            `json:"checks,omitempty"`
	Runtime          RuntimeConfig                     `json:"runtime,omitempty"`
	Extensions       ExtensionsConfig                  `json:"extensions,omitempty"`
	Index            IndexConfig                       `json:"index,omitempty"`
	AutoCompact      bool                              `json:"autoCompact,omitempty"`
}

// Application constants
const (
	defaultDataDirectory = ".opcode"
	defaultLogLevel      = "info"
	appName              = "opcode"

	MaxTokensFallbackDefault = 4096
)

var defaultContextPaths = []string{
	"AGENTS.md",
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
}

// Global configuration instance
var cfg *Config

// ValidateStartup requires the agents used by the application. Loading settings
// alone remains possible without credentials, e.g. for tooling and setup.
func (c *Config) ValidateStartup() error {
	for _, name := range []AgentName{AgentCoder, AgentTitle, AgentSummarizer, AgentTask} {
		if c == nil || c.Agents[name].Model == "" {
			return fmt.Errorf("no model configured for agent %q: set LOCAL_ENDPOINT for Ollama/LM Studio, set a provider API key (e.g. OPENAI_API_KEY or ANTHROPIC_API_KEY), or configure providers and agents.coder.model in ~/.opcode/config.json", name)
		}
	}
	return nil
}

// Load initializes the configuration from environment variables and config files.
// If debug is true, debug mode is enabled and log level is set to debug.
// It returns an error if configuration loading fails.
func Load(workingDir string, debug bool) (*Config, error) {
	return loadConfiguration(workingDir, debug, true)
}

// LoadOffline loads configuration for a standalone diagnostic command. It
// does not discover local models, start integrations or create debug logs.
// Application startup continues to use the cached Load path.
func LoadOffline(workingDir string) (*Config, error) {
	return loadConfiguration(workingDir, false, false)
}

func loadConfiguration(workingDir string, debug, discoverLocal bool) (*Config, error) {
	if cfg != nil {
		return cfg, nil
	}
	// Standalone offline diagnostics and explicitly supplied configurations are
	// read-only. Normal startup creates the canonical file from legacy global
	// settings or a safe editable provider template, plus a separate reference.
	if discoverLocal && viper.ConfigFileUsed() == "" {
		if err := ensureReferenceTemplate(); err != nil {
			return nil, err
		}
		if err := ensureGlobalTemplate(); err != nil {
			return nil, err
		}
	}

	cfg = &Config{
		WorkingDir: workingDir,
		MCPServers: make(map[string]MCPServer),
		Providers:  make(map[models.ModelProvider]Provider),
		LSP:        make(map[string]LSPConfig),
	}

	configureViper()
	setDefaults(debug)

	// Read global config
	if path := viper.ConfigFileUsed(); path != "" {
		if err := ValidateDocument(path); err != nil {
			cfg = nil
			return nil, fmt.Errorf("read config: %w", err)
		}
	}
	if err := readConfig(viper.ReadInConfig()); err != nil {
		return cfg, err
	}
	if path := viper.ConfigFileUsed(); path != "" {
		if err := ValidateDocument(path); err != nil {
			cfg = nil
			return nil, err
		}
	}

	// Repository configuration cannot weaken the user's execution policy.
	var trusted struct {
		Security   SecurityConfig
		Extensions ExtensionsConfig
		Index      IndexConfig
	}
	if err := viper.Unmarshal(&trusted); err != nil {
		return nil, err
	}
	// Load and merge local config
	if err := mergeLocalConfig(workingDir); err != nil {
		cfg = nil
		return nil, err
	}
	if path := ProjectConfigPath(workingDir); path != viper.ConfigFileUsed() {
		if err := ValidateDocument(path); err != nil {
			cfg = nil
			return nil, err
		}
	}

	setProviderDefaults()

	// Apply configuration to the struct
	if err := viper.Unmarshal(cfg); err != nil {
		return cfg, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	// AllSettings expands dots as paths, but model IDs (e.g. local.llama3.1:8b)
	// are literal map keys. Decode the original models map before validation.
	modelJSON, err := json.Marshal(viper.GetStringMap("models"))
	var definitions map[models.ModelID]models.Model
	if err != nil || json.Unmarshal(modelJSON, &definitions) != nil {
		cfg = nil
		return nil, fmt.Errorf("invalid model definitions")
	}
	cfg.Models = definitions
	if cfg.Data.Directory != "" && !filepath.IsAbs(cfg.Data.Directory) {
		cfg.Data.Directory = filepath.Join(workingDir, cfg.Data.Directory)
	}
	if cfg.TUI.Theme == legacyAppName {
		cfg.TUI.Theme = appName
	}
	if err := configureProviders(); err != nil {
		cfg = nil
		return nil, err
	}
	cfg.Security = trusted.Security
	cfg.Extensions = trusted.Extensions
	cfg.Index.Embeddings = trusted.Index.Embeddings
	if err := cfg.Extensions.Validate(); err != nil {
		cfg = nil
		return nil, err
	}
	if err := cfg.TUI.Validate(); err != nil {
		cfg = nil
		return nil, err
	}
	if err := cfg.Index.Validate(); err != nil {
		return nil, err
	}
	if err := cfg.Runtime.Validate(); err != nil {
		cfg = nil
		return nil, err
	}
	cfg.Runtime = cfg.Runtime.Defaults()
	if cfg.Security.Sandbox == "" {
		cfg.Security.Sandbox = "bubblewrap"
	}
	if cfg.Security.Sandbox != "bubblewrap" && cfg.Security.Sandbox != "none" {
		cfg = nil
		return nil, fmt.Errorf("security.sandbox must be bubblewrap or none")
	}
	if err := registerModels(cfg.Models); err != nil {
		cfg = nil
		return nil, err
	}
	if err := configureLocalProvider(discoverLocal); err != nil {
		cfg = nil
		return nil, err
	}

	if discoverLocal {
		refreshAutomaticContexts()
	}
	applyDefaultValues()
	defaultLevel := slog.LevelInfo
	if cfg.Debug {
		defaultLevel = slog.LevelDebug
	}
	if discoverLocal && Environment("OPCODE_DEV_DEBUG") == "true" {
		loggingFile := fmt.Sprintf("%s/%s", cfg.Data.Directory, "debug.log")
		messagesPath := fmt.Sprintf("%s/%s", cfg.Data.Directory, "messages")

		// if file does not exist create it
		if _, err := os.Stat(loggingFile); os.IsNotExist(err) {
			if err := os.MkdirAll(cfg.Data.Directory, 0o755); err != nil {
				return cfg, fmt.Errorf("failed to create directory: %w", err)
			}
			if _, err := os.Create(loggingFile); err != nil {
				return cfg, fmt.Errorf("failed to create log file: %w", err)
			}
		}

		if _, err := os.Stat(messagesPath); os.IsNotExist(err) {
			if err := os.MkdirAll(messagesPath, 0o756); err != nil {
				return cfg, fmt.Errorf("failed to create directory: %w", err)
			}
		}
		logging.MessageDir = messagesPath

		sloggingFileWriter, err := os.OpenFile(loggingFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
		if err != nil {
			return cfg, fmt.Errorf("failed to open log file: %w", err)
		}
		// Configure logger
		logger := slog.New(slog.NewTextHandler(sloggingFileWriter, &slog.HandlerOptions{
			Level: defaultLevel,
		}))
		slog.SetDefault(logger)
	} else {
		// Configure logger
		logger := slog.New(slog.NewTextHandler(logging.NewWriter(), &slog.HandlerOptions{
			Level: defaultLevel,
		}))
		slog.SetDefault(logger)
	}

	// Validate configuration
	if err := Validate(); err != nil {
		return cfg, fmt.Errorf("config validation failed: %w", err)
	}

	// Override the max tokens for title agent
	if title, ok := cfg.Agents[AgentTitle]; ok {
		title.MaxTokens = 80
		cfg.Agents[AgentTitle] = title
	}
	return cfg, nil
}

// configureViper sets up viper's configuration paths and environment variables.
func configureViper() {
	explicitFile := viper.ConfigFileUsed()
	viper.SetConfigName("config")
	viper.SetConfigType("json")
	if explicitFile != "" {
		viper.SetConfigFile(explicitFile)
	} else if path, exists := globalConfigPath(); exists {
		viper.SetConfigFile(path)
	}
	viper.AddConfigPath("$HOME/.opcode")
	viper.SetEnvPrefix(strings.ToUpper(appName))
	viper.AllowEmptyEnv(true)
	viper.AutomaticEnv()
	bindCompatibleEnvironment(viper.GetViper())
}

// setDefaults configures default values for configuration options.
func setDefaults(debug bool) {
	viper.SetDefault("data.directory", defaultDataPath(cfg.WorkingDir))
	viper.SetDefault("contextPaths", defaultGuidancePaths(cfg.WorkingDir))
	viper.SetDefault("tui.theme", "opcode")
	viper.SetDefault("autoCompact", true)

	// Set default shell from environment or fallback to /bin/bash
	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		shellPath = "/bin/bash"
	}
	viper.SetDefault("shell.path", shellPath)
	viper.SetDefault("shell.args", []string{})

	if debug {
		viper.SetDefault("debug", true)
		viper.Set("log.level", "debug")
	} else {
		viper.SetDefault("debug", false)
		viper.SetDefault("log.level", defaultLogLevel)
	}
}

// setProviderDefaults configures LLM provider defaults based on provider provided by
// environment variables and configuration file.
func setProviderDefaults() {
	// Resolve named secrets only in memory, before choosing provider defaults.
	for id := range viper.GetStringMap("providers") {
		if name := viper.GetString("providers." + id + ".apiKeyEnv"); name != "" {
			viper.Set("providers."+id+".apiKey", os.Getenv(name))
		}
	}
	// Set all API keys we can find in the environment
	// Note: Viper does not default if the json apiKey is ""
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.anthropic.apiKey", apiKey)
	}
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.openai.apiKey", apiKey)
	}
	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.gemini.apiKey", apiKey)
	}
	if apiKey := os.Getenv("GROQ_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.groq.apiKey", apiKey)
	}
	if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.openrouter.apiKey", apiKey)
	}
	if apiKey := os.Getenv("XAI_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.xai.apiKey", apiKey)
	}
	if apiKey := os.Getenv("LOCAL_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.local.apiKey", apiKey)
	}
	if apiKey := os.Getenv("AZURE_OPENAI_ENDPOINT"); apiKey != "" {
		// api-key may be empty when using Entra ID credentials – that's okay
		viper.SetDefault("providers.azure.apiKey", os.Getenv("AZURE_OPENAI_API_KEY"))
	}
	if apiKey, err := LoadGitHubToken(); err == nil && apiKey != "" {
		viper.SetDefault("providers.copilot.apiKey", apiKey)
		if viper.GetString("providers.copilot.apiKey") == "" {
			viper.Set("providers.copilot.apiKey", apiKey)
		}
	}
	// An explicitly selected local endpoint must not pick a remote provider's
	// defaults just because credentials for that provider happen to exist.
	if !viper.GetBool("providers.local.disabled") && (os.Getenv("LOCAL_ENDPOINT") != "" || viper.GetString("providers.local.baseURL") != "") {
		return
	}

	// Use this order to set the default models
	// 1. Copilot
	// 2. Anthropic
	// 3. OpenAI
	// 4. Google Gemini
	// 5. Groq
	// 6. OpenRouter
	// 7. AWS Bedrock
	// 8. Azure
	// 9. Google Cloud VertexAI

	// copilot configuration
	if key := viper.GetString("providers.copilot.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.CopilotGPT4o)
		viper.SetDefault("agents.summarizer.model", models.CopilotGPT4o)
		viper.SetDefault("agents.task.model", models.CopilotGPT4o)
		viper.SetDefault("agents.title.model", models.CopilotGPT4o)
		return
	}

	// Anthropic configuration
	if key := viper.GetString("providers.anthropic.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.Claude4Sonnet)
		viper.SetDefault("agents.summarizer.model", models.Claude4Sonnet)
		viper.SetDefault("agents.task.model", models.Claude4Sonnet)
		viper.SetDefault("agents.title.model", models.Claude4Sonnet)
		return
	}

	// OpenAI configuration
	if key := viper.GetString("providers.openai.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.GPT41)
		viper.SetDefault("agents.summarizer.model", models.GPT41)
		viper.SetDefault("agents.task.model", models.GPT41Mini)
		viper.SetDefault("agents.title.model", models.GPT41Mini)
		return
	}

	// Google Gemini configuration
	if key := viper.GetString("providers.gemini.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.Gemini25)
		viper.SetDefault("agents.summarizer.model", models.Gemini25)
		viper.SetDefault("agents.task.model", models.Gemini25Flash)
		viper.SetDefault("agents.title.model", models.Gemini25Flash)
		return
	}

	// Groq configuration
	if key := viper.GetString("providers.groq.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.QWENQwq)
		viper.SetDefault("agents.summarizer.model", models.QWENQwq)
		viper.SetDefault("agents.task.model", models.QWENQwq)
		viper.SetDefault("agents.title.model", models.QWENQwq)
		return
	}

	// OpenRouter configuration
	if key := viper.GetString("providers.openrouter.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.OpenRouterClaude37Sonnet)
		viper.SetDefault("agents.summarizer.model", models.OpenRouterClaude37Sonnet)
		viper.SetDefault("agents.task.model", models.OpenRouterClaude37Sonnet)
		viper.SetDefault("agents.title.model", models.OpenRouterClaude35Haiku)
		return
	}

	// XAI configuration
	if key := viper.GetString("providers.xai.apiKey"); strings.TrimSpace(key) != "" {
		viper.SetDefault("agents.coder.model", models.XAIGrok3Beta)
		viper.SetDefault("agents.summarizer.model", models.XAIGrok3Beta)
		viper.SetDefault("agents.task.model", models.XAIGrok3Beta)
		viper.SetDefault("agents.title.model", models.XAiGrok3MiniFastBeta)
		return
	}

	// AWS Bedrock configuration
	if hasAWSCredentials() {
		viper.SetDefault("agents.coder.model", models.BedrockClaude37Sonnet)
		viper.SetDefault("agents.summarizer.model", models.BedrockClaude37Sonnet)
		viper.SetDefault("agents.task.model", models.BedrockClaude37Sonnet)
		viper.SetDefault("agents.title.model", models.BedrockClaude37Sonnet)
		return
	}

	// Azure OpenAI configuration
	if os.Getenv("AZURE_OPENAI_ENDPOINT") != "" {
		viper.SetDefault("agents.coder.model", models.AzureGPT41)
		viper.SetDefault("agents.summarizer.model", models.AzureGPT41)
		viper.SetDefault("agents.task.model", models.AzureGPT41Mini)
		viper.SetDefault("agents.title.model", models.AzureGPT41Mini)
		return
	}

	// Google Cloud VertexAI configuration
	if hasVertexAICredentials() {
		viper.SetDefault("agents.coder.model", models.VertexAIGemini25)
		viper.SetDefault("agents.summarizer.model", models.VertexAIGemini25)
		viper.SetDefault("agents.task.model", models.VertexAIGemini25Flash)
		viper.SetDefault("agents.title.model", models.VertexAIGemini25Flash)
		return
	}
}

// hasAWSCredentials checks if AWS credentials are available in the environment.
func hasAWSCredentials() bool {
	// Check for explicit AWS credentials
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		return true
	}

	// Check for AWS profile
	if os.Getenv("AWS_PROFILE") != "" || os.Getenv("AWS_DEFAULT_PROFILE") != "" {
		return true
	}

	// Check for AWS region
	if os.Getenv("AWS_REGION") != "" || os.Getenv("AWS_DEFAULT_REGION") != "" {
		return true
	}

	// Check if running on EC2 with instance profile
	if os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI") != "" ||
		os.Getenv("AWS_CONTAINER_CREDENTIALS_FULL_URI") != "" {
		return true
	}

	return false
}

// hasVertexAICredentials checks if VertexAI credentials are available in the environment.
func hasVertexAICredentials() bool {
	// Check for explicit VertexAI parameters
	if os.Getenv("VERTEXAI_PROJECT") != "" && os.Getenv("VERTEXAI_LOCATION") != "" {
		return true
	}
	// Check for Google Cloud project and location
	if os.Getenv("GOOGLE_CLOUD_PROJECT") != "" && (os.Getenv("GOOGLE_CLOUD_REGION") != "" || os.Getenv("GOOGLE_CLOUD_LOCATION") != "") {
		return true
	}
	return false
}

func hasCopilotCredentials() bool {
	// Check for explicit Copilot parameters
	if token, _ := LoadGitHubToken(); token != "" {
		return true
	}
	return false
}

// readConfig handles the result of reading a configuration file.
func readConfig(err error) error {
	if err == nil {
		return nil
	}

	// It's okay if the config file doesn't exist
	if _, ok := err.(viper.ConfigFileNotFoundError); ok {
		return nil
	}

	return fmt.Errorf("failed to read config: %w", err)
}

// mergeLocalConfig loads and merges configuration from the local directory.
func mergeLocalConfig(workingDir string) error {
	local := viper.New()
	local.SetConfigFile(ProjectConfigPath(workingDir))
	local.SetConfigType("json")
	if err := local.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read project configuration: %w", err)
	}
	settings := local.AllSettings()
	if local.InConfig("models") {
		settings["models"] = local.GetStringMap("models")
	}
	return viper.MergeConfigMap(settings)
}

// applyDefaultValues sets default values for configuration fields that need processing.
func applyDefaultValues() {
	// Set default MCP type if not specified
	for k, v := range cfg.MCPServers {
		if v.Type == "" {
			v.Type = MCPStdio
			cfg.MCPServers[k] = v
		}
	}
}

// It validates model IDs and providers, ensuring they are supported.
func validateAgent(cfg *Config, name AgentName, agent Agent) error {
	// Check if model exists
	// TODO:	If a copilot model is specified, but model is not found,
	// 		 	it might be new model. The https://api.githubcopilot.com/models
	// 		 	endpoint should be queried to validate if the model is supported.
	model, modelExists := models.SupportedModels[agent.Model]
	if !modelExists {
		logging.Warn("unsupported model configured, reverting to default",
			"agent", name,
			"configured_model", agent.Model)

		// Set default model based on available providers
		if setDefaultModelForAgent(name) {
			logging.Info("set default model for agent", "agent", name, "model", cfg.Agents[name].Model)
		} else {
			return fmt.Errorf("no valid provider available for agent %s", name)
		}
		return nil
	}

	// Check if provider for the model is configured
	provider := model.Provider
	providerCfg, providerExists := cfg.Providers[provider]
	if model.ProviderProfile != "" {
		var err error
		providerCfg, err = ProviderForModel(model)
		if err != nil {
			return fmt.Errorf("agent %s: %w", name, err)
		}
		providerExists = true
	}

	if !providerExists {
		// Provider not configured, check if we have environment variables
		apiKey := getProviderAPIKey(provider)
		if apiKey == "" && provider != models.ProviderLocal {
			logging.Warn("provider not configured for model, reverting to default",
				"agent", name,
				"model", agent.Model,
				"provider", provider)

			// Set default model based on available providers
			if setDefaultModelForAgent(name) {
				logging.Info("set default model for agent", "agent", name, "model", cfg.Agents[name].Model)
			} else {
				return fmt.Errorf("no valid provider available for agent %s", name)
			}
		} else {
			// Add provider with API key from environment
			cfg.Providers[provider] = Provider{
				APIKey: apiKey,
			}
			logging.Info("added provider from environment", "provider", provider)
		}
	} else if providerCfg.Disabled || (providerCfg.APIKey == "" && requiresAPIKey(provider)) {
		// Provider is disabled or has no API key
		logging.Warn("provider is disabled or has no API key, reverting to default",
			"agent", name,
			"model", agent.Model,
			"provider", provider)

		// Set default model based on available providers
		if setDefaultModelForAgent(name) {
			logging.Info("set default model for agent", "agent", name, "model", cfg.Agents[name].Model)
		} else {
			return fmt.Errorf("no valid provider available for agent %s", name)
		}
	}

	// Validate max tokens
	if agent.MaxTokens <= 0 {
		logging.Warn("invalid max tokens, setting to default",
			"agent", name,
			"model", agent.Model,
			"max_tokens", agent.MaxTokens)

		// Update the agent with default max tokens
		updatedAgent := cfg.Agents[name]
		if model.DefaultMaxTokens > 0 {
			updatedAgent.MaxTokens = model.DefaultMaxTokens
		} else {
			updatedAgent.MaxTokens = MaxTokensFallbackDefault
		}
		cfg.Agents[name] = updatedAgent
	} else if model.ContextWindow > 0 && agent.MaxTokens > model.ContextWindow/2 {
		// Ensure max tokens doesn't exceed half the context window (reasonable limit)
		logging.Warn("max tokens exceeds half the context window, adjusting",
			"agent", name,
			"model", agent.Model,
			"max_tokens", agent.MaxTokens,
			"context_window", model.ContextWindow)

		// Update the agent with adjusted max tokens
		updatedAgent := cfg.Agents[name]
		updatedAgent.MaxTokens = model.ContextWindow / 2
		cfg.Agents[name] = updatedAgent
	}

	// Validate reasoning effort for models that support reasoning
	if model.CanReason && provider == models.ProviderOpenAI {
		if agent.ReasoningEffort == "" {
			// Set default reasoning effort for models that support it
			logging.Info("setting default reasoning effort for model that supports reasoning",
				"agent", name,
				"model", agent.Model)

			// Update the agent with default reasoning effort
			updatedAgent := cfg.Agents[name]
			updatedAgent.ReasoningEffort = "medium"
			cfg.Agents[name] = updatedAgent
		} else {
			// Check if reasoning effort is valid (low, medium, high)
			effort := strings.ToLower(agent.ReasoningEffort)
			if effort != "none" && effort != "minimal" && effort != "low" && effort != "medium" && effort != "high" && effort != "xhigh" {
				logging.Warn("invalid reasoning effort, setting to medium",
					"agent", name,
					"model", agent.Model,
					"reasoning_effort", agent.ReasoningEffort)

				// Update the agent with valid reasoning effort
				updatedAgent := cfg.Agents[name]
				updatedAgent.ReasoningEffort = "medium"
				cfg.Agents[name] = updatedAgent
			}
		}
	} else if !model.CanReason && agent.ReasoningEffort != "" {
		// Model doesn't support reasoning but reasoning effort is set
		logging.Warn("model doesn't support reasoning but reasoning effort is set, ignoring",
			"agent", name,
			"model", agent.Model,
			"reasoning_effort", agent.ReasoningEffort)

		// Update the agent to remove reasoning effort
		updatedAgent := cfg.Agents[name]
		updatedAgent.ReasoningEffort = ""
		cfg.Agents[name] = updatedAgent
	}

	return nil
}

// Validate checks if the configuration is valid and applies defaults where needed.
func Validate() error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	// A single coder configuration is sufficient. Keep explicit auxiliary
	// agents and inherit the coder's model for any roles left unspecified.
	if coder, ok := cfg.Agents[AgentCoder]; ok && coder.Model != "" {
		for _, name := range []AgentName{AgentTitle, AgentSummarizer, AgentTask} {
			if _, exists := cfg.Agents[name]; !exists {
				cfg.Agents[name] = coder
			}
		}
	}

	// Validate agent models
	for name, agent := range cfg.Agents {
		if err := validateAgent(cfg, name, agent); err != nil {
			return err
		}
	}

	// Validate providers
	for provider, providerCfg := range cfg.Providers {
		if providerCfg.APIKey == "" && !providerCfg.Disabled && requiresAPIKey(provider) {
			fmt.Printf("provider has no API key, marking as disabled %s", provider)
			logging.Warn("provider has no API key, marking as disabled", "provider", provider)
			providerCfg.Disabled = true
			cfg.Providers[provider] = providerCfg
		}
	}

	// Validate LSP configurations
	for language, lspConfig := range cfg.LSP {
		if lspConfig.Command == "" && !lspConfig.Disabled {
			logging.Warn("LSP configuration has no command, marking as disabled", "language", language)
			lspConfig.Disabled = true
			cfg.LSP[language] = lspConfig
		}
	}

	return nil
}

// getProviderAPIKey gets the API key for a provider from environment variables
func getProviderAPIKey(provider models.ModelProvider) string {
	switch provider {
	case models.ProviderAnthropic:
		return os.Getenv("ANTHROPIC_API_KEY")
	case models.ProviderOpenAI:
		return os.Getenv("OPENAI_API_KEY")
	case models.ProviderGemini:
		return os.Getenv("GEMINI_API_KEY")
	case models.ProviderGROQ:
		return os.Getenv("GROQ_API_KEY")
	case models.ProviderAzure:
		return os.Getenv("AZURE_OPENAI_API_KEY")
	case models.ProviderOpenRouter:
		return os.Getenv("OPENROUTER_API_KEY")
	case models.ProviderBedrock:
		if hasAWSCredentials() {
			return "aws-credentials-available"
		}
	case models.ProviderVertexAI:
		if hasVertexAICredentials() {
			return "vertex-ai-credentials-available"
		}
	}
	return ""
}

// setDefaultModelForAgent sets a default model for an agent based on available providers
func setDefaultModelForAgent(agent AgentName) bool {
	if hasCopilotCredentials() {
		maxTokens := int64(5000)
		if agent == AgentTitle {
			maxTokens = 80
		}

		cfg.Agents[agent] = Agent{
			Model:     models.CopilotGPT4o,
			MaxTokens: maxTokens,
		}
		return true
	}
	// Check providers in order of preference
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		maxTokens := int64(5000)
		if agent == AgentTitle {
			maxTokens = 80
		}
		cfg.Agents[agent] = Agent{
			Model:     models.Claude37Sonnet,
			MaxTokens: maxTokens,
		}
		return true
	}

	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		var model models.ModelID
		maxTokens := int64(5000)
		reasoningEffort := ""

		switch agent {
		case AgentTitle:
			model = models.GPT41Mini
			maxTokens = 80
		case AgentTask:
			model = models.GPT41Mini
		default:
			model = models.GPT41
		}

		// Check if model supports reasoning
		if modelInfo, ok := models.SupportedModels[model]; ok && modelInfo.CanReason {
			reasoningEffort = "medium"
		}

		cfg.Agents[agent] = Agent{
			Model:           model,
			MaxTokens:       maxTokens,
			ReasoningEffort: reasoningEffort,
		}
		return true
	}

	if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
		var model models.ModelID
		maxTokens := int64(5000)
		reasoningEffort := ""

		switch agent {
		case AgentTitle:
			model = models.OpenRouterClaude35Haiku
			maxTokens = 80
		case AgentTask:
			model = models.OpenRouterClaude37Sonnet
		default:
			model = models.OpenRouterClaude37Sonnet
		}

		// Check if model supports reasoning
		if modelInfo, ok := models.SupportedModels[model]; ok && modelInfo.CanReason {
			reasoningEffort = "medium"
		}

		cfg.Agents[agent] = Agent{
			Model:           model,
			MaxTokens:       maxTokens,
			ReasoningEffort: reasoningEffort,
		}
		return true
	}

	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		var model models.ModelID
		maxTokens := int64(5000)

		if agent == AgentTitle {
			model = models.Gemini25Flash
			maxTokens = 80
		} else {
			model = models.Gemini25
		}

		cfg.Agents[agent] = Agent{
			Model:     model,
			MaxTokens: maxTokens,
		}
		return true
	}

	if apiKey := os.Getenv("GROQ_API_KEY"); apiKey != "" {
		maxTokens := int64(5000)
		if agent == AgentTitle {
			maxTokens = 80
		}

		cfg.Agents[agent] = Agent{
			Model:     models.QWENQwq,
			MaxTokens: maxTokens,
		}
		return true
	}

	if hasAWSCredentials() {
		maxTokens := int64(5000)
		if agent == AgentTitle {
			maxTokens = 80
		}

		cfg.Agents[agent] = Agent{
			Model:           models.BedrockClaude37Sonnet,
			MaxTokens:       maxTokens,
			ReasoningEffort: "medium", // Claude models support reasoning
		}
		return true
	}

	if hasVertexAICredentials() {
		var model models.ModelID
		maxTokens := int64(5000)

		if agent == AgentTitle {
			model = models.VertexAIGemini25Flash
			maxTokens = 80
		} else {
			model = models.VertexAIGemini25
		}

		cfg.Agents[agent] = Agent{
			Model:     model,
			MaxTokens: maxTokens,
		}
		return true
	}

	return false
}

func updateCfgFile(updateCfg func(config *Config)) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	// Get the config file path
	configFile := viper.ConfigFileUsed()
	var configData []byte
	if configFile == "" {
		configFile = configuredFilePath()
		if configFile == "" {
			return fmt.Errorf("cannot determine configuration path")
		}
		data, err := os.ReadFile(configFile)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to read config file: %w", err)
		}
		if os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(configFile), 0700); err != nil {
				return err
			}
			data = []byte(`{}`)
		}
		configData = data
	} else {
		// Read the existing config file
		data, err := os.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read config file: %w", err)
		}
		configData = data
	}

	return patchConfigFile(configFile, configData, updateCfg)
}

// Get returns the current configuration.
// It's safe to call this function multiple times.
func Get() *Config {
	return cfg
}

// WorkingDirectory returns the current working directory from the configuration.
func WorkingDirectory() string {
	if cfg == nil {
		panic("config not loaded")
	}
	return cfg.WorkingDir
}

func UpdateAgentModel(agentName AgentName, modelID models.ModelID) error {
	if cfg == nil {
		panic("config not loaded")
	}

	model, ok := models.SupportedModels[modelID]
	if !ok {
		return fmt.Errorf("model %s not supported", modelID)
	}

	return UpdateAgentModelDefinition(agentName, model)
}

// UpdateTheme updates the theme in the configuration and writes it to the config file.
func UpdateTheme(themeName string) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	// Update the in-memory config
	cfg.TUI.Theme = themeName

	// Update the file config
	return updateCfgFile(func(config *Config) {
		config.TUI.Theme = themeName
	})
}

// Tries to load Github token from all possible locations
func LoadGitHubToken() (string, error) {
	// First check environment variable
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}

	// Get config directory
	var configDir string
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		configDir = xdgConfig
	} else if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			configDir = localAppData
		} else {
			configDir = filepath.Join(os.Getenv("HOME"), "AppData", "Local")
		}
	} else {
		configDir = filepath.Join(os.Getenv("HOME"), ".config")
	}

	// Try both hosts.json and apps.json files
	filePaths := []string{
		filepath.Join(configDir, "github-copilot", "hosts.json"),
		filepath.Join(configDir, "github-copilot", "apps.json"),
	}

	for _, filePath := range filePaths {
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var config map[string]map[string]interface{}
		if err := json.Unmarshal(data, &config); err != nil {
			continue
		}

		for key, value := range config {
			if strings.Contains(key, "github.com") {
				if oauthToken, ok := value["oauth_token"].(string); ok {
					return oauthToken, nil
				}
			}
		}
	}

	return "", fmt.Errorf("GitHub token not found in standard locations")
}
