package dialog

import (
	"errors"
	"github.com/spf13/viper"
	"os"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/require"
)

func TestVaultProviderEditNeverCopiesResolvedSecretAndKeepsReference(t *testing.T) {
	m := settingsDialog(t)
	config.Get().ProviderProfiles = map[string]config.ProviderProfile{"office": {Backend: models.ProviderOpenAI, Provider: config.Provider{APIKeyRef: "keyring:provider-abcdefghijklmnopqrst", APIKey: "fake-resolved-vault-key"}}}
	m.edit(&providerRow{id: "office"})
	require.Equal(t, "vault", m.fields[providerAuthField].Value())
	require.Empty(t, m.fields[providerKeyField].Value())
	require.NotContains(t, m.View(), "fake-resolved-vault-key")
	_, profile, err := m.readProfile()
	require.NoError(t, err)
	require.Equal(t, "keyring:provider-abcdefghijklmnopqrst", profile.APIKeyRef)
	require.Empty(t, profile.APIKey)
	require.Empty(t, profile.APIKeyEnv)
	m.fields[providerAuthField].SetValue("api_key")
	_, _, err = m.readProfile()
	require.Error(t, err, "vault secret must not become a direct key")
}

func TestVaultProviderSaveAwaitsNativeResultWithoutCancellationOrDuplicateWrites(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	m.fields[providerAuthField].SetValue("vault")
	m.fields[providerKeyField].SetValue("fake-explicit-vault-input")
	calls := 0
	m.prepareVault = func(ref, key string) (*config.PreparedVaultCredential, error) {
		calls++
		require.Empty(t, ref)
		require.Equal(t, "fake-explicit-vault-input", key)
		return nil, errors.New("vault unavailable in fixture")
	}
	command := m.save()
	require.NotNil(t, command)
	require.True(t, m.saving)
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlS}, {Type: tea.KeyRunes, Runes: []rune("new input")}} {
		_, cmd := m.Update(key)
		require.Nil(t, cmd)
		require.True(t, m.saving)
		require.Equal(t, "provider", m.screen)
	}
	require.Nil(t, m.save())
	require.Contains(t, m.View(), "cannot cancel")
	require.NotContains(t, m.View(), "fake-explicit-vault-input")
	m.Update(command())
	require.Equal(t, 1, calls)
	require.False(t, m.saving)
	require.Contains(t, m.note, "vault unavailable")
	require.Equal(t, "provider", m.screen)
	require.Equal(t, "fake-explicit-vault-input", m.fields[providerKeyField].Value(), "failed save preserves masked draft")
}

type blockedCleanupVault struct {
	started chan struct{}
	release chan struct{}
}

func (f *blockedCleanupVault) Get(string, string) (string, error) {
	return "fake-vault-fixture-key", nil
}
func (f *blockedCleanupVault) Set(string, string, string) error { return nil }
func (f *blockedCleanupVault) Delete(string, string) error {
	close(f.started)
	<-f.release
	return errors.New("fake-private-native-error")
}

func TestVaultProviderCleanupStaysPendingOutsideTheUIUntilNativeResult(t *testing.T) {
	m := settingsDialog(t)
	m.edit(nil)
	m.fields[providerAuthField].SetValue("vault")
	m.fields[providerKeyField].SetValue("fake-explicit-vault-input")
	m.fields[3].SetValue("://invalid") // Validation fails only at metadata commit.
	backend := &blockedCleanupVault{started: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(backend.release) }) })
	m.prepareVault = func(ref, key string) (*config.PreparedVaultCredential, error) {
		return config.PrepareVaultCredentialWithBackend(backend, ref, key)
	}
	before, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	prepare := m.save()
	require.NotNil(t, prepare)
	_, cleanup := m.Update(prepare())
	require.NotNil(t, cleanup)
	require.True(t, m.PendingNativeOperation(), "failed metadata still owns native cleanup")
	select {
	case <-backend.started:
		t.Fatal("native Delete ran inside UI Update")
	default:
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cleanup() }()
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		t.Fatal("cleanup command did not start")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.Nil(t, cmd)
	require.True(t, m.PendingNativeOperation())
	require.Equal(t, "provider", m.screen)
	release.Do(func() { close(backend.release) })
	select {
	case msg := <-result:
		m.Update(msg)
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish")
	}
	require.False(t, m.PendingNativeOperation())
	require.Contains(t, m.note, "cleanup failed")
	require.NotContains(t, m.note, "fake-private-native-error")
	after, err := os.ReadFile(viper.ConfigFileUsed())
	require.NoError(t, err)
	require.Equal(t, before, after)
}
