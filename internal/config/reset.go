package config

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/raulaguila/opcode/internal/localstate"
)

// ResetGlobalConfig restores the built-in defaults before configuration is
// loaded. It backs up the selected global file exactly, including invalid JSON,
// and leaves project configuration and application data in place.
func ResetGlobalConfig() (path, backup string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	path = filepath.Join(home, "."+appName, "config.json")
	unlock := localstate.Lock(path)
	defer unlock()
	source, exists := globalConfigPath()
	if !exists {
		return path, "", publishTemplate(path, defaultConfigTemplate)
	}

	raw, err := readResetSource(source)
	if err != nil {
		return path, "", err
	}
	backupDirectory := filepath.Join(filepath.Dir(path), "config-backups")
	root, err := localstate.OpenPrivateRoot(backupDirectory, true)
	if err != nil {
		return path, "", fmt.Errorf("cannot create configuration backup directory: %w", err)
	}
	defer root.Close()
	name := filepath.Base(source) + ".backup-" + rand.Text()
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return path, "", fmt.Errorf("cannot create configuration backup: %w", err)
	}
	_, writeErr := file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = root.Remove(name)
		return path, "", fmt.Errorf("cannot save configuration backup: %w", err)
	}
	backup = filepath.Join(backupDirectory, name)
	current, err := readResetSource(source)
	if err != nil || !bytes.Equal(raw, current) {
		return path, backup, fmt.Errorf("configuration changed before reset; backup retained at %s; retry with other instances closed", backup)
	}
	if source != path {
		// Legacy globals stay intact for rollback. A concurrent canonical file
		// must not be replaced without first being backed up on a later retry.
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return path, backup, fmt.Errorf("configuration destination appeared before reset; backup retained at %s; retry", backup)
		}
		if err := publishTemplate(path, defaultConfigTemplate); err != nil {
			return path, backup, fmt.Errorf("cannot reset configuration; backup retained at %s: %w", backup, err)
		}
		return path, backup, nil
	}
	if err := localstate.WriteBytes(path, defaultConfigTemplate); err != nil {
		return path, backup, fmt.Errorf("cannot reset configuration; backup retained at %s: %w", backup, err)
	}
	return path, backup, nil
}

// Reset must also recover malformed files, so this bounded read deliberately
// does not parse JSON. Pin directories and reject symlinks before reading.
func readResetSource(path string) ([]byte, error) {
	root, err := localstate.OpenPrivateRoot(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := filepath.Base(path)
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() > maxSettingsDocument {
		return nil, fmt.Errorf("configuration reset requires a regular file of at most %d bytes", maxSettingsDocument)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() || opened.Size() > maxSettingsDocument {
		return nil, fmt.Errorf("configuration changed during reset")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxSettingsDocument+1))
	if err != nil || len(raw) > maxSettingsDocument {
		return nil, fmt.Errorf("cannot read bounded configuration for reset")
	}
	return raw, nil
}
