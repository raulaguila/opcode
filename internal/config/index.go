package config

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

// IndexConfig is resource policy, not an authorization source. Limits may only
// lower the application's hard ceilings. Embeddings are explicitly opt-in.
type IndexConfig struct {
	Background    *bool            `json:"background,omitempty"`
	MaxFiles      int              `json:"maxFiles,omitempty"`
	MaxFileBytes  int64            `json:"maxFileBytes,omitempty"`
	MaxTextBytes  int64            `json:"maxTextBytes,omitempty"`
	MemoryMiB     int              `json:"memoryMiB,omitempty"`
	UpdateSeconds int              `json:"updateSeconds,omitempty"`
	Embeddings    *EmbeddingConfig `json:"embeddings,omitempty"`
}
type EmbeddingConfig struct {
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
}

func (c IndexConfig) Defaults() IndexConfig {
	if c.MaxFiles == 0 {
		c.MaxFiles = 20000
	}
	if c.MaxFileBytes == 0 {
		c.MaxFileBytes = 256 << 10
	}
	if c.MaxTextBytes == 0 {
		c.MaxTextBytes = 20 << 20
	}
	if c.MemoryMiB == 0 {
		c.MemoryMiB = 32
	}
	if c.UpdateSeconds == 0 {
		c.UpdateSeconds = 5
	}
	return c
}
func (c IndexConfig) Validate() error {
	c = c.Defaults()
	if c.MaxFiles < 1 || c.MaxFiles > 20000 || c.MaxFileBytes < 1024 || c.MaxFileBytes > 256<<10 || c.MaxTextBytes < 1024 || c.MaxTextBytes > 20<<20 || c.MemoryMiB < 4 || c.MemoryMiB > 128 || c.UpdateSeconds < 1 || c.UpdateSeconds > 30 {
		return fmt.Errorf("index limits: maxFiles 1–20000, maxFileBytes 1024–262144, maxTextBytes 1024–20971520, memoryMiB 4–128, updateSeconds 1–30")
	}
	if c.Embeddings != nil && (c.Embeddings.Endpoint == "" || c.Embeddings.Model == "") {
		return fmt.Errorf("index.embeddings requires endpoint and model")
	}
	if c.Embeddings != nil {
		return c.Embeddings.Validate()
	}
	return nil
}

func (c EmbeddingConfig) Validate() error {
	u, err := url.Parse(c.Endpoint)
	if err != nil {
		return fmt.Errorf("invalid embedding endpoint")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/api/embed" || len(c.Model) < 1 || len(c.Model) > 160 || strings.ContainsAny(c.Model, "\r\n\x00") {
		return fmt.Errorf("embeddings require an explicit HTTP loopback IP /api/embed endpoint and model; no downloads")
	}
	return nil
}

func (c LSPConfig) ValidateRouting() error {
	if len(c.FileTypes) > 32 || len(c.RootMarkers) > 16 {
		return fmt.Errorf("LSP fileTypes max 32, rootMarkers max 16")
	}
	for _, kind := range c.FileTypes {
		if len(kind) == 0 || len(kind) > 80 || strings.ContainsAny(kind, "/\\\r\n\x00") {
			return fmt.Errorf("invalid LSP file type")
		}
	}
	for _, marker := range c.RootMarkers {
		if !filepath.IsLocal(marker) || len(marker) > 160 || strings.ContainsAny(marker, "\r\n\x00") {
			return fmt.Errorf("LSP root marker must be a relative path <=160 bytes")
		}
	}
	return nil
}
