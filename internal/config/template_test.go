package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestNestedGlobalAndProjectConfigurationWinWithoutCopyingOldFiles(t *testing.T) {
	isolateMigrationConfig(t)
	home, root := os.Getenv("HOME"), t.TempDir()
	global := filepath.Join(home, ".opcode", "config.json")
	project := filepath.Join(root, ".opcode", "config.json")
	writeMigrationJSON(t, filepath.Join(home, ".opcode.json"), `{"runtime":{"mode":"execute"}}`)
	writeMigrationJSON(t, global, `{"tui":{"language":"pt"},"security":{"sandbox":"none"},"future":9007199254740993}`)
	writeMigrationJSON(t, filepath.Join(root, ".opcode.json"), `{"runtime":{"mode":"execute"}}`)
	writeMigrationJSON(t, project, `{"runtime":{"mode":"plan"},"security":{"sandbox":"bubblewrap"}}`)
	loaded, err := LoadOffline(root)
	require.NoError(t, err)
	require.Equal(t, global, viper.ConfigFileUsed())
	require.Equal(t, project, ProjectConfigPath(root))
	require.Equal(t, "plan", loaded.Runtime.Mode)
	require.Equal(t, "pt", loaded.TUI.Language)
	require.Equal(t, "none", loaded.Security.Sandbox, "project configuration cannot override global security")
	require.NoError(t, SavePreferences(Preferences{ResumeLast: true}, loaded.Runtime))
	data, err := os.ReadFile(global)
	require.NoError(t, err)
	require.Contains(t, string(data), "9007199254740993")
	data, err = os.ReadFile(filepath.Join(home, ".opcode.json"))
	require.NoError(t, err)
	require.JSONEq(t, `{"runtime":{"mode":"execute"}}`, string(data))
}

func TestFreshStartupCreatesPrivateProviderTemplateOnceWithoutNetwork(t *testing.T) {
	isolateMigrationConfig(t)
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	calls := 0
	http.DefaultTransport = localDiscoveryTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("template must not connect")
	})
	root := t.TempDir()
	path := filepath.Join(os.Getenv("HOME"), ".opcode", "config.json")
	_, err := LoadOffline(root)
	require.NoError(t, err)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err), "offline diagnostics must remain read-only")
	cfg = nil
	viper.Reset()
	loaded, err := Load(root, false)
	require.NoError(t, err)
	require.Equal(t, path, viper.ConfigFileUsed())
	require.Zero(t, calls)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var document Config
	require.NoError(t, json.Unmarshal(data, &document))
	require.Equal(t, models.ProviderLocal, document.ProviderProfiles["ollama"].Backend)
	require.Equal(t, "ollama", document.ProviderProfiles["ollama"].API)
	require.Equal(t, "OPENAI_API_KEY", document.ProviderProfiles["openai"].APIKeyEnv)
	require.True(t, document.ProviderProfiles["openai"].Disabled)
	for _, p := range document.ProviderProfiles {
		require.Empty(t, p.APIKey)
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	info, err = os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0700), info.Mode().Perm())
	require.NoError(t, SavePreferences(Preferences{ResumeLast: true}, loaded.Runtime))
	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	cfg = nil
	viper.Reset()
	_, err = Load(root, false)
	require.NoError(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, updated, after, "startup must never overwrite user changes")
	require.Zero(t, calls)
}

func TestNewProjectConfigurationFolderKeepsExistingLegacyDatabase(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, ".opencode")
	writeMigrationJSON(t, filepath.Join(old, "opencode.db"), "existing database")
	writeMigrationJSON(t, filepath.Join(root, ".opcode", "config.json"), `{}`)
	require.Equal(t, old, defaultDataPath(root), "adding a configuration folder must not hide the existing database")
	writeMigrationJSON(t, filepath.Join(root, ".opcode", "opcode.db"), "new database")
	require.Equal(t, filepath.Join(root, ".opcode"), defaultDataPath(root))
}

func TestTemplateConcurrentStartupIsAtomicAndNeverOverwritesLegacy(t *testing.T) {
	isolateMigrationConfig(t)
	var group sync.WaitGroup
	errors := make(chan error, 16)
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := ensureGlobalTemplate(); err != nil {
				errors <- err
				return
			}
			data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".opcode", "config.json"))
			if err == nil && !json.Valid(data) {
				err = fmt.Errorf("partially written template observed")
			}
			errors <- err
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	path := filepath.Join(os.Getenv("HOME"), ".opcode", "config.json")
	require.NoError(t, os.Remove(path))
	old := filepath.Join(os.Getenv("HOME"), ".opcode.json")
	writeMigrationJSON(t, old, `{"future":"keep","providers":{"openai":{"apiKey":"private-original-value"}}}`)
	before, err := os.ReadFile(old)
	require.NoError(t, err)
	require.NoError(t, ensureGlobalTemplate())
	after, err := os.ReadFile(old)
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err), "legacy credentials must not be duplicated")
}

func TestTemplateFailureDoesNotWriteThroughSymlinkOrOverwriteMalformedConfig(t *testing.T) {
	isolateMigrationConfig(t)
	out := t.TempDir()
	directory := filepath.Join(os.Getenv("HOME"), ".opcode")
	require.NoError(t, os.Symlink(out, directory))
	require.Error(t, ensureGlobalTemplate())
	entries, err := os.ReadDir(out)
	require.NoError(t, err)
	require.Empty(t, entries)
	require.NoError(t, os.Remove(directory))
	path := filepath.Join(directory, "config.json")
	writeMigrationJSON(t, path, `{"broken":`)
	_, err = Load(t.TempDir(), false)
	require.ErrorContains(t, err, "read config")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, `{"broken":`, string(data))
}
