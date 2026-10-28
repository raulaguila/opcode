package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/raulaguila/opcode/internal/config"
)

type copilotOptions struct {
	reasoningEffort string
	extraHeaders    map[string]string
	bearerToken     string
}
type CopilotOption func(*copilotOptions)
type copilotClient struct {
	*openaiClient
	options     copilotOptions
	httpClient  *http.Client
	tokenMu     sync.Mutex
	githubToken string
	token       string
	expiresAt   time.Time
}
type CopilotTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

func newCopilotClient(opts providerClientOptions) (*copilotClient, error) {
	options := copilotOptions{}
	for _, apply := range opts.copilotOptions {
		apply(&options)
	}
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		githubToken = opts.apiKey
	}
	if githubToken == "" && options.bearerToken == "" {
		githubToken, _ = config.LoadGitHubToken()
	}
	if githubToken == "" && options.bearerToken == "" {
		return nil, fmt.Errorf("GitHub token is required for Copilot")
	}
	opts.openaiOptions = append(opts.openaiOptions, WithOpenAIBaseURL("https://api.githubcopilot.com"), WithReasoningEffort(options.reasoningEffort))
	c := &copilotClient{openaiClient: newOpenAIClient(opts), options: options, githubToken: githubToken, token: options.bearerToken, httpClient: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	c.transport.headers.Del("Authorization")
	for k, v := range map[string]string{"Editor-Version": "Opcode/1.0", "Editor-Plugin-Version": "Opcode/1.0", "Copilot-Integration-Id": "vscode-chat"} {
		c.transport.headers.Set(k, v)
	}
	for k, v := range options.extraHeaders {
		c.transport.headers.Set(k, v)
	}
	c.transport.authorize = func(ctx context.Context, req *http.Request, _ []byte) error {
		c.tokenMu.Lock()
		defer c.tokenMu.Unlock()
		if c.token == "" || !c.expiresAt.IsZero() && time.Until(c.expiresAt) < time.Minute {
			if err := c.refreshToken(ctx); err != nil {
				return err
			}
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		return nil
	}
	c.transport.refresh = func(ctx context.Context) error {
		c.tokenMu.Lock()
		defer c.tokenMu.Unlock()
		return c.refreshToken(ctx)
	}
	return c, nil
}

// Caller holds tokenMu, so concurrent requests share refreshed credentials.
func (c *copilotClient) refreshToken(ctx context.Context) error {
	if c.githubToken == "" {
		return fmt.Errorf("Copilot bearer token expired; a GitHub token is required to refresh it")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/copilot_internal/v2/token", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+c.githubToken)
	req.Header.Set("User-Agent", "Opcode/1.0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("exchange Copilot token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Copilot token exchange HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return err
	}
	var result CopilotTokenResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if result.Token == "" {
		return fmt.Errorf("Copilot token exchange returned an empty token")
	}
	c.token = result.Token
	c.expiresAt = time.Time{}
	if result.ExpiresAt > 0 {
		c.expiresAt = time.Unix(result.ExpiresAt, 0)
	}
	return nil
}
func WithCopilotReasoningEffort(effort string) CopilotOption {
	return func(o *copilotOptions) {
		var options openaiOptions
		WithReasoningEffort(effort)(&options)
		o.reasoningEffort = options.reasoningEffort
	}
}
func WithCopilotExtraHeaders(headers map[string]string) CopilotOption {
	return func(o *copilotOptions) { o.extraHeaders = headers }
}
func WithCopilotBearerToken(token string) CopilotOption {
	return func(o *copilotOptions) { o.bearerToken = token }
}
