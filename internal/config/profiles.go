package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
)

// ProviderProfile separates a user-chosen connection name from its wire backend.
// The embedded settings preserve the legacy providers JSON shape.
type ProviderProfile struct {
	Name     string               `json:"name,omitempty"`
	Backend  models.ModelProvider `json:"backend"`
	Provider `mapstructure:",squash"`
}

func knownBackend(id models.ModelProvider) bool {
	switch id {
	case models.ProviderLocal, models.ProviderOpenAI, models.ProviderAnthropic, models.ProviderGemini, models.ProviderOpenRouter, models.ProviderGROQ, models.ProviderXAI, models.ProviderBedrock, models.ProviderAzure, models.ProviderVertexAI, models.ProviderCopilot:
		return true
	}
	return false
}
func validateProfileID(id string) error {
	if len(id) == 0 || len(id) > 64 {
		return fmt.Errorf("profile ID must contain 1–64 lowercase letters, digits, underscores or hyphens")
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return fmt.Errorf("profile ID must contain only lowercase letters, digits, underscores or hyphens")
		}
	}
	return nil
}
func validEnvironmentName(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for i, r := range name {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func ProviderForProfile(id string) (models.ModelProvider, Provider, error) {
	if cfg == nil {
		return "", Provider{}, fmt.Errorf("config not loaded")
	}
	profile, ok := cfg.ProviderProfiles[id]
	if !ok {
		return "", Provider{}, fmt.Errorf("provider profile %q is not configured", id)
	}
	if profile.Disabled {
		return "", Provider{}, fmt.Errorf("provider profile %q is disabled", id)
	}
	return profile.Backend, profile.Provider, nil
}
func ProviderForModel(model models.Model) (Provider, error) {
	if cfg == nil {
		return Provider{}, fmt.Errorf("config not loaded")
	}
	if model.ProviderProfile != "" {
		backend, p, err := ProviderForProfile(model.ProviderProfile)
		if err != nil {
			return Provider{}, err
		}
		if backend != model.Provider {
			return Provider{}, fmt.Errorf("model backend differs from provider profile %q", model.ProviderProfile)
		}
		return p, nil
	}
	p, ok := cfg.Providers[model.Provider]
	if !ok || p.Disabled {
		return Provider{}, fmt.Errorf("provider %s is not enabled", model.Provider)
	}
	return p, nil
}
func ProfileModelCatalog(id string) func(context.Context) ([]models.Model, error) {
	backend, p, err := ProviderForProfile(id)
	if err != nil {
		return func(context.Context) ([]models.Model, error) { return nil, err }
	}
	return modelCatalog(backend, id, p)
}

// ConfiguredProfileModels provides an offline catalog with distinct IDs even
// when two profiles expose the same provider model ID.
func ConfiguredProfileModels(id string) []models.Model {
	backend, p, err := ProviderForProfile(id)
	if err != nil {
		return nil
	}
	byWire := map[string]models.Model{}
	for _, model := range models.SupportedModels {
		if model.Provider == backend && model.ProviderProfile == "" {
			model.ID = models.ModelID("profile." + id + "." + model.APIModel)
			model.ProviderProfile = id
			byWire[model.APIModel] = providerModelDefaults(p, model)
		}
	}
	for _, model := range models.SupportedModels {
		if model.ProviderProfile == id {
			byWire[model.APIModel] = model
		}
	}
	result := make([]models.Model, 0, len(byWire))
	for _, model := range byWire {
		result = append(result, model)
	}
	slices.SortFunc(result, func(a, b models.Model) int { return strings.Compare(a.APIModel, b.APIModel) })
	return result
}

// PreviewProviderProfile resolves an environment reference only in the returned
// value. Keep the caller's raw settings when saving a connection.
func PreviewProviderProfile(id string, profile ProviderProfile) (ProviderProfile, error) {
	if err := validateProfileID(id); err != nil {
		return ProviderProfile{}, err
	}
	if len(profile.Name) > 128 || strings.ContainsFunc(profile.Name, unicode.IsControl) {
		return ProviderProfile{}, fmt.Errorf("profile name must be at most 128 printable characters")
	}
	if profile.Backend == models.ProviderLocal && profile.BaseURL == "" {
		return ProviderProfile{}, fmt.Errorf("local profile requires baseURL")
	}
	p, err := resolveProvider(profile.Backend, profile.Provider)
	if err != nil {
		return ProviderProfile{}, err
	}
	if p.APIKey == "" && profile.Backend != models.ProviderLocal && profile.Backend != models.ProviderBedrock && profile.Backend != models.ProviderVertexAI && profile.Backend != models.ProviderAzure {
		return ProviderProfile{}, fmt.Errorf("set an API key environment variable for this backend")
	}
	profile.Provider = p
	return profile, nil
}
func TestProviderProfile(id string, profile ProviderProfile) func(context.Context) ([]models.Model, error) {
	resolved, err := PreviewProviderProfile(id, profile)
	if err != nil {
		return func(context.Context) ([]models.Model, error) { return nil, err }
	}
	return func(ctx context.Context) ([]models.Model, error) {
		return models.DiscoverProviderModels(ctx, resolved.Backend, resolved.BaseURL, resolved.APIKey)
	}
}
func SaveProviderProfile(id string, profile ProviderProfile) error {
	resolved, err := PreviewProviderProfile(id, profile)
	if err != nil {
		return err
	}
	raw := profile
	if raw.APIKeyEnv != "" {
		raw.APIKey = ""
	}
	err = updateProfileFile(id, func(file *Config) {
		if file.ProviderProfiles == nil {
			file.ProviderProfiles = map[string]ProviderProfile{}
		}
		file.ProviderProfiles[id] = raw
	})
	if err != nil {
		return err
	}
	credentialMu.Lock()
	if cfg.ProviderProfiles == nil {
		cfg.ProviderProfiles = map[string]ProviderProfile{}
	}
	cfg.ProviderProfiles[id] = resolved
	credentialMu.Unlock()
	return nil
}
func updateProfileFile(id string, update func(*Config)) error {
	path := ProjectConfigPath(cfg.WorkingDir)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot read project configuration: %w", err)
	}
	if err == nil {
		var file Config
		if json.Unmarshal(data, &file) != nil {
			return fmt.Errorf("invalid project configuration JSON")
		}
		if _, exists := file.ProviderProfiles[id]; exists {
			return patchConfigFile(path, data, update)
		}
	}
	return updateCfgFile(update)
}
func SavePreferences(preferences Preferences, runtime RuntimeConfig) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if err := runtime.Validate(); err != nil {
		return err
	}
	preferences.FavoriteModels = uniqueModels(preferences.FavoriteModels, 100)
	preferences.RecentModels = uniqueModels(preferences.RecentModels, 20)
	if err := updateCfgFile(func(file *Config) { file.Preferences = preferences; file.Runtime = runtime }); err != nil {
		return err
	}
	cfg.Preferences = preferences
	cfg.Runtime = runtime.Defaults()
	return nil
}
func ToggleFavoriteModel(id models.ModelID) error {
	prefs := cfg.Preferences
	prefs.FavoriteModels = slices.Clone(prefs.FavoriteModels)
	if i := slices.Index(prefs.FavoriteModels, id); i >= 0 {
		prefs.FavoriteModels = append(prefs.FavoriteModels[:i], prefs.FavoriteModels[i+1:]...)
	} else {
		prefs.FavoriteModels = append(prefs.FavoriteModels, id)
	}
	return SavePreferences(prefs, cfg.Runtime)
}
func uniqueModels(input []models.ModelID, limit int) []models.ModelID {
	result := []models.ModelID{}
	for _, id := range input {
		if id != "" && !slices.Contains(result, id) {
			result = append(result, id)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

// SaveProvider edits an existing legacy connection without migrating its IDs.
func SaveProvider(backend models.ModelProvider, raw Provider) error {
	resolved, err := resolveProvider(backend, raw)
	if err != nil {
		return err
	}
	if raw.APIKeyEnv != "" {
		raw.APIKey = ""
	}
	update := func(file *Config) {
		if file.Providers == nil {
			file.Providers = map[models.ModelProvider]Provider{}
		}
		file.Providers[backend] = raw
	}
	path := ProjectConfigPath(cfg.WorkingDir)
	data, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("cannot read project configuration: %w", readErr)
	}
	project := false
	if readErr == nil {
		var file Config
		if json.Unmarshal(data, &file) != nil {
			return fmt.Errorf("invalid project configuration JSON")
		}
		_, project = file.Providers[backend]
	}
	if project {
		err = patchConfigFile(path, data, update)
	} else {
		err = updateCfgFile(update)
	}
	if err != nil {
		return err
	}
	credentialMu.Lock()
	if cfg.Providers == nil {
		cfg.Providers = map[models.ModelProvider]Provider{}
	}
	cfg.Providers[backend] = resolved
	credentialMu.Unlock()
	return nil
}

// StoredProviderKey reads only literal configuration, never resolved environment
// values. Used to preserve an existing private-file key while editing metadata.
func StoredProviderKey(id string, legacy bool) string {
	paths := []string{ProjectConfigPath(cfg.WorkingDir)}
	if path := configuredFilePath(); path != "" {
		paths = append(paths, path)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var file Config
		if json.Unmarshal(data, &file) != nil {
			continue
		}
		if legacy {
			if p, ok := file.Providers[models.ModelProvider(id)]; ok {
				if p.APIKeyEnv != "" {
					return ""
				}
				return p.APIKey
			}
		} else {
			if p, ok := file.ProviderProfiles[id]; ok {
				if p.APIKeyEnv != "" {
					return ""
				}
				return p.APIKey
			}
		}
	}
	return ""
}
func ProviderEnvironmentName(id models.ModelProvider) string {
	names := map[models.ModelProvider]string{models.ProviderLocal: "LOCAL_API_KEY", models.ProviderOpenAI: "OPENAI_API_KEY", models.ProviderAnthropic: "ANTHROPIC_API_KEY", models.ProviderGemini: "GEMINI_API_KEY", models.ProviderOpenRouter: "OPENROUTER_API_KEY", models.ProviderGROQ: "GROQ_API_KEY", models.ProviderXAI: "XAI_API_KEY", models.ProviderCopilot: "GITHUB_TOKEN", models.ProviderAzure: "AZURE_OPENAI_API_KEY"}
	name := names[id]
	if name != "" && os.Getenv(name) != "" {
		return name
	}
	return ""
}

func configuredFilePath() string {
	if path := viper.ConfigFileUsed(); path != "" {
		return path
	}
	path, _ := globalConfigPath()
	return path
}

func requiresAPIKey(backend models.ModelProvider) bool {
	return backend != models.ProviderLocal && backend != models.ProviderBedrock && backend != models.ProviderVertexAI && backend != models.ProviderAzure
}
