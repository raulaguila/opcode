package provider

import (
	"crypto/sha256"
	"sync"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/tiktoken-go/tokenizer"
)

// One connection/codec owns its counter; request-scoped clones share it. Only
// two exact byte-digest/count pairs survive a call, never prompt or key text.
type requestTokenCounter struct {
	provider models.ModelProvider
	model    string
	once     sync.Once
	codec    tokenizer.Codec
	name     string
	mu       sync.Mutex
	entries  [2]tokenCountEntry
	next     int
}

type tokenCountEntry struct {
	digest [32]byte
	bytes  int
	tokens int
	valid  bool
}

func newRequestTokenCounter(model models.Model) *requestTokenCounter {
	return &requestTokenCounter{provider: model.Provider, model: model.APIModel}
}

func (c *requestTokenCounter) matches(model models.Model) bool {
	return c != nil && c.provider == model.Provider && c.model == model.APIModel
}

func (c *requestTokenCounter) initialize() {
	c.once.Do(func() {
		if c.provider != models.ProviderOpenAI {
			return
		}
		c.codec, _ = tokenizer.ForModel(tokenizer.Model(c.model))
		if c.codec != nil {
			c.name = c.codec.GetName()
		}
	})
}

func (c *requestTokenCounter) GetName() string { c.initialize(); return c.name }

func (c *requestTokenCounter) count(raw []byte) (int, error) {
	c.initialize()
	if c.codec == nil {
		return 0, tokenizer.ErrModelNotSupported
	}
	digest := sha256.Sum256(raw)
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, entry := range c.entries {
		if entry.valid && entry.bytes == len(raw) && entry.digest == digest {
			c.next = (i + 1) % len(c.entries)
			return entry.tokens, nil
		}
	}
	count, err := c.codec.Count(string(raw))
	if err != nil {
		return 0, err
	}
	c.entries[c.next] = tokenCountEntry{digest: digest, bytes: len(raw), tokens: count, valid: true}
	c.next = (c.next + 1) % len(c.entries)
	return count, nil
}
