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

func TestStartupAdoptsLegacyGlobalInCanonicalPrivateFile(t *testing.T) {
	isolateMigrationConfig(t)
	old := filepath.Join(os.Getenv("HOME"), ".opencode.json")
	body := `{"future":9007199254740993,"providers":{"openai":{"apiKey":"private-fixture"}},"runtime":{"mode":"ask"}}`
	writeMigrationJSON(t, old, body)
	_, err := Load(t.TempDir(), false)
	require.NoError(t, err)
	path := filepath.Join(os.Getenv("HOME"), ".opcode", "config.json")
	require.Equal(t, path, viper.ConfigFileUsed())
	for _, file := range []string{old, path} {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		require.Equal(t, body, string(data))
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestExplicitAndOfflineLoadsDoNotAdoptLegacyGlobal(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit=%t", explicit), func(t *testing.T) {
			isolateMigrationConfig(t)
			root := t.TempDir()
			writeMigrationJSON(t, filepath.Join(os.Getenv("HOME"), ".opencode.json"), `{}`)
			if explicit {
				path := filepath.Join(root, "explicit.json")
				writeMigrationJSON(t, path, `{}`)
				viper.SetConfigFile(path)
				_, err := Load(root, false)
				require.NoError(t, err)
			} else {
				_, err := LoadOffline(root)
				require.NoError(t, err)
			}
			_, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".opcode", "config.json"))
			require.True(t, os.IsNotExist(err))
		})
	}
}

func TestLegacyAdoptionRejectsSymlinkSource(t *testing.T) {
	isolateMigrationConfig(t)
	target := filepath.Join(t.TempDir(), "outside.json")
	writeMigrationJSON(t, target, `{}`)
	require.NoError(t, os.Symlink(target, filepath.Join(os.Getenv("HOME"), ".opencode.json")))
	require.Error(t, ensureGlobalTemplate())
	_, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".opcode", "config.json"))
	require.True(t, os.IsNotExist(err))
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
	adopted, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, adopted, "normal startup preserves legacy global settings at the canonical path")
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

func TestEveryStartupRestoresReferenceExampleWithoutCopyingUserSettings(t *testing.T) {
	isolateMigrationConfig(t)
	home, project := os.Getenv("HOME"), t.TempDir()
	active := filepath.Join(home, ".opcode", "config.json")
	example := filepath.Join(home, "opcode", "config.example.json")
	original := `{"future":"private-reference-fixture","runtime":{"mode":"plan"}}`
	writeMigrationJSON(t, active, original)
	_, err := LoadOffline(project)
	require.NoError(t, err)
	_, err = os.Stat(example)
	require.True(t, os.IsNotExist(err), "offline commands do not create examples")
	for i := 0; i < 2; i++ {
		cfg = nil
		viper.Reset()
		_, err := Load(project, false)
		require.NoError(t, err)
		raw, err := os.ReadFile(example)
		require.NoError(t, err)
		require.Equal(t, defaultConfigTemplate, raw)
		require.NotContains(t, string(raw), "private-reference-fixture")
		info, err := os.Stat(example)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0600), info.Mode().Perm())
		raw, err = os.ReadFile(active)
		require.NoError(t, err)
		require.Equal(t, original, string(raw))
		require.NoError(t, os.Remove(example))
	}
	writeMigrationJSON(t, example, `{"my":"notes"}`)
	cfg = nil
	viper.Reset()
	_, err = Load(project, false)
	require.NoError(t, err)
	raw, err := os.ReadFile(example)
	require.NoError(t, err)
	require.Equal(t, `{"my":"notes"}`, string(raw), "an edited reference is never replaced")
}

func TestInvalidActiveConfigStillGetsReferenceExample(t *testing.T) {
	isolateMigrationConfig(t)
	home := os.Getenv("HOME")
	writeMigrationJSON(t, filepath.Join(home, ".opcode", "config.json"), `{"invalid":`)
	_, err := Load(t.TempDir(), false)
	require.Error(t, err)
	raw, err := os.ReadFile(filepath.Join(home, "opcode", "config.example.json"))
	require.NoError(t, err)
	require.Equal(t, defaultConfigTemplate, raw)
}

func TestReferenceTemplateRejectsSymlinks(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(fmt.Sprint(parent), func(t *testing.T) {
			isolateMigrationConfig(t)
			directory := filepath.Join(os.Getenv("HOME"), "opcode")
			outside := t.TempDir()
			if parent {
				require.NoError(t, os.Symlink(outside, directory))
			} else {
				require.NoError(t, os.Mkdir(directory, 0700))
				require.NoError(t, os.Symlink(filepath.Join(outside, "absent.json"), filepath.Join(directory, "config.example.json")))
			}
			require.Error(t, ensureReferenceTemplate())
			entries, err := os.ReadDir(outside)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}
