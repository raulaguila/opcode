package config

import (
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/raulaguila/opcode/internal/localstate"
)

//go:embed template.json
var defaultConfigTemplate []byte

// ensureGlobalTemplate creates the canonical global file, preserving legacy JSON
// and leaving the original for rollback. A hard link publishes the fully written
// private file atomically without replacing a concurrently created file.
func ensureGlobalTemplate() error {
	source, exists := globalConfigPath()
	if source == "" {
		return fmt.Errorf("cannot determine the user configuration directory")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, "."+appName, "config.json")
	if source == path && exists {
		return nil
	}
	data := defaultConfigTemplate
	if exists {
		// Bound reads, reject symlinks, preserve secrets and large numbers exactly.
		var original json.RawMessage
		if err := localstate.Read(source, 4<<20, &original); err != nil {
			return fmt.Errorf("read config: cannot adopt legacy global configuration: %w", err)
		}
		if err := ValidateDocument(source); err != nil {
			return err
		}
		data = original
	}
	return publishTemplate(path, data)
}

// ensureReferenceTemplate keeps a separate, credential-free example available
// even after the active configuration has been filled in or becomes invalid.
func ensureReferenceTemplate() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return publishTemplate(filepath.Join(home, appName, "config.example.json"), defaultConfigTemplate)
}

// publishTemplate never replaces an existing regular file, including one
// created by another instance while this template is being written.
func publishTemplate(path string, data []byte) error {
	root, err := localstate.OpenPrivateRoot(filepath.Dir(path), true)
	if err != nil {
		return fmt.Errorf("cannot create private configuration directory: %w", err)
	}
	defer root.Close()
	if info, err := root.Lstat(filepath.Base(path)); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("configuration template must be a regular file: %s", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	temp := ".opcode-template-" + rand.Text()
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("cannot create configuration template: %w", err)
	}
	defer root.Remove(temp)
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("cannot save configuration template: %w", writeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if err := root.Link(temp, filepath.Base(path)); err != nil && !os.IsExist(err) {
		return fmt.Errorf("cannot publish configuration template: %w", err)
	}
	info, err := root.Lstat(filepath.Base(path))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("configuration template must be a regular file: %s", path)
	}
	return nil
}
