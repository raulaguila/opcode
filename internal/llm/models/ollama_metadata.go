package models

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// enrichOllamaContexts reads metadata only. /api/show does not load or generate.
// The caller's shared timeout bounds the whole catalog, including partial failures.
type preferredOllamaModelKey struct{}

// WithPreferredOllamaModel prioritizes the selected model before optional
// catalog enrichment. It never reads configuration or mutates the registry.
func WithPreferredOllamaModel(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, preferredOllamaModelKey{}, id)
}

func enrichOllamaContexts(ctx context.Context, client *http.Client, endpoint, key string, found []localModel, running []OllamaRuntime) {
	count := min(len(found), 256)
	for i := range count {
		applyOllamaAllocation(&found[i], running)
	}
	order := ollamaMetadataOrder(ctx, found[:count], running)
	if len(order) == 0 || ctx.Err() != nil {
		return
	}
	// Finish the selected/loaded model first, so a large optional catalog cannot
	// consume its shared deadline. The remainder has at most four requests in flight.
	enrichOllamaModel(ctx, client, endpoint, key, &found[order[0]], running)
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(4, len(order)-1) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				enrichOllamaModel(ctx, client, endpoint, key, &found[index], running)
			}
		}()
	}
	for _, index := range order[1:] {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		}
	}
	close(jobs)
	workers.Wait()
}

func ollamaMetadataOrder(ctx context.Context, found []localModel, running []OllamaRuntime) []int {
	preferred, _ := ctx.Value(preferredOllamaModelKey{}).(string)
	order := make([]int, 0, len(found))
	seen := make([]bool, len(found))
	for i, model := range found {
		if preferred != "" && ollamaSameModel(model.ID, preferred) {
			order = append(order, i)
			seen[i] = true
		}
	}
	for _, active := range running {
		for i, model := range found {
			if !seen[i] && ollamaSameModel(model.ID, active.Name) {
				order = append(order, i)
				seen[i] = true
			}
		}
	}
	for i := range found {
		if !seen[i] {
			order = append(order, i)
		}
	}
	return order
}

func ollamaSameModel(a, b string) bool { return a == b || a+":latest" == b || b+":latest" == a }

func enrichOllamaModel(ctx context.Context, client *http.Client, endpoint, key string, model *localModel, running []OllamaRuntime) {
	if ctx.Err() != nil {
		return
	}
	identity, cacheable := ollamaMetadataIdentity(endpoint, key, *model, running)
	if cacheable && cachedOllamaMetadata(identity, model) {
		return
	}
	details, ok := readOllamaMetadata(ctx, client, endpoint, key, model.ID)
	if !ok || ctx.Err() != nil {
		return
	}
	applyOllamaMetadata(model, details)
	if cacheable {
		cacheOllamaMetadata(identity, *model)
	}
}

type ollamaMetadataCacheKey struct {
	endpoint, model, digest string
	credential              [32]byte
}
type ollamaStaticMetadata struct {
	context                    int64
	source                     string
	toolsKnown, toolsSupported bool
	observed                   time.Time
}

var ollamaMetadataCache = struct {
	sync.Mutex
	entries map[ollamaMetadataCacheKey]ollamaStaticMetadata
}{entries: make(map[ollamaMetadataCacheKey]ollamaStaticMetadata)}

func ollamaMetadataIdentity(endpoint, key string, model localModel, running []OllamaRuntime) (ollamaMetadataCacheKey, bool) {
	digest := model.Digest
	for _, active := range running {
		if digest == "" && ollamaSameModel(model.ID, active.Name) {
			digest = active.Digest
		}
	}
	// HTTPS clients may use different trust/opt-in TLS policies. Until that
	// security identity is explicit, never bypass their handshake using a cache.
	identity := ollamaMetadataCacheKey{endpoint: endpoint, model: model.ID, digest: digest, credential: sha256.Sum256([]byte(key))}
	return identity, strings.HasPrefix(endpoint, "http://") && len(digest) > 0 && len(digest) <= 256
}

func cachedOllamaMetadata(identity ollamaMetadataCacheKey, model *localModel) bool {
	ollamaMetadataCache.Lock()
	cached, ok := ollamaMetadataCache.entries[identity]
	ollamaMetadataCache.Unlock()
	if !ok || time.Since(cached.observed) > 5*time.Minute {
		return false
	}
	model.MaxContextLength = cached.context
	if cached.source != "" {
		model.ContextSource = cached.source
	}
	if cached.toolsKnown {
		supported := cached.toolsSupported
		model.SupportsTools = &supported
	}
	return true
}

func cacheOllamaMetadata(identity ollamaMetadataCacheKey, model localModel) {
	// Store only static normalized scalars, never the up-to-1MiB response or
	// /api/ps allocation; loaded context and placement must stay current.
	cached := ollamaStaticMetadata{context: model.MaxContextLength, source: model.ContextSource, observed: time.Now()}
	if model.SupportsTools != nil {
		cached.toolsKnown = true
		cached.toolsSupported = *model.SupportsTools
	}
	if strings.HasSuffix(cached.source, ":loaded") {
		cached.source = ""
	}
	ollamaMetadataCache.Lock()
	defer ollamaMetadataCache.Unlock()
	if len(ollamaMetadataCache.entries) >= 256 {
		clear(ollamaMetadataCache.entries)
	}
	ollamaMetadataCache.entries[identity] = cached
}

type ollamaMetadata struct {
	Capabilities *[]string                  `json:"capabilities"`
	Parameters   string                     `json:"parameters"`
	Info         map[string]json.RawMessage `json:"model_info"`
}

func applyOllamaAllocation(model *localModel, running []OllamaRuntime) {
	for _, active := range running {
		if (active.Name == model.ID || active.Name == model.ID+":latest") && active.Context > 0 {
			model.LoadedContextLength = active.Context
			model.ContextSource = "ollama:loaded"
			model.State = "loaded"
			break
		}
	}
}
func readOllamaMetadata(ctx context.Context, client *http.Client, endpoint, key, id string) (ollamaMetadata, bool) {
	var details ollamaMetadata
	body, _ := json.Marshal(struct {
		Model string `json:"model"`
	}{id})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(endpoint, "/v1")+"/api/show", bytes.NewReader(body))
	if err != nil {
		return details, false
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := client.Do(req)
	if err != nil {
		return details, false
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || len(raw) > 1<<20 {
		return details, false
	}
	ok := json.Unmarshal(raw, &details) == nil
	return details, ok
}
func applyOllamaMetadata(model *localModel, details ollamaMetadata) {
	if details.Capabilities != nil {
		supported := false
		for _, capability := range *details.Capabilities {
			if capability == "tools" {
				supported = true
			}
		}
		model.SupportsTools = &supported
	}
	var architecture string
	_ = json.Unmarshal(details.Info["general.architecture"], &architecture)
	var maximum int64
	_ = json.Unmarshal(details.Info[architecture+".context_length"], &maximum)
	context, source := ollamaParameterContext(maximum, details.Parameters)
	if context > 0 {
		model.MaxContextLength, model.ContextSource = context, source
	}
}
func ollamaParameterContext(maximum int64, parameters string) (int64, string) {
	context, source := maximum, "ollama:model"
	for line := range strings.SplitSeq(parameters, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "num_ctx" {
			continue
		}
		configured, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || configured <= 0 {
			continue
		}
		context, source = configured, "ollama:parameter"
		if maximum > 0 {
			context = min(context, maximum)
		}
	}
	return context, source
}
