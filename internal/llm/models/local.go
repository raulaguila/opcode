package models

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

const (
	ProviderLocal ModelProvider = "local"

	localModelsPath        = "v1/models"
	lmStudioBetaModelsPath = "api/v0/models"
)

// NormalizeLocalEndpoint accepts an OpenAI-compatible API base URL. A server
// origin without a path uses /v1; explicit path prefixes are preserved.
func NormalizeLocalEndpoint(endpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("configure a valid local API base URL using providers.local.baseURL or LOCAL_ENDPOINT (e.g. http://127.0.0.1:11434/v1)")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "" {
		u.Path = "/v1"
	}
	u.RawPath = ""
	return u.String(), nil
}

// DiscoverLocalModels runs during configuration loading, never during package
// initialization. It uses bounded, cancellable requests and does not mutate the
// model registry until the caller has a complete result.
func DiscoverLocalModels(ctx context.Context, endpoint, apiKey string) ([]Model, error) {
	endpoint, err := NormalizeLocalEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var found []localModel
	compatibleCatalog := false
	// Keep LM Studio's loaded-context metadata when its richer API is available.
	u, _ := url.Parse(endpoint)
	if u.Path == "/v1" {
		u.Path = "/" + lmStudioBetaModelsPath
		found, _ = listLocalModels(ctx, client, u.String(), apiKey, true)
	}
	if len(found) == 0 {
		compatibleCatalog = true
		found, err = listLocalModels(ctx, client, endpoint+"/models", apiKey, false)
		if err != nil {
			return nil, err
		}
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("local server returned no language models; load a model in Ollama or LM Studio")
	}
	if compatibleCatalog {
		if running, err := InspectOllama(ctx, endpoint, apiKey); err == nil {
			enrichOllamaContexts(ctx, client, endpoint, apiKey, found, running)
		}
	}
	slices.SortStableFunc(found, func(a, b localModel) int {
		if (a.State == "loaded") == (b.State == "loaded") {
			return 0
		}
		if a.State == "loaded" {
			return -1
		}
		return 1
	})
	result := make([]Model, 0, len(found))
	for _, m := range found {
		result = append(result, convertLocalModel(m))
	}
	return result, nil
}

type localModelList struct {
	Data []localModel `json:"data"`
}

type localModel struct {
	ID                  string `json:"id"`
	Object              string `json:"object"`
	Type                string `json:"type"`
	Publisher           string `json:"publisher"`
	Arch                string `json:"arch"`
	CompatibilityType   string `json:"compatibility_type"`
	Quantization        string `json:"quantization"`
	State               string `json:"state"`
	MaxContextLength    int64  `json:"max_context_length"`
	LoadedContextLength int64  `json:"loaded_context_length"`
	ContextSource       string
}

func listLocalModels(ctx context.Context, client *http.Client, endpoint, apiKey string, lmStudio bool) ([]localModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid local discovery request")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("cannot connect to local model server; start Ollama or LM Studio and check providers.local.baseURL / LOCAL_ENDPOINT")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local model discovery returned HTTP %d", resp.StatusCode)
	}
	const maxBody = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(raw) > maxBody {
		return nil, fmt.Errorf("cannot read local model list (maximum 1 MiB)")
	}
	var modelList localModelList
	if err := json.Unmarshal(raw, &modelList); err != nil {
		return nil, fmt.Errorf("local server returned an invalid model list")
	}
	var result []localModel
	for _, model := range modelList.Data {
		if !validCatalogID(model.ID) || (model.Type != "" && model.Type != "llm") || (lmStudio && model.Object != "model") {
			continue
		}
		result = append(result, model)
	}
	return result, nil
}

func convertLocalModel(model localModel) Model {
	source := model.ContextSource
	if source == "" {
		switch {
		case model.LoadedContextLength > 0:
			source = "lmstudio:loaded"
		case model.MaxContextLength > 0:
			source = "lmstudio:model"
		default:
			source = "fallback"
		}
	}
	return Model{
		ID:                  ModelID("local." + model.ID),
		Name:                friendlyModelName(model.ID),
		Provider:            ProviderLocal,
		APIModel:            model.ID,
		ContextEstimated:    model.LoadedContextLength == 0 && model.MaxContextLength == 0,
		ContextAuto:         true,
		ContextSource:       source,
		ContextWindow:       cmp.Or(model.LoadedContextLength, model.MaxContextLength, 4096),
		DefaultMaxTokens:    min(4096, cmp.Or(model.LoadedContextLength, model.MaxContextLength, 4096)/2),
		CanReason:           false,
		SupportsAttachments: false,
	}
}

var modelInfoRegex = regexp.MustCompile(`(?i)^([a-z0-9]+)(?:[-_]?([rv]?\d[\.\d]*))?(?:[-_]?([a-z]+))?.*`)

func friendlyModelName(modelID string) string {
	mainID := modelID
	tag := ""

	if slash := strings.LastIndex(mainID, "/"); slash != -1 {
		mainID = mainID[slash+1:]
	}

	if at := strings.Index(modelID, "@"); at != -1 {
		mainID = modelID[:at]
		tag = modelID[at+1:]
	}

	match := modelInfoRegex.FindStringSubmatch(mainID)
	if match == nil {
		return modelID
	}

	capitalize := func(s string) string {
		if s == "" {
			return ""
		}
		runes := []rune(s)
		runes[0] = unicode.ToUpper(runes[0])
		return string(runes)
	}

	family := capitalize(match[1])
	version := ""
	label := ""

	if len(match) > 2 && match[2] != "" {
		version = strings.ToUpper(match[2])
	}

	if len(match) > 3 && match[3] != "" {
		label = capitalize(match[3])
	}

	var parts []string
	if family != "" {
		parts = append(parts, family)
	}
	if version != "" {
		parts = append(parts, version)
	}
	if label != "" {
		parts = append(parts, label)
	}
	if tag != "" {
		parts = append(parts, tag)
	}

	return strings.Join(parts, " ")
}
