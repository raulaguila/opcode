package agent

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/message"
)

// auditCache keeps the model's replies by what was asked. An audit repeated
// after a small change asks only about what changed; anything a complete run
// did not ask again leaves the cache.
type auditCache struct {
	path    string
	replies map[string]string
	used    map[string]string
	fresh   int // replies paid for since the last save
}

// A crash loses at most this many paid replies.
const auditCacheInterval = 50

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

// save prunes only after a complete run: one cut short says nothing about what
// it never reached.
func (c *auditCache) save(complete bool) error {
	kept := c.used
	if !complete {
		kept = maps.Clone(c.replies)
		maps.Copy(kept, c.used)
	}
	c.fresh = 0
	data, err := json.Marshal(kept)
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
		if r.cache.fresh++; r.cache.fresh >= auditCacheInterval {
			if err := r.cache.save(false); err != nil {
				logging.Warn("audit cache not saved", "error", err)
			}
		}
	}
	return response.Content, nil
}
