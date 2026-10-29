package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

type fakeCredentialVault struct {
	entries                   map[string]string
	gets, sets, deletes       int
	getErr, setErr, deleteErr error
}

func (f *fakeCredentialVault) Get(s, a string) (string, error) {
	f.gets++
	if s != providerVaultService {
		panic("wrong vault namespace")
	}
	return f.entries[a], f.getErr
}
func (f *fakeCredentialVault) Set(s, a, k string) error {
	f.sets++
	if s != providerVaultService {
		panic("wrong vault namespace")
	}
	f.entries[a] = k
	return f.setErr
}
func (f *fakeCredentialVault) Delete(s, a string) error {
	f.deletes++
	if s != providerVaultService {
		panic("wrong vault namespace")
	}
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.entries, a)
	return nil
}
func vaultConfig(t *testing.T) (*fakeCredentialVault, string) {
	t.Helper()
	isolateModelConfig(t)
	home, root := t.TempDir(), t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(name, home)
	}
	t.Setenv("LOCAL_ENDPOINT", "")
	path := filepath.Join(root, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"providerProfiles":{"office":{"backend":"openai","apiKey":"fake-old-literal-key","baseURL":"https://example.invalid/v1"}},"future":{"keep":9007199254740993}}`), 0600))
	viper.SetConfigFile(path)
	cfg = &Config{WorkingDir: root, Providers: map[models.ModelProvider]Provider{}, ProviderProfiles: map[string]ProviderProfile{"office": {Backend: models.ProviderOpenAI, Provider: Provider{APIKey: "fake-old-literal-key", BaseURL: "https://example.invalid/v1"}}}}
	fake := &fakeCredentialVault{entries: map[string]string{}}
	old := providerVault
	providerVault = fake
	t.Cleanup(func() { providerVault = old })
	return fake, path
}
func TestNativeVaultSaveUsesReferenceAndAuthenticatedReadOnly(t *testing.T) {
	fake, path := vaultConfig(t)
	profile := cfg.ProviderProfiles["office"]
	secret := "fake-vault-provider-secret"
	require.NoError(t, SaveProviderProfileInVault("office", profile, secret))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), secret)
	require.NotContains(t, string(raw), "fake-old-literal-key")
	require.Contains(t, string(raw), "9007199254740993")
	var stored Config
	require.NoError(t, json.Unmarshal(raw, &stored))
	ref := stored.ProviderProfiles["office"].APIKeyRef
	require.True(t, vaultReferencePattern.MatchString(ref))
	require.Empty(t, stored.ProviderProfiles["office"].APIKey)
	require.Equal(t, secret, fake.entries[strings.TrimPrefix(ref, "keyring:")])
	require.Empty(t, StoredProviderKey("office", false))
	require.Contains(t, CredentialValuesSnapshot(), secret)
	require.NoError(t, configureProviders())
	require.Zero(t, fake.gets)
	_, metadata, err := ProviderForProfile("office")
	require.NoError(t, err)
	require.Empty(t, metadata.APIKey)
	require.Zero(t, fake.gets)
	root := cfg.WorkingDir
	cfg = nil
	viper.Reset()
	viper.SetConfigFile(path)
	_, err = LoadOffline(root)
	require.NoError(t, err)
	require.Zero(t, fake.gets, "offline inspection must not unlock the vault")
	_, err = json.Marshal(RedactedDocument())
	require.NoError(t, err)
	require.Zero(t, fake.gets)
	resolved, err := ProviderForModel(models.Model{Provider: models.ProviderOpenAI, ProviderProfile: "office"})
	require.NoError(t, err)
	require.Equal(t, secret, resolved.APIKey)
	require.Equal(t, 1, fake.gets)
	redacted, _ := json.Marshal(RedactedDocument())
	require.NotContains(t, string(redacted), secret)
	require.NoError(t, ValidateDocument(path))
}
func TestNativeVaultFailuresPreservePreviousMetadataAndRollbackFreshEntry(t *testing.T) {
	for _, failure := range []string{"set", "persist", "config missing", "validation"} {
		t.Run(failure, func(t *testing.T) {
			fake, path := vaultConfig(t)
			original, err := os.ReadFile(path)
			require.NoError(t, err)
			profile := cfg.ProviderProfiles["office"]
			const secret = "fake-failure-secret-value"
			if failure == "set" {
				fake.setErr = errors.New(secret)
			}
			pending, err := PrepareVaultCredential("", secret)
			if failure == "set" {
				require.Error(t, err)
				require.NotContains(t, err.Error(), secret)
			} else {
				require.NoError(t, err)
				switch failure {
				case "persist":
					require.NoError(t, os.MkdirAll(filepath.Dir(ProjectConfigPath(cfg.WorkingDir)), 0700))
					require.NoError(t, os.WriteFile(ProjectConfigPath(cfg.WorkingDir), []byte(`{invalid`), 0600))
				case "config missing":
					cfg = nil
				case "validation":
					profile.Backend = "unsupported"
				}
				err = pending.SaveProviderProfile("office", profile)
				require.Error(t, err)
				require.NotContains(t, err.Error(), secret)
				require.NoError(t, pending.Rollback(), "cleanup is idempotent")
				require.Error(t, pending.SaveProviderProfile("office", profile), "a failed prepared ref cannot be reused")
			}
			current, e := os.ReadFile(path)
			require.NoError(t, e)
			require.Equal(t, original, current)
			require.Empty(t, fake.entries)
			require.Equal(t, 1, fake.deletes)
			if cfg != nil {
				require.Equal(t, "fake-old-literal-key", cfg.ProviderProfiles["office"].APIKey)
			}
		})
	}
}
func TestNativeVaultRotationReuseAndCleanupFailure(t *testing.T) {
	fake, path := vaultConfig(t)
	profile := cfg.ProviderProfiles["office"]
	require.NoError(t, SaveProviderProfileInVault("office", profile, "fake-first-vault-key"))
	old := cfg.ProviderProfiles["office"].APIKeyRef
	reused, err := PrepareVaultCredential(old, "")
	require.NoError(t, err)
	require.NoError(t, reused.Rollback())
	require.Zero(t, fake.deletes)
	require.NoError(t, SaveProviderProfileInVault("office", cfg.ProviderProfiles["office"], "fake-second-vault-key"))
	require.Len(t, fake.entries, 2, "old references may still be used by another layer; do not delete blindly")
	pending, err := PrepareVaultCredential("", "fake-third-vault-key")
	require.NoError(t, err)
	fake.deleteErr = errors.New("sensitive native backend output")
	err = pending.Rollback()
	require.ErrorContains(t, err, "cleanup failed")
	require.NotContains(t, err.Error(), "sensitive")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "fake-third-vault-key")
}
func TestNativeVaultReferencesAreExplicitBoundedAndNeverFallback(t *testing.T) {
	fake, path := vaultConfig(t)
	_, err := PrepareVaultCredential("", strings.Repeat("x", maxVaultSecretBytes+1))
	require.Error(t, err)
	require.Zero(t, fake.sets)
	for _, p := range []Provider{{APIKeyRef: "other-service:credential"}, {APIKeyRef: "keyring:provider-abcdefghijklmnopqrst", APIKeyEnv: "MY_KEY"}} {
		_, err := resolveProvider(models.ProviderOpenAI, p)
		require.Error(t, err)
	}
	require.Zero(t, fake.gets)
	_, err = resolveProvider(models.ProviderOpenAI, Provider{APIKeyRef: "keyring:provider-abcdefghijklmnopqrst", APIKey: "fake-do-not-fallback"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "fake-do-not-fallback")
	require.NoError(t, os.WriteFile(path, []byte(`{"providers":{"openai":{"apiKeyRef":"keyring:provider-abcdefghijklmnopqrst","apiKey":"fake-conflicting-secret"}}}`), 0600))
	err = ValidateDocument(path)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "fake-conflicting-secret")
}
func TestNativeVaultLegacySaveAndSuccessfulCommitCannotRollback(t *testing.T) {
	fake, path := vaultConfig(t)
	prepared, err := PrepareVaultCredential("", "fake-legacy-vault-key")
	require.NoError(t, err)
	require.NoError(t, prepared.SaveProvider(models.ProviderOpenAI, Provider{BaseURL: "https://example.invalid/v1"}))
	require.NoError(t, prepared.Rollback())
	require.Zero(t, fake.deletes)
	require.Error(t, prepared.SaveProvider(models.ProviderOpenAI, Provider{}))
	require.Len(t, fake.entries, 1)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "fake-legacy-vault-key")
	require.NoError(t, SaveProvider(models.ProviderOpenAI, Provider{APIKey: "fake-explicit-direct-key"}))
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	var stored Config
	require.NoError(t, json.Unmarshal(raw, &stored))
	require.Empty(t, stored.Providers[models.ProviderOpenAI].APIKeyRef)
	require.Equal(t, "fake-explicit-direct-key", stored.Providers[models.ProviderOpenAI].APIKey)
}

func TestNativeVaultOfflineSelectedModelDoesNotUnlock(t *testing.T) {
	fake, path := vaultConfig(t)
	root := cfg.WorkingDir
	raw := `{"providerProfiles":{"office":{"backend":"openai","baseURL":"https://example.invalid/v1","apiKeyRef":"keyring:provider-abcdefghijklmnopqrst"}},"models":{"profile.office.test":{"provider":"openai","provider_profile":"office","api_model":"test","context_window":32768}},"agents":{"coder":{"model":"profile.office.test"}}}`
	require.NoError(t, os.WriteFile(path, []byte(raw), 0600))
	cfg = nil
	viper.Reset()
	viper.SetConfigFile(path)
	loaded, err := LoadOffline(root)
	require.NoError(t, err)
	require.NoError(t, loaded.ValidateStartup())
	require.Zero(t, fake.gets)
	model := models.SupportedModels["profile.office.test"]
	_ = ModelWithProviderDefaults(model)
	_ = ConfiguredProfileModels("office")
	require.Zero(t, fake.gets, "metadata and offline validation must not unlock even the selected model")
	_, err = ProviderForModel(model)
	require.Error(t, err)
	require.Equal(t, 1, fake.gets)
}
