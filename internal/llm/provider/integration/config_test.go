package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	appconfig "github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
)

type liveConfig struct {
	TimeoutSeconds int                     `json:"timeoutSeconds,omitempty"`
	Providers      map[string]liveProvider `json:"providers"`
}
type liveProvider struct {
	Enabled         bool   `json:"enabled,omitempty"`
	Model           string `json:"model"`
	APIKey          string `json:"apiKey,omitempty"`
	APIKeyEnv       string `json:"apiKeyEnv,omitempty"`
	MaxTokens       int64  `json:"maxTokens,omitempty"`
	CanReason       bool   `json:"canReason,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	ThinkingMode    string `json:"thinkingMode,omitempty"`
	Tools           *bool  `json:"tools,omitempty"`
	Endpoint        string `json:"endpoint,omitempty"`
	APIVersion      string `json:"apiVersion,omitempty"`
	Project         string `json:"project,omitempty"`
	Location        string `json:"location,omitempty"`
	CredentialsFile string `json:"credentialsFile,omitempty"`
	Region          string `json:"region,omitempty"`
	AWSProfile      string `json:"awsProfile,omitempty"`
}
type providerSpec struct {
	name        string
	provider    models.ModelProvider
	api, keyEnv string
	optionalKey bool
}

var providerSpecs = []providerSpec{
	{"openai", models.ProviderOpenAI, "chat_completions", "OPENAI_API_KEY", false},
	{"openai-responses", models.ProviderOpenAI, "responses", "OPENAI_API_KEY", false},
	{"anthropic", models.ProviderAnthropic, "", "ANTHROPIC_API_KEY", false},
	{"gemini", models.ProviderGemini, "", "GEMINI_API_KEY", false},
	{"groq", models.ProviderGROQ, "chat_completions", "GROQ_API_KEY", false},
	{"openrouter", models.ProviderOpenRouter, "chat_completions", "OPENROUTER_API_KEY", false},
	{"xai", models.ProviderXAI, "chat_completions", "XAI_API_KEY", false},
	{"local", models.ProviderLocal, "chat_completions", "LOCAL_API_KEY", true},
	{"azure", models.ProviderAzure, "chat_completions", "AZURE_OPENAI_API_KEY", true},
	{"azure-responses", models.ProviderAzure, "responses", "AZURE_OPENAI_API_KEY", true},
	{"vertexai", models.ProviderVertexAI, "", "", true},
	{"bedrock", models.ProviderBedrock, "", "AWS_BEARER_TOKEN_BEDROCK", true},
	{"copilot", models.ProviderCopilot, "", "GITHUB_TOKEN", false},
}

type envLookup func(string) (string, bool)
type liveCase struct {
	spec        providerSpec
	config      liveProvider
	environment map[string]string
}

func liveConfigPath(getenv envLookup) (string, bool, error) {
	rawGetenv := getenv
	getenv = func(name string) (string, bool) { return appconfig.LookupEnvironmentWith(name, rawGetenv) }
	if path, ok := getenv("OPCODE_LIVE_CONFIG"); ok {
		if path == "" {
			return "", true, errors.New("OPCODE_LIVE_CONFIG must name a file")
		}
		return path, true, nil
	}
	if path, ok := getenv("XDG_CONFIG_HOME"); ok && path != "" {
		return appconfig.IntegrationConfigPath(path), false, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", false, errors.New("cannot find user configuration directory; set OPCODE_LIVE_CONFIG")
	}
	return appconfig.IntegrationConfigPath(dir), false, nil
}
func readLiveConfig(path string, required bool) (liveConfig, error) {
	empty := liveConfig{Providers: map[string]liveProvider{}}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && !required {
		return empty, nil
	}
	if err != nil {
		return empty, errors.New("cannot open integration configuration file")
	}
	if !info.Mode().IsRegular() {
		return empty, errors.New("integration configuration must be a regular file, not a symlink")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return empty, errors.New("integration configuration is readable by other users; run chmod 600 on the file")
	}
	file, err := os.Open(path)
	if err != nil {
		return empty, errors.New("cannot open integration configuration file")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return empty, errors.New("integration configuration changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		return empty, errors.New("cannot read integration configuration or file exceeds 64 KiB")
	}
	// Never include JSON parser errors: malformed field names or values can contain credentials.
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var cfg liveConfig
	if decoder.Decode(&cfg) != nil {
		return empty, errors.New("invalid integration JSON; check field names and types against the example")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return empty, errors.New("integration configuration must contain exactly one JSON object")
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]liveProvider{}
	}
	return cfg, nil
}
func resolveLiveCases(cfg liveConfig, getenv envLookup) ([]liveCase, int, error) {
	rawGetenv := getenv
	getenv = func(name string) (string, bool) { return appconfig.LookupEnvironmentWith(name, rawGetenv) }
	selected, err := liveProviderSelection(cfg, getenv)
	if err != nil {
		return nil, 0, err
	}
	timeout, err := liveTimeout(cfg.TimeoutSeconds, getenv)
	if err != nil {
		return nil, 0, err
	}
	var result []liveCase
	for _, spec := range providerSpecs {
		entry, enabled, err := resolveLiveEntry(spec, cfg.Providers[spec.name], selected, getenv)
		if err != nil {
			return nil, 0, err
		}
		if !enabled {
			continue
		}
		environment, err := liveEnvironment(spec, entry, getenv)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, liveCase{spec: spec, config: entry, environment: environment})
	}
	if len(result) == 0 {
		return nil, 0, errors.New("no live provider selected; enable a provider in integration.json or set OPCODE_TEST_<PROVIDER>_MODEL")
	}
	return result, timeout, nil
}

func liveProviderSelection(cfg liveConfig, getenv envLookup) (map[string]bool, error) {
	known := map[string]bool{}
	for _, spec := range providerSpecs {
		known[spec.name] = true
	}
	for name := range cfg.Providers {
		if !known[name] {
			return nil, errors.New("unrecognized integration provider; use a provider name from the example")
		}
	}
	selected := map[string]bool{}
	value, _ := getenv("OPCODE_LIVE_PROVIDERS")
	if value == "" {
		return selected, nil
	}
	for name := range strings.SplitSeq(value, ",") {
		name = strings.TrimSpace(name)
		if !known[name] {
			return nil, errors.New("invalid OPCODE_LIVE_PROVIDERS selection")
		}
		selected[name] = true
	}
	return selected, nil
}
func liveTimeout(timeout int, getenv envLookup) (int, error) {
	if value, ok := getenv("OPCODE_LIVE_TIMEOUT_SECONDS"); ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return 0, errors.New("OPCODE_LIVE_TIMEOUT_SECONDS must be an integer")
		}
		timeout = parsed
	}
	if timeout == 0 {
		timeout = 180
	}
	if timeout < 1 || timeout > 600 {
		return 0, errors.New("timeoutSeconds must be between 1 and 600")
	}
	return timeout, nil
}
func resolveLiveEntry(spec providerSpec, entry liveProvider, selected map[string]bool, getenv envLookup) (liveProvider, bool, error) {
	prefix := "OPCODE_TEST_" + strings.ToUpper(strings.ReplaceAll(spec.name, "-", "_")) + "_"
	envModel, hasModel := getenv(prefix + "MODEL")
	enabled := entry.Enabled || hasModel && envModel != ""
	if len(selected) > 0 {
		enabled = selected[spec.name]
	}
	if !enabled {
		return entry, false, nil
	}
	if hasModel {
		entry.Model = envModel
	}
	if strings.TrimSpace(entry.Model) == "" {
		return entry, false, fmt.Errorf("%s: configure model or %sMODEL", spec.name, prefix)
	}
	keyEnv := entry.APIKeyEnv
	if keyEnv == "" {
		keyEnv = spec.keyEnv
	}
	if keyEnv != "" {
		if value, ok := getenv(keyEnv); ok {
			entry.APIKey = value
		}
	}
	if !spec.optionalKey && entry.APIKey == "" {
		return entry, false, fmt.Errorf("%s: API credential is missing; configure apiKey or its environment variable", spec.name)
	}
	if err := liveGenerationEnvironment(spec, &entry, prefix, getenv); err != nil {
		return entry, false, err
	}
	if err := validateLiveGeneration(spec, &entry); err != nil {
		return entry, false, err
	}
	return entry, true, nil
}
func liveGenerationEnvironment(spec providerSpec, entry *liveProvider, prefix string, getenv envLookup) error {
	if value, ok := getenv(prefix + "MAX_TOKENS"); ok {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("%s: MAX_TOKENS must be an integer", spec.name)
		}
		entry.MaxTokens = parsed
	}
	if value, ok := getenv(prefix + "CAN_REASON"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%s: CAN_REASON must be true or false", spec.name)
		}
		entry.CanReason = parsed
	}
	if value, ok := getenv(prefix + "REASONING_EFFORT"); ok {
		entry.ReasoningEffort = value
	}
	if value, ok := getenv(prefix + "THINKING_MODE"); ok {
		entry.ThinkingMode = value
	}
	return nil
}
func validateLiveGeneration(spec providerSpec, entry *liveProvider) error {
	if entry.MaxTokens == 0 {
		entry.MaxTokens = 512
		if entry.CanReason || entry.ThinkingMode != "" {
			entry.MaxTokens = 4096
		}
	}
	if entry.MaxTokens < 1 || entry.MaxTokens > 16384 {
		return fmt.Errorf("%s: maxTokens must be between 1 and 16384", spec.name)
	}
	switch entry.ReasoningEffort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh":
	default:
		return fmt.Errorf("%s: invalid reasoningEffort", spec.name)
	}
	if entry.ReasoningEffort != "" && !entry.CanReason {
		return fmt.Errorf("%s: reasoningEffort requires canReason", spec.name)
	}
	if entry.ThinkingMode != "" && (spec.provider != models.ProviderAnthropic && spec.provider != models.ProviderBedrock || entry.ThinkingMode != "enabled" && entry.ThinkingMode != "adaptive") {
		return fmt.Errorf("%s: invalid thinkingMode", spec.name)
	}
	return nil
}
func liveEnvironment(spec providerSpec, entry liveProvider, getenv envLookup) (map[string]string, error) {
	environment := map[string]string{}
	bind := func(name, fallback string) string {
		if value, ok := getenv(name); ok {
			return value
		}
		if fallback != "" {
			environment[name] = fallback
		}
		return fallback
	}
	switch spec.provider {
	case models.ProviderLocal:
		if bind("LOCAL_ENDPOINT", entry.Endpoint) == "" {
			return nil, errors.New("local: configure endpoint or LOCAL_ENDPOINT")
		}
	case models.ProviderAzure:
		if bind("AZURE_OPENAI_ENDPOINT", entry.Endpoint) == "" || bind("AZURE_OPENAI_API_VERSION", entry.APIVersion) == "" {
			return nil, fmt.Errorf("%s: configure Azure endpoint and apiVersion", spec.name)
		}
		environment["AZURE_OPENAI_API_KEY"] = entry.APIKey
	case models.ProviderVertexAI:
		if err := liveVertexEnvironment(entry, getenv, bind); err != nil {
			return nil, err
		}
	case models.ProviderBedrock:
		liveBedrockEnvironment(entry, getenv, bind, environment)
	case models.ProviderCopilot:
		environment["GITHUB_TOKEN"] = entry.APIKey
	}
	return environment, nil
}
func liveVertexEnvironment(entry liveProvider, getenv envLookup, bind func(string, string) string) error {
	project := entry.Project
	if value, ok := getenv("GOOGLE_CLOUD_PROJECT"); ok {
		project = value
	}
	if bind("VERTEXAI_PROJECT", project) == "" {
		return errors.New("vertexai: configure project or VERTEXAI_PROJECT")
	}
	location := entry.Location
	if value, ok := getenv("GOOGLE_CLOUD_LOCATION"); ok {
		location = value
	}
	bind("VERTEXAI_LOCATION", location)
	bind("GOOGLE_APPLICATION_CREDENTIALS", entry.CredentialsFile)
	return nil
}
func liveBedrockEnvironment(entry liveProvider, getenv envLookup, bind func(string, string) string, environment map[string]string) {
	region := entry.Region
	if value, ok := getenv("AWS_DEFAULT_REGION"); ok {
		region = value
	}
	bind("AWS_REGION", region)
	bind("AWS_PROFILE", entry.AWSProfile)
	if entry.APIKey != "" {
		environment["AWS_BEARER_TOKEN_BEDROCK"] = entry.APIKey
	}
}
