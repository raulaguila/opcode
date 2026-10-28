package config

import (
	"strings"
	"sync"
)

// credentialMu protects publishing the credential-bearing maps against readers
// such as a cancelled connection probe that is still redacting its final error.
// Network and disk work never holds this lock.
var credentialMu sync.RWMutex

// CredentialValuesSnapshot returns owned strings, never maps or configuration
// pointers. Resolution is in memory and preserves the environment-prefix aliases.
func CredentialValuesSnapshot() []string {
	credentialMu.RLock()
	defer credentialMu.RUnlock()
	if cfg == nil {
		return nil
	}
	result := []string{}
	seen := map[string]bool{}
	add := func(secret string) {
		if len(secret) >= 8 && !seen[secret] {
			seen[secret] = true
			result = append(result, secret)
		}
	}
	for _, provider := range cfg.Providers {
		add(provider.APIKey)
		add(Environment(provider.APIKeyEnv))
	}
	for _, profile := range cfg.ProviderProfiles {
		add(profile.APIKey)
		add(Environment(profile.APIKeyEnv))
	}
	for _, server := range cfg.MCPServers {
		for _, name := range server.HeadersEnv {
			add(Environment(name))
		}
		for _, name := range server.EnvVars {
			add(Environment(name))
		}
		for _, entry := range server.Env {
			_, secret, _ := strings.Cut(entry, "=")
			add(secret)
		}
		for _, secret := range server.Headers {
			add(secret)
		}
	}
	return result
}
