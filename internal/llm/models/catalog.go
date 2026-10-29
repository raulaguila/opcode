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
	if endpoint == "" {
		endpoint = CatalogEndpoint(provider)
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid provider baseURL (expected an HTTP API base URL without credentials or query)")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
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
	found := []Model{}
	seen := map[string]bool{}
	// Bound pagination as well as the size of individual pages and the total list.
	for page := 0; page < 10; page++ {
		u.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("cannot create model catalog request")
		}
		switch provider {
		case ProviderAnthropic:
			req.Header.Set("x-api-key", apiKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		case ProviderGemini:
			req.Header.Set("x-goog-api-key", apiKey)
		default:
			if apiKey != "" {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("cannot reach model catalog")
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("model catalog returned HTTP %d", resp.StatusCode)
		}
		if readErr != nil || len(body) > 4<<20 {
			return nil, fmt.Errorf("model catalog response exceeds limit or could not be read")
		}
		result, err := decodeCatalogPage(body)
		if err != nil {
			return nil, fmt.Errorf("invalid model catalog response")
		}
		add := func(id, name string, contextWindow, maxTokens int64) {
			if !validCatalogID(id) || seen[id] || len(found) >= 5000 {
				return
			}
			seen[id] = true
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
			found = append(found, Model{ID: ModelID(string(provider) + "." + id), Name: name, Provider: provider, APIModel: id, ContextWindow: contextWindow, ContextAuto: true, ContextSource: source, ContextEstimated: estimated, DefaultMaxTokens: maxTokens})
		}
		for _, raw := range result.Data {
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
				continue
			}
			if provider == ProviderGemini {
				if !slices.Contains(item.Methods, "generateContent") {
					continue
				}
				item.ID = strings.TrimPrefix(item.ID, "models/")
			}
			// These endpoints also expose embeddings, audio and image-only models.
			lower := strings.ToLower(item.ID)
			if provider == ProviderOpenAI && (strings.Contains(lower, "embedding") || strings.Contains(lower, "whisper") || strings.Contains(lower, "tts") || strings.Contains(lower, "dall-e") || strings.Contains(lower, "moderation") || strings.Contains(lower, "realtime") || strings.Contains(lower, "transcribe") || strings.Contains(lower, "image") || strings.Contains(lower, "audio") || strings.HasPrefix(lower, "sora")) {
				continue
			}
			if len(item.Architecture.Output) > 0 && !slices.Contains(item.Architecture.Output, "text") {
				continue
			}
			contextWindow := item.ContextWindow
			if item.MaxInputTokens > 0 {
				contextWindow = item.MaxInputTokens
			}
			name := item.DisplayName
			if name == "" {
				name = item.Name
			}
			add(item.ID, name, contextWindow, item.MaxTokens)
		}
		if result.Next != "" {
			q.Set("pageToken", result.Next)
		} else if result.HasMore && result.LastID != "" {
			q.Set("after_id", result.LastID)
		} else {
			return found, nil
		}
	}
	return nil, fmt.Errorf("model catalog pagination exceeds limit")
}
