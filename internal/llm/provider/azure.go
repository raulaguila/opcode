package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
)

type azureProtocol struct {
	*openaiClient
	apiVersion string
}

func (a *azureProtocol) request(messages []message.Message, tools []tools.BaseTool, stream bool) (string, any, error) {
	path, payload, err := a.openaiClient.request(messages, tools, stream)
	if a.apiVersion == "v1" || strings.HasSuffix(a.transport.baseURL, "/openai/v1") {
		return path, payload, err
	}
	if a.providerOptions.model.API != "responses" {
		path = "/deployments/" + url.PathEscape(a.providerOptions.model.APIModel) + path
	}
	return path + "?api-version=" + url.QueryEscape(a.apiVersion), payload, err
}
func newAzureClient(opts providerClientOptions) (*openaiClient, error) {
	endpoint := strings.TrimRight(os.Getenv("AZURE_OPENAI_ENDPOINT"), "/")
	version := os.Getenv("AZURE_OPENAI_API_VERSION")
	if endpoint == "" || version == "" {
		return nil, fmt.Errorf("provider Azure OpenAI requires AZURE_OPENAI_ENDPOINT and AZURE_OPENAI_API_VERSION (or v1)")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.User != nil {
		return nil, fmt.Errorf("provider Azure OpenAI endpoint must be an HTTPS URL without credentials or query parameters")
	}
	base := endpoint
	if !strings.HasSuffix(base, "/openai/v1") {
		if version == "v1" {
			base += "/openai/v1"
		} else if !strings.HasSuffix(base, "/openai") {
			base += "/openai"
		}
	}
	c := newOpenAIClient(opts)
	c.transport.baseURL = base
	c.transport.headers.Del("Authorization")
	if err := configureAzureAuthentication(c, opts.apiKey); err != nil {
		return nil, err
	}
	c.protocol = &azureProtocol{openaiClient: c, apiVersion: version}
	return c, nil
}

func configureAzureAuthentication(c *openaiClient, configuredKey string) error {
	key := configuredKey
	if key == "" {
		key = os.Getenv("AZURE_OPENAI_API_KEY")
	}
	if key != "" {
		c.transport.headers.Set("api-key", key)
	} else {
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return fmt.Errorf("create Azure credential: %w", err)
		}
		c.transport.authorize = func(ctx context.Context, req *http.Request, _ []byte) error {
			token, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{"https://cognitiveservices.azure.com/.default"}})
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+token.Token)
			}
			return err
		}
	}

	return nil
}
