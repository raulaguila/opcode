package config

import (
	"strings"
	"sync"
)

// credentialMu protects publishing the credential-bearing maps against readers
// such as a cancelled connection probe that is still redacting its final error.
// Network and disk work never holds this lock.
var credentialMu sync.RWMutex

var runtimeCredentials = map[string][]string{}

// RegisterRuntimeCredentials makes OAuth and server credentials participate in
// the same redaction boundary as configured provider keys. They are never saved
// as configuration. Owners replace or remove their bounded entries on shutdown.
func RegisterRuntimeCredentials(owner string, values ...string) {
	credentialMu.Lock()
	defer credentialMu.Unlock()
	if len(values) == 0 {
		delete(runtimeCredentials, owner)
		return
	}
	if len(runtimeCredentials) >= 128 && runtimeCredentials[owner] == nil {
		return
	}
	secrets := []string{}
	for _, value := range values {
		if len(value) >= 8 && len(value) <= 65536 && len(secrets) < 8 {
			secrets = append(secrets, value)
		}
	}
	runtimeCredentials[owner] = secrets
}

// CredentialValuesSnapshot returns owned strings, never maps or configuration
// pointers. Resolution is in memory and preserves the environment-prefix aliases.
func CredentialValuesSnapshot() []string {
	credentialMu.RLock()
	defer credentialMu.RUnlock()
	result := []string{}
	seen := map[string]bool{}
	add := func(secret string) {
		if len(secret) >= 8 && !seen[secret] {
			seen[secret] = true
			result = append(result, secret)
		}
	}
	for _, values := range runtimeCredentials {
		for _, secret := range values {
			add(secret)
		}
	}
	if cfg == nil {
		return result
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
		if server.OAuth != nil {
			add(Environment(server.OAuth.ClientSecretEnv))
		}
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
