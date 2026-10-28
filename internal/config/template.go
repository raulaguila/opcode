package config

import (
	"crypto/rand"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/raulaguila/opcode/internal/localstate"
)

//go:embed template.json
var defaultConfigTemplate []byte

// ensureGlobalTemplate expands the default only when no compatible global
// configuration exists. A hard link publishes the fully written private file
// atomically without replacing a file created concurrently by another process.
func ensureGlobalTemplate() error {
	path, exists := globalConfigPath()
	if path == "" {
		return fmt.Errorf("cannot determine the user configuration directory")
	}
	if exists {
		return nil
	}
	root, err := localstate.OpenPrivateRoot(filepath.Dir(path), true)
	if err != nil {
		return fmt.Errorf("cannot create private configuration directory: %w", err)
	}
	defer root.Close()
	temp := ".opcode-template-" + rand.Text()
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("cannot create configuration template: %w", err)
	}
	defer root.Remove(temp)
	_, writeErr := file.Write(defaultConfigTemplate)
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
	return nil
}
