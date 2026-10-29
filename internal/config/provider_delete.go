package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/models"
)

// DeleteProvider removes an unused connection from both loaded configuration
// layers. Legacy backends retain an empty disabled marker to suppress env-based
// recreation; no endpoint or credential is retained in that marker.
func DeleteProvider(id string, legacy bool) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	if legacy {
		if _, ok := cfg.Providers[models.ModelProvider(id)]; !ok {
			return fmt.Errorf("provider not configured")
		}
	} else if _, ok := cfg.ProviderProfiles[id]; !ok {
		return fmt.Errorf("provider profile not configured")
	}
	matches := func(key models.ModelID, model models.Model) bool {
		if model.Provider == "" {
			if known, ok := models.SupportedModels[key]; ok {
				model = known
			}
		}
		if legacy {
			return model.ProviderProfile == "" && (model.Provider == models.ModelProvider(id) || model.Provider == "" && strings.HasPrefix(string(key), id+"."))
		}
		return model.ProviderProfile == id || strings.HasPrefix(string(key), "profile."+id+".")
	}
	checkAgents := func(c *Config) error {
		var users []string
		for name, agent := range c.Agents {
			if agent.Model != "" && matches(agent.Model, c.Models[agent.Model]) {
				users = append(users, string(name))
			}
		}
		slices.Sort(users)
		if len(users) > 0 {
			return fmt.Errorf(Text("Conexão usada por %s; escolha outros modelos antes de excluir", "Connection used by %s; choose replacement models before deleting"), strings.Join(users, ", "))
		}
		return nil
	}
	if err := checkAgents(cfg); err != nil {
		return err
	}
	type snapshot struct {
		path string
		raw  []byte
	}
	var snapshots []snapshot
	seen := map[string]bool{}
	for _, path := range []string{configuredFilePath(), ProjectConfigPath(cfg.WorkingDir)} {
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot read configuration for deletion: %w", err)
		}
		var file Config
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &file) != nil || json.Unmarshal(raw, &object) != nil || object == nil {
			return fmt.Errorf("invalid configuration JSON; nothing deleted")
		}
		if err := checkAgents(&file); err != nil {
			return err
		}
		_, touched := file.ProviderProfiles[id]
		if legacy {
			_, touched = file.Providers[models.ModelProvider(id)]
			touched = touched || path == filepath.Clean(configuredFilePath())
		}
		for key, model := range file.Models {
			touched = touched || matches(key, model)
		}
		if !touched {
			continue
		}
		snapshots = append(snapshots, snapshot{path, raw})
	}
	if len(snapshots) == 0 {
		return fmt.Errorf("no configuration file available for deletion")
	}
	remove := func(c *Config) {
		if legacy {
			if c.Providers == nil {
				c.Providers = map[models.ModelProvider]Provider{}
			}
			c.Providers[models.ModelProvider(id)] = Provider{Disabled: true}
		} else {
			delete(c.ProviderProfiles, id)
		}
		for key, model := range c.Models {
			if matches(key, model) {
				delete(c.Models, key)
			}
		}
	}
	for i, file := range snapshots {
		raw := file.raw
		var err error
		if legacy {
			raw, err = legacyRemovalMarker(raw, id)
		}
		if err == nil {
			err = patchConfigFile(file.path, raw, remove)
		}
		if err != nil {
			for j := i - 1; j >= 0; j-- {
				if restoreErr := restoreProviderConfig(snapshots[j].path, snapshots[j].raw); restoreErr != nil {
					err = errors.Join(err, fmt.Errorf("cannot restore prior configuration: %w", restoreErr))
				}
			}
			return err
		}
	}
	credentialMu.Lock()
	remove(cfg)
	credentialMu.Unlock()
	if !legacy {
		for key, model := range models.SupportedModels {
			if matches(key, model) {
				delete(models.SupportedModels, key)
			}
		}
	}
	return nil
}

// Replace the entire legacy entry, including fields unknown to this version.
// patchConfigFile otherwise preserves unknown children when updating an object.
func legacyRemovalMarker(raw []byte, id string) ([]byte, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	providers := map[string]json.RawMessage{}
	if entry, ok := document["providers"]; ok {
		if err := json.Unmarshal(entry, &providers); err != nil {
			return nil, err
		}
		if providers == nil {
			providers = map[string]json.RawMessage{}
		}
	}
	providers[id] = json.RawMessage(`{"disabled":true}`)
	entry, err := json.Marshal(providers)
	if err != nil {
		return nil, err
	}
	document["providers"] = entry
	return json.Marshal(document)
}

func restoreProviderConfig(path string, raw []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".opcode-restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
