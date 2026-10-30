package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/raulaguila/opcode/internal/httpclient"
)

var ErrCatalogUnsupported = errors.New("this provider uses the configured model catalog")

// CatalogEndpoint is shared by discovery and inference, so a custom endpoint
// never lists models from a different account/server than generation uses.
func CatalogEndpoint(provider ModelProvider) string {
	switch provider {
	case ProviderOpenAI:
		return "https://api.openai.com/v1"
	case ProviderAnthropic:
		return "https://api.anthropic.com/v1"
	case ProviderGemini:
		return "https://generativelanguage.googleapis.com/v1beta"
	case ProviderGROQ:
		return "https://api.groq.com/openai/v1"
	case ProviderOpenRouter:
		return "https://openrouter.ai/api/v1"
	case ProviderXAI:
		return "https://api.x.ai/v1"
	}
	return ""
}

func validCatalogID(id string) bool {
	return id != "" && len(id) <= 512 && !strings.ContainsFunc(id, unicode.IsControl)
}

// DiscoverProviderModels only reads metadata. It never generates completions,
// downloads a model, changes the registry, or follows redirects with credentials.
func DiscoverProviderModels(ctx context.Context, provider ModelProvider, endpoint, apiKey string) ([]Model, error) {
	if provider == ProviderLocal {
		return DiscoverLocalModels(ctx, endpoint, apiKey)
	}
	if CatalogEndpoint(provider) == "" {
		return nil, ErrCatalogUnsupported
	}
	u, err := providerCatalogURL(provider, endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if provider == ProviderAnthropic {
		q.Set("limit", "1000")
	}
	if provider == ProviderGemini {
		q.Set("pageSize", "1000")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client := &http.Client{Transport: httpclient.Transport(ctx, http.DefaultTransport), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	d := providerCatalogDiscovery{ctx: ctx, client: client, provider: provider, apiKey: apiKey, u: u, q: q, found: []Model{}, seen: map[string]bool{}}
	for range 10 {
		result, err := d.readPage()
		if err != nil {
			return nil, err
		}
		for _, raw := range result.Data {
			d.appendItem(raw)
		}
		if !d.nextPage(result) {
			return d.found, nil
		}
	}
	return nil, fmt.Errorf("model catalog pagination exceeds limit")
}

type providerCatalogDiscovery struct {
	ctx      context.Context
	client   *http.Client
	provider ModelProvider
	apiKey   string
	u        *url.URL
	q        url.Values
	found    []Model
	seen     map[string]bool
}

func (d *providerCatalogDiscovery) add(id, name string, contextWindow, maxTokens int64) {

	if !validCatalogID(id) || d.seen[id] || len(d.found) >= 5000 {
		return
	}
	d.seen[id] = true
	if name == "" || strings.ContainsFunc(name, unicode.IsControl) {
		name = id
	}
	estimated := contextWindow <= 0
	source := "provider"
	if contextWindow <= 0 {
		contextWindow = 4096
		source = "fallback"
	}
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	maxTokens = min(maxTokens, contextWindow/2)
	d.found = append(d.found, Model{ID: ModelID(string(d.provider) + "." + id), Name: name, Provider: d.provider, APIModel: id, ContextWindow: contextWindow, ContextAuto: true, ContextSource: source, ContextEstimated: estimated, DefaultMaxTokens: maxTokens})

}
func (d *providerCatalogDiscovery) appendItem(raw json.RawMessage) {

	var item struct {
		ID             string   `json:"id"`
		Name           string   `json:"name"`
		DisplayName    string   `json:"display_name"`
		ContextWindow  int64    `json:"context_length"`
		MaxInputTokens int64    `json:"max_input_tokens"`
		MaxTokens      int64    `json:"max_tokens"`
		Methods        []string `json:"supportedGenerationMethods"`
		Architecture   struct {
			Output []string `json:"output_modalities"`
		} `json:"architecture"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return
	}
	if d.provider == ProviderGemini {
		if !slices.Contains(item.Methods, "generateContent") {
			return
		}
		item.ID = strings.TrimPrefix(item.ID, "models/")
	}
	// These endpoints also expose embeddings, audio and image-only models.
	lower := strings.ToLower(item.ID)
	if d.provider == ProviderOpenAI && (strings.Contains(lower, "embedding") || strings.Contains(lower, "whisper") || strings.Contains(lower, "tts") || strings.Contains(lower, "dall-e") || strings.Contains(lower, "moderation") || strings.Contains(lower, "realtime") || strings.Contains(lower, "transcribe") || strings.Contains(lower, "image") || strings.Contains(lower, "audio") || strings.HasPrefix(lower, "sora")) {
		return
	}
	if len(item.Architecture.Output) > 0 && !slices.Contains(item.Architecture.Output, "text") {
		return
	}
	contextWindow := item.ContextWindow
	if item.MaxInputTokens > 0 {
		contextWindow = item.MaxInputTokens
	}
	name := item.DisplayName
	if name == "" {
		name = item.Name
	}
	d.add(item.ID, name, contextWindow, item.MaxTokens)

}
func (d *providerCatalogDiscovery) readPage() (catalogPage, error) {
	d.u.RawQuery = d.q.Encode()
	req, err := http.NewRequestWithContext(d.ctx, http.MethodGet, d.u.String(), nil)
	if err != nil {
		return catalogPage{}, fmt.Errorf("cannot create model catalog request")
	}
	switch d.provider {
	case ProviderAnthropic:
		req.Header.Set("x-api-key", d.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case ProviderGemini:
		req.Header.Set("x-goog-api-key", d.apiKey)
	default:
		if d.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+d.apiKey)
		}
	}
	resp, err := d.client.Do(req)
	if err != nil {
		if d.ctx.Err() != nil {
			return catalogPage{}, d.ctx.Err()
		}
		return catalogPage{}, fmt.Errorf("cannot reach model catalog")
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return catalogPage{}, fmt.Errorf("model catalog returned HTTP %d", resp.StatusCode)
	}
	if readErr != nil || len(body) > 4<<20 {
		return catalogPage{}, fmt.Errorf("model catalog response exceeds limit or could not be read")
	}
	result, err := decodeCatalogPage(body)
	if err != nil {
		return catalogPage{}, fmt.Errorf("invalid model catalog response")
	}

	return result, nil

}
func (d *providerCatalogDiscovery) nextPage(page catalogPage) bool {
	if page.Next != "" {
		d.q.Set("pageToken", page.Next)
		return true
	}
	if page.HasMore && page.LastID != "" {
		d.q.Set("after_id", page.LastID)
		return true
	}
	return false
}
func providerCatalogURL(provider ModelProvider, endpoint string) (*url.URL, error) {
	if endpoint == "" {
		endpoint = CatalogEndpoint(provider)
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid provider baseURL (expected an HTTP API base URL without credentials or query)")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
	return u, nil
}
