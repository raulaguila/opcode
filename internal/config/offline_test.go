package config

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestOfflineConfigurationDoesNotDiscoverLocalModelsOrReplaceLiveSingleton(t *testing.T) {
	isolateModelConfig(t)
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	calls := 0
	http.DefaultTransport = localDiscoveryTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("offline load must not make requests")
	})
	dir := t.TempDir()
	path := filepath.Join(dir, ".opcode.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"providers":{"local":{"baseURL":"http://local.invalid"}}}`), 0600))
	viper.SetConfigFile(path)
	loaded, err := LoadOffline(dir)
	require.NoError(t, err)
	require.Zero(t, calls)
	require.Error(t, loaded.ValidateStartup())
	require.NoError(t, os.WriteFile(path, []byte(`invalid JSON`), 0600))
	again, err := LoadOffline(t.TempDir())
	require.NoError(t, err)
	require.Same(t, loaded, again)
	require.Zero(t, calls)
}
