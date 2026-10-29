package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/zalando/go-keyring"
)

const providerVaultService = "opcode.providers"
const maxVaultSecretBytes = 2048

var vaultReferencePattern = regexp.MustCompile(`^keyring:provider-[A-Za-z0-9_-]{20,80}$`)

// CredentialVaultBackend is an injectable native-store seam. Merely loading this
// package never calls the native store; only an explicit credential operation does.
type CredentialVaultBackend interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}
type nativeCredentialVault struct{}

func (nativeCredentialVault) Get(s, a string) (string, error) { return keyring.Get(s, a) }
func (nativeCredentialVault) Set(s, a, p string) error        { return keyring.Set(s, a, p) }
func (nativeCredentialVault) Delete(s, a string) error        { return keyring.Delete(s, a) }

var providerVault CredentialVaultBackend = nativeCredentialVault{}

func validateVaultReference(p Provider) error {
	if p.APIKeyRef == "" {
		return nil
	}
	if !vaultReferencePattern.MatchString(p.APIKeyRef) {
		return fmt.Errorf("invalid apiKeyRef; use an Opcode-generated native vault reference")
	}
	if p.APIKeyEnv != "" {
		return fmt.Errorf("apiKeyRef and apiKeyEnv cannot be combined")
	}
	return nil
}
func validateVaultDocument(p Provider) error {
	if err := validateVaultReference(p); err != nil {
		return err
	}
	if p.APIKeyRef != "" && p.APIKey != "" {
		return fmt.Errorf("apiKeyRef and a literal apiKey cannot be combined")
	}
	return nil
}
func readVaultCredential(ref string) (string, error) { return readCredentialFrom(providerVault, ref) }
func readCredentialFrom(backend CredentialVaultBackend, ref string) (string, error) {
	if err := validateVaultReference(Provider{APIKeyRef: ref}); err != nil {
		return "", err
	}
	secret, err := backend.Get(providerVaultService, strings.TrimPrefix(ref, "keyring:"))
	if err != nil || secret == "" || len(secret) > maxVaultSecretBytes {
		// Native errors can contain output from OS helpers. Never forward them.
		return "", fmt.Errorf("native credential vault could not read the key; unlock the vault or explicitly choose another authentication method")
	}
	RegisterRuntimeCredentials("provider-vault:"+ref, secret)
	return secret, nil
}

// PreparedVaultCredential owns a newly created credential until metadata commits.
// Preparation can run outside the UI goroutine. The caller must wait for it:
// native unlock dialogs have no cancellation API. Commit runs on the config owner.
type PreparedVaultCredential struct {
	ref, secret string
	backend     CredentialVaultBackend
	created     bool
	finished    bool
}

// PrepareVaultCredential creates a fresh entry or reads an existing reference
// when secret is blank. It does not publish configuration or replace an old entry.
func PrepareVaultCredential(existingRef, secret string) (*PreparedVaultCredential, error) {
	return PrepareVaultCredentialWithBackend(providerVault, existingRef, secret)
}

// PrepareVaultCredentialWithBackend permits controlled in-process backends.
// Tests inject a fake here, never the real user vault.
func PrepareVaultCredentialWithBackend(backend CredentialVaultBackend, existingRef, secret string) (*PreparedVaultCredential, error) {
	if backend == nil {
		return nil, fmt.Errorf("credential vault backend is required")
	}
	pending := &PreparedVaultCredential{backend: backend}
	if secret == "" {
		if existingRef == "" {
			return nil, fmt.Errorf("enter an API key for the native vault")
		}
		key, err := readCredentialFrom(pending.backend, existingRef)
		if err != nil {
			return nil, err
		}
		pending.ref, pending.secret = existingRef, key
		return pending, nil
	}
	if len(secret) > maxVaultSecretBytes {
		return nil, fmt.Errorf("native vault API keys must be at most %d bytes; use explicit direct or environment authentication for longer keys", maxVaultSecretBytes)
	}
	pending.ref, pending.secret, pending.created = "keyring:provider-"+rand.Text(), secret, true
	RegisterRuntimeCredentials("provider-vault:"+pending.ref, secret)
	if err := pending.backend.Set(providerVaultService, strings.TrimPrefix(pending.ref, "keyring:"), secret); err != nil {
		// A failed backend may have partially stored an entry. Best-effort cleanup
		// concerns only this fresh random reference, never the previous credential.
		cleanup := pending.Rollback()
		return nil, errors.Join(fmt.Errorf("native credential vault could not store the key; previous configuration preserved"), cleanup)
	}
	return pending, nil
}

// Rollback never removes a reused credential. An OS cleanup failure is reported
// without its raw message or the secret. Retrying cannot delete an older entry.
func (p *PreparedVaultCredential) Rollback() error {
	if p == nil || p.finished {
		return nil
	}
	p.finished = true
	p.secret = ""
	if !p.created {
		return nil
	}
	if err := p.backend.Delete(providerVaultService, strings.TrimPrefix(p.ref, "keyring:")); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("native vault cleanup failed; an unused Opcode credential may remain in the OS vault")
	}
	p.created = false
	RegisterRuntimeCredentials("provider-vault:" + p.ref)
	return nil
}
func (p *PreparedVaultCredential) settings(raw Provider) Provider {
	raw.APIKey, raw.APIKeyEnv, raw.APIKeyRef = "", "", p.ref
	return raw
}

// CommitProviderProfile persists metadata on the configuration owner. It performs
// no native I/O. After an error, the caller still owns Rollback (possibly async).
func (p *PreparedVaultCredential) CommitProviderProfile(id string, profile ProviderProfile) error {
	if p == nil {
		return fmt.Errorf("credential not prepared")
	}
	if p.finished {
		return fmt.Errorf("prepared credential already completed")
	}
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	raw := profile
	raw.Provider = p.settings(raw.Provider)
	candidate := raw
	candidate.APIKeyRef, candidate.APIKey = "", p.secret
	resolved, err := PreviewProviderProfile(id, candidate)
	if err == nil {
		resolved.APIKeyRef = p.ref
		err = persistProviderProfile(id, raw, resolved)
	}
	if err != nil {
		return err
	}
	p.created, p.finished, p.secret = false, true, ""
	return nil
}

// CommitProvider has the same ownership contract for a legacy connection.
func (p *PreparedVaultCredential) CommitProvider(backend models.ModelProvider, raw Provider) error {
	if p == nil {
		return fmt.Errorf("credential not prepared")
	}
	if p.finished {
		return fmt.Errorf("prepared credential already completed")
	}
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	raw = p.settings(raw)
	candidate := raw
	candidate.APIKeyRef, candidate.APIKey = "", p.secret
	resolved, err := resolveProvider(backend, candidate)
	if err == nil {
		resolved.APIKeyRef = p.ref
		err = persistProvider(backend, raw, resolved)
	}
	if err != nil {
		return err
	}
	p.created, p.finished, p.secret = false, true, ""
	return nil
}

// Save helpers are synchronous for non-UI callers; the TUI separates metadata
// commit from native rollback so an OS unlock/cleanup never blocks its event loop.
func (p *PreparedVaultCredential) SaveProviderProfile(id string, profile ProviderProfile) error {
	err := p.CommitProviderProfile(id, profile)
	if err != nil && p != nil {
		return errors.Join(err, p.Rollback())
	}
	return err
}
func (p *PreparedVaultCredential) SaveProvider(backend models.ModelProvider, raw Provider) error {
	err := p.CommitProvider(backend, raw)
	if err != nil && p != nil {
		return errors.Join(err, p.Rollback())
	}
	return err
}
func SaveProviderProfileInVault(id string, profile ProviderProfile, secret string) error {
	p, err := PrepareVaultCredential(profile.APIKeyRef, secret)
	if err != nil {
		return err
	}
	return p.SaveProviderProfile(id, profile)
}
func SaveProviderInVault(backend models.ModelProvider, raw Provider, secret string) error {
	p, err := PrepareVaultCredential(raw.APIKeyRef, secret)
	if err != nil {
		return err
	}
	return p.SaveProvider(backend, raw)
}
