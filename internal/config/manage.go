package config

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/raulaguila/opcode/internal/localstate"
)

const SchemaVersion = 1

// Paths does not initialize configuration or contact a provider.
func Paths(root string) (global, project string) {
	return configuredFilePath(), ProjectConfigPath(root)
}

// ValidateDocument validates the file without applying environment values or
// defaults, initializing state, checking credentials or making network requests.
// Unknown fields survive upgrades; known fields must retain their JSON types.
func ValidateDocument(path string) error {
	doc, err := readSettingsDocument(path)
	if err != nil {
		return fmt.Errorf("configuration %s: %w", path, err)
	}
	raw, _ := json.Marshal(doc)
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		var typed *json.UnmarshalTypeError
		if errors.As(err, &typed) {
			return fmt.Errorf("configuration %s: field %s must be %s (values omitted)", path, typed.Field, typed.Type)
		}
		return fmt.Errorf("configuration %s: invalid field type (values omitted)", path)
	}
	if c.SchemaVersion < 0 || c.SchemaVersion > SchemaVersion {
		return fmt.Errorf("configuration %s: unsupported schemaVersion; maximum supported is %d", path, SchemaVersion)
	}
	if err := c.Runtime.Validate(); err != nil {
		return fmt.Errorf("configuration %s: runtime: %w", path, err)
	}
	if err := c.TUI.Validate(); err != nil {
		return fmt.Errorf("configuration %s: tui: %w", path, err)
	}
	if c.Security.Sandbox != "" && c.Security.Sandbox != "none" && c.Security.Sandbox != "bubblewrap" {
		return fmt.Errorf("configuration %s: security.sandbox must be bubblewrap or none", path)
	}
	for id, p := range c.ProviderProfiles {
		prefix := "providerProfiles." + id
		if validateProfileID(id) != nil {
			return fmt.Errorf("configuration %s: providerProfiles contains an invalid profile ID", path)
		}
		if !knownBackend(p.Backend) {
			return fmt.Errorf("configuration %s: %s.backend is unsupported", path, prefix)
		}
		if p.API != "" && p.API != "ollama" && p.API != "responses" && p.API != "chat_completions" {
			return fmt.Errorf("configuration %s: %s.api is unsupported", path, prefix)
		}
		if p.ContextWindow < 0 || p.DefaultMaxTokens < 0 {
			return fmt.Errorf("configuration %s: %s token limits must be nonnegative", path, prefix)
		}
		if p.APIKeyEnv != "" && !validEnvironmentName(p.APIKeyEnv) {
			return fmt.Errorf("configuration %s: %s.apiKeyEnv must name an environment variable", path, prefix)
		}
	}
	return nil
}

// MigrateDocument is explicit and in-place: legacy locations keep working. It
// saves an exact private backup before adding the version, never moves secrets.
func MigrateDocument(path string) (string, bool, error) {
	unlock := localstate.Lock(path)
	defer unlock()
	if err := ValidateDocument(path); err != nil {
		return "", false, err
	}
	doc, err := readSettingsDocument(path)
	if err != nil {
		return "", false, err
	}
	if v, ok := doc["schemaVersion"]; ok && fmt.Sprint(v) == fmt.Sprint(SchemaVersion) {
		return "", false, nil
	}
	root, err := localstate.OpenPrivateRoot(filepath.Dir(path), false)
	if err != nil {
		return "", false, err
	}
	defer root.Close()
	name := filepath.Base(path)
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("configuration must exist as a regular file")
	}
	source, err := root.Open(name)
	if err != nil {
		return "", false, err
	}
	opened, err := source.Stat()
	if err != nil || !os.SameFile(info, opened) {
		source.Close()
		return "", false, fmt.Errorf("configuration changed during migration")
	}
	raw, err := io.ReadAll(io.LimitReader(source, maxSettingsDocument+1))
	source.Close()
	if err != nil || len(raw) > maxSettingsDocument {
		return "", false, fmt.Errorf("cannot read bounded configuration")
	}
	var original map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&original) != nil {
		return "", false, fmt.Errorf("invalid configuration")
	}
	a0, _ := json.Marshal(doc)
	b0, _ := json.Marshal(original)
	if string(a0) != string(b0) {
		return "", false, fmt.Errorf("configuration changed before backup")
	}
	backupDirectory := filepath.Join(filepath.Dir(path), ".opcode", "config-backups")
	if filepath.Base(filepath.Dir(path)) == ".opcode" {
		backupDirectory = filepath.Join(filepath.Dir(path), "config-backups")
	}
	backupRoot, err := localstate.OpenPrivateRoot(backupDirectory, true)
	if err != nil {
		return "", false, err
	}
	defer backupRoot.Close()
	backupName := name + ".backup-" + rand.Text()
	f, err := backupRoot.OpenFile(backupName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", false, err
	}
	_, werr := f.Write(raw)
	serr := f.Sync()
	cerr := f.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		backupRoot.Remove(backupName)
		return "", false, err
	}
	current, err := readSettingsDocument(path)
	if err != nil {
		return "", false, err
	}
	a, _ := json.Marshal(doc)
	b, _ := json.Marshal(current)
	if string(a) != string(b) {
		return "", false, fmt.Errorf("configuration changed; backup retained, retry migration")
	}
	doc["schemaVersion"] = SchemaVersion
	if err := localstate.Write(path, doc, maxSettingsDocument); err != nil {
		return "", false, err
	}
	return filepath.Join(backupDirectory, backupName), true, nil
}

// RedactedDocument uses a strict allowlist so extension/unknown fields cannot
// accidentally disclose credentials. EffectiveConfiguration is for the UI;
// this document export deliberately omits commands, arguments and raw env maps.
func RedactedDocument() map[string]any {
	result := map[string]any{}
	for _, value := range EffectiveConfiguration() {
		path := value.Path
		allowed := strings.HasPrefix(path, "tui.") || strings.HasPrefix(path, "runtime.") || strings.HasPrefix(path, "preferences.") || path == "schemaVersion" || path == "data.directory" || path == "wd"
		if strings.HasPrefix(path, "providerProfiles.") || strings.HasPrefix(path, "providers.") {
			parts := strings.Split(path, ".")
			if len(parts) == 3 {
				switch parts[2] {
				case "backend", "disabled", "api", "apiKeyEnv", "contextWindow", "maxTokens", "defaultMaxTokens":
					allowed = true
				}
			}
		}
		if allowed && !value.Secret {
			result[path] = map[string]any{"value": value.Value, "origin": value.Origin}
		}
	}
	return result
}
