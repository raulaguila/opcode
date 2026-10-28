package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaRuntime is observed allocation, not a promise about free GPU memory.
type OllamaRuntime struct {
	Name    string `json:"name"`
	Context int64  `json:"context_length"`
	Size    int64  `json:"size"`
	VRAM    int64  `json:"size_vram"`
}

func (m OllamaRuntime) Placement() string {
	if m.VRAM <= 0 {
		return "CPU"
	}
	if m.Size > 0 && m.VRAM >= m.Size {
		return "GPU"
	}
	return "CPU/GPU"
}

// InspectOllama uses only GET /api/ps. It never loads/generates/downloads models
// or follows redirects (which could disclose a configured Authorization header).
func InspectOllama(ctx context.Context, endpoint, key string) ([]OllamaRuntime, error) {
	base, err := NormalizeLocalEndpoint(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid Ollama endpoint")
	}
	base = strings.TrimSuffix(base, "/v1")
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/ps", nil)
	if err != nil {
		return nil, fmt.Errorf("invalid Ollama status request")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Ollama status unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama status unavailable (HTTP %d)", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("Ollama status exceeds 1 MiB")
	}
	var body struct {
		Models []OllamaRuntime `json:"models"`
	}
	if json.Unmarshal(raw, &body) != nil || len(body.Models) > 256 {
		return nil, fmt.Errorf("invalid Ollama status response")
	}
	for _, m := range body.Models {
		if m.Context < 0 || m.Size < 0 || m.VRAM < 0 || len(m.Name) > 512 {
			return nil, fmt.Errorf("invalid Ollama allocation")
		}
	}
	return body.Models, nil
}
