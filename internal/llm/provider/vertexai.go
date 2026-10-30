package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"cloud.google.com/go/auth/credentials"
)

func newVertexAIClient(opts providerClientOptions) (*geminiClient, error) {
	project := os.Getenv("VERTEXAI_PROJECT")
	if project == "" {
		project = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	location := os.Getenv("VERTEXAI_LOCATION")
	if location == "" {
		location = os.Getenv("GOOGLE_CLOUD_LOCATION")
	}
	if location == "" {
		location = "us-central1"
	}
	if project == "" {
		return nil, fmt.Errorf("provider VertexAI requires VERTEXAI_PROJECT or GOOGLE_CLOUD_PROJECT")
	}
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(location) {
		return nil, fmt.Errorf("invalid VertexAI location")
	}
	cred, err := credentials.DetectDefault(&credentials.DetectOptions{Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"}})
	if err != nil {
		return nil, fmt.Errorf("create VertexAI credential: %w", err)
	}
	c := &geminiClient{providerOptions: opts}
	for _, apply := range opts.geminiOptions {
		apply(&c.options)
	}
	host := location + "-aiplatform.googleapis.com"
	if location == "global" {
		host = "aiplatform.googleapis.com"
	}
	c.modelPath = "/projects/" + url.PathEscape(project) + "/locations/" + url.PathEscape(location) + "/publishers/google/models/" + url.PathEscape(strings.TrimPrefix(opts.model.APIModel, "models/"))
	transport := newAPITransport("https://" + host + "/v1")
	transport.authorize = func(ctx context.Context, req *http.Request, _ []byte) error {
		token, err := cred.Token(ctx)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+token.Value)
		}
		return err
	}
	c.httpProvider = &httpProvider{transport: transport, protocol: c}
	return c, nil
}
