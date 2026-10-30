package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/raulaguila/opcode/internal/llm/models"
)

// ModelSelection validates a candidate without changing the active configuration.
func ModelSelection(name AgentName, model models.Model) (models.Model, Agent, error) {
	model = ModelWithProviderDefaults(model)
	normalized, err := normalizeModel(model.ID, model)
	if err != nil {
		return models.Model{}, Agent{}, err
	}
	p, providerErr := ProviderForModel(normalized)
	if providerErr != nil || p.Disabled || (p.APIKey == "" && normalized.Provider != models.ProviderLocal && normalized.Provider != models.ProviderBedrock && normalized.Provider != models.ProviderVertexAI && normalized.Provider != models.ProviderAzure) {
		return models.Model{}, Agent{}, fmt.Errorf("provider %s is not enabled", normalized.Provider)
	}
	agent := cfg.Agents[name]
	agent.Model = normalized.ID
	agent.MaxTokens = min(normalized.DefaultMaxTokens, normalized.ContextWindow/2)
	if !normalized.CanReason {
		agent.ReasoningEffort = ""
	}
	return normalized, agent, nil
}

func UpdateAgentModelDefinition(name AgentName, model models.Model) error {
	model, agent, err := ModelSelection(name, model)
	if err != nil {
		return err
	}
	// Discovered context belongs to the runtime, not the user's settings.
	// Omission requests discovery on restart; positive manual overrides remain.
	configured := model
	if model.UsesAutomaticContext() {
		configured.ContextWindow = 0
		configured.ContextSource = ""
		configured.ContextEstimated = false
		configured.ContextAuto = false
	}
	update := func(file *Config) {
		ensureModelSelectionMaps(file)
		file.Agents[name] = agent
		file.Models[model.ID] = configured
		file.Preferences.RecentModels = uniqueModels(append([]models.ModelID{model.ID}, cfg.Preferences.RecentModels...), 20)
		inheritModelSelection(file, name, agent)
	}
	err = updateModelSelectionFile(name, update)
	if err != nil {
		return err
	}
	ensureModelSelectionMaps(cfg)
	cfg.Agents[name] = agent
	cfg.Preferences.RecentModels = uniqueModels(append([]models.ModelID{model.ID}, cfg.Preferences.RecentModels...), 20)
	inheritModelSelection(cfg, name, agent)
	cfg.Models[model.ID] = configured
	models.SupportedModels[model.ID] = model
	return nil
}

func updateModelSelectionFile(name AgentName, update func(*Config)) error {
	// A project agent setting overrides the global one at load time. Save back
	// to that project file when present so a restart keeps the user's choice.
	path := ProjectConfigPath(cfg.WorkingDir)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot read project configuration: %w", err)
	}
	if err == nil {
		var local Config
		if json.Unmarshal(data, &local) != nil {
			return fmt.Errorf("invalid project configuration JSON")
		}
		if local.Agents[name].Model != "" {
			return patchConfigFile(path, data, update)
		}
	}
	return updateCfgFile(update)
}

// patchConfigFile applies only changed typed fields to the original JSON.
// Unknown fields and credential references survive; resolved environment keys
// never enter this document. Write to a private temp file and rename atomically.
func patchConfigFile(path string, data []byte, update func(*Config)) error {
	var original map[string]json.RawMessage
	var before Config
	if json.Unmarshal(data, &original) != nil || original == nil || json.Unmarshal(data, &before) != nil {
		return fmt.Errorf("invalid configuration JSON")
	}
	oldJSON, err := json.Marshal(before)
	if err != nil {
		return fmt.Errorf("cannot encode configuration")
	}
	update(&before)
	newJSON, err := json.Marshal(before)
	if err != nil {
		return fmt.Errorf("cannot encode updated configuration")
	}
	patched := mergeChangedJSON(data, oldJSON, newJSON)
	var formatted bytes.Buffer
	if err = json.Indent(&formatted, patched, "", "  "); err != nil {
		return fmt.Errorf("cannot format updated configuration")
	}
	formatted.WriteByte('\n')
	file, err := os.CreateTemp(filepath.Dir(path), ".opcode-config-*")
	if err != nil {
		return fmt.Errorf("cannot create configuration file: %w", err)
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err = file.Write(formatted.Bytes()); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("cannot save configuration: %w", err)
	}
	if err = os.Rename(temp, path); err != nil {
		return fmt.Errorf("cannot replace configuration: %w", err)
	}
	return nil
}

func mergeChangedJSON(original, before, after json.RawMessage) json.RawMessage {
	if reflect.DeepEqual(before, after) {
		return original
	}
	var a, b, c map[string]json.RawMessage
	if json.Unmarshal(before, &b) != nil || b == nil || json.Unmarshal(after, &c) != nil || c == nil {
		return after
	}
	if json.Unmarshal(original, &a) != nil || a == nil {
		a = map[string]json.RawMessage{}
	}
	for key, value := range c {
		if bytes.Equal(b[key], value) {
			continue
		}
		a[key] = mergeChangedJSON(a[key], b[key], value)
	}
	for key := range b {
		if _, ok := c[key]; !ok {
			delete(a, key)
		}
	}
	result, _ := json.Marshal(a)
	return result
}

func inheritModelSelection(target *Config, name AgentName, agent Agent) {
	if name == AgentCoder {
		for _, auxiliary := range []AgentName{AgentTitle, AgentSummarizer, AgentTask} {
			if cfg.Agents[auxiliary].Model == "" {
				inherited := agent
				if auxiliary == AgentTitle {
					inherited.MaxTokens = min(80, agent.MaxTokens)
				}
				target.Agents[auxiliary] = inherited
			}
		}
	}
}

func ensureModelSelectionMaps(file *Config) {
	if file.Agents == nil {
		file.Agents = map[AgentName]Agent{}
	}
	if file.Models == nil {
		file.Models = map[models.ModelID]models.Model{}
	}
}
