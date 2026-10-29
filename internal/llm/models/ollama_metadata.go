package models

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// enrichOllamaContexts reads metadata only. /api/show does not load or generate.
// The caller's shared timeout bounds the whole catalog, including partial failures.
func enrichOllamaContexts(ctx context.Context, client *http.Client, endpoint, key string, found []localModel, running []OllamaRuntime) {
	for i := range found {
		if ctx.Err() != nil || i >= 256 {
			return
		}
		model := &found[i]
		for _, active := range running {
			if (active.Name == model.ID || active.Name == model.ID+":latest") && active.Context > 0 {
				model.LoadedContextLength = active.Context
				model.ContextSource = "ollama:loaded"
				model.State = "loaded"
				break
			}
		}
		// Allocation is only a fallback. Native Ollama requests can resize num_ctx;
		// a previous 16k allocation must not hide the model's advertised capacity.
		body, _ := json.Marshal(struct {
			Model string `json:"model"`
		}{model.ID})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(endpoint, "/v1")+"/api/show", bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		response, err := client.Do(req)
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || len(raw) > 1<<20 {
			continue
		}
		var details struct {
			Capabilities *[]string                  `json:"capabilities"`
			Parameters   string                     `json:"parameters"`
			Info         map[string]json.RawMessage `json:"model_info"`
		}
		if json.Unmarshal(raw, &details) != nil {
			continue
		}
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
		context := maximum
		source := "ollama:model"
		for _, line := range strings.Split(details.Parameters, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "num_ctx" {
				if configured, err := strconv.ParseInt(fields[1], 10, 64); err == nil && configured > 0 {
					context, source = configured, "ollama:parameter"
					if maximum > 0 {
						context = min(context, maximum)
					}
				}
			}
		}
		if context > 0 {
			model.MaxContextLength, model.ContextSource = context, source
		}
	}
}
