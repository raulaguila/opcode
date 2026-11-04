package agent

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/message"
)

// auditCache keeps the model's replies by what was asked. An audit repeated
// after a small change asks only about what changed; anything not asked again
// leaves the cache when it is saved.
type auditCache struct {
	path    string
	replies map[string]string
	used    map[string]string
}

func auditDirectory() string {
	cfg := config.Get()
	if cfg == nil {
		return filepath.Join(config.WorkingDirectory(), ".opcode", "audit")
	}
	return filepath.Join(cmp.Or(cfg.Data.Directory, filepath.Join(config.WorkingDirectory(), ".opcode")), "audit")
}

func loadAuditCache() *auditCache {
	cache := &auditCache{path: filepath.Join(auditDirectory(), "cache.json"), replies: map[string]string{}, used: map[string]string{}}
	if data, err := os.ReadFile(cache.path); err == nil {
		_ = json.Unmarshal(data, &cache.replies)
	}
	return cache
}

func (c *auditCache) save() error {
	data, err := json.Marshal(c.used)
	if err != nil {
		return err
	}
	return writeAuditFile(c.path, data)
}

// writeAuditFile keeps audit files out of the project's changes.
func writeAuditFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), ".gitignore"), []byte("*\n"), 0o600); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ask sends one question or answers it from the cache. A reply goes in only if
// valid says so: what the model failed to answer must be asked again.
func (r *auditRun) ask(ctx context.Context, connection provider.Provider, instruction, text string, valid func(string) bool) (string, error) {
	sum := sha256.Sum256([]byte(string(connection.Model().ID) + "\x00" + instruction + "\x00" + text))
	key := hex.EncodeToString(sum[:])
	if reply, ok := r.cache.replies[key]; ok {
		r.cache.used[key] = reply
		r.report.cached++
		return reply, nil
	}
	request := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}}}
	response, err := sendAuxiliaryRequest(ctx, config.AgentCoder, connection, request, nil)
	if err != nil {
		return "", err
	}
	_ = r.agent.TrackUsage(ctx, r.session, connection.Model(), response.Usage)
	if valid(response.Content) {
		r.cache.used[key] = response.Content
	}
	return response.Content, nil
}
