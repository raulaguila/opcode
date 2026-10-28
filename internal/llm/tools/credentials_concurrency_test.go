package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestCredentialRedactionWhileSavingProvidersAndMCP(t *testing.T) {
	cfg := config.Get()
	previous := *cfg
	previousPath := viper.ConfigFileUsed()
	t.Cleanup(func() { *cfg = previous; viper.SetConfigFile(previousPath) })
	root := t.TempDir()
	file := filepath.Join(root, "settings.json")
	require.NoError(t, os.WriteFile(file, []byte(`{}`), 0600))
	viper.SetConfigFile(file)
	cfg.WorkingDir = root
	cfg.ProviderProfiles = nil
	cfg.Providers = nil
	cfg.MCPServers = nil
	// Names deliberately omit TOKEN/KEY/SECRET: only the configured references
	// identify these fake values as credentials.
	const profileSecret = "profile-private-fixture-value"
	const mcpSecret = "mcp-private-fixture-value"
	const legacySecret = "legacy-private-fixture-value"
	t.Setenv("OPCODE_TEST_SOURCE_A", profileSecret)
	t.Setenv("OPCODE_TEST_SOURCE_B", mcpSecret)
	t.Setenv("OPCODE_TEST_SOURCE_C", legacySecret)
	profile := config.ProviderProfile{Backend: models.ProviderLocal, Provider: config.Provider{BaseURL: "http://local.invalid/v1", API: "ollama", APIKeyEnv: "OPCODE_TEST_SOURCE_A"}}
	server := config.MCPServer{Type: config.MCPHTTP, URL: "https://mcp.invalid/service", HeadersEnv: map[string]string{"Authorization": "OPCODE_TEST_SOURCE_B"}}
	legacy := config.Provider{BaseURL: "http://local.invalid/v1", APIKeyEnv: "OPCODE_TEST_SOURCE_C"}
	require.NoError(t, config.SaveProviderProfile("initial", profile))
	require.NoError(t, config.SaveIntegration(config.Integration{Kind: "mcp", Name: "initial", MCP: server}))
	require.NoError(t, config.SaveProvider(models.ProviderLocal, legacy))
	stopped := make(chan struct{})
	finished := make(chan int, 1)
	failures := make(chan string, 1)
	go func() {
		count := 0
		defer func() { finished <- count }()
		payload := profileSecret + " " + mcpSecret + " " + legacySecret
		for {
			select {
			case <-stopped:
				return
			default:
			}
			redacted := RedactCredentials(payload)
			count++
			if strings.Contains(redacted, profileSecret) || strings.Contains(redacted, mcpSecret) || strings.Contains(redacted, legacySecret) {
				select {
				case failures <- "configured credential survived redaction":
				default:
				}
				return
			}
		}
	}()
	defer func() {
		close(stopped)
		require.Positive(t, <-finished)
		select {
		case failure := <-failures:
			t.Error(failure)
		default:
		}
	}()
	for i := 0; i < 40; i++ {
		profile.Name = fmt.Sprintf("Connection %d", i)
		require.NoError(t, config.SaveProviderProfile(fmt.Sprintf("profile-%d", i), profile))
		require.NoError(t, config.SaveIntegration(config.Integration{Kind: "mcp", Name: fmt.Sprintf("mcp-%d", i), MCP: server}))
		legacy.ContextWindow = int64(8192 + i)
		require.NoError(t, config.SaveProvider(models.ProviderLocal, legacy))
	}
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	for _, secret := range []string{profileSecret, mcpSecret, legacySecret} {
		require.NotContains(t, string(data), secret)
	}
}
