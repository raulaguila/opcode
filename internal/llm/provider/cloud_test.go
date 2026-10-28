package provider

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAzureWireContracts(t *testing.T) {
	for _, test := range []struct{ name, version, api, path string }{
		{"legacy chat", "2025-04-01-preview", "", "/openai/deployments/test-model/chat/completions"},
		{"legacy responses", "2025-04-01-preview", "responses", "/openai/responses"},
		{"v1 chat", "v1", "", "/openai/v1/chat/completions"},
		{"v1 responses", "v1", "responses", "/openai/v1/responses"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AZURE_OPENAI_ENDPOINT", "https://azure.test")
			t.Setenv("AZURE_OPENAI_API_VERSION", test.version)
			opts := testOptions(models.ProviderAzure)
			opts.model.API = test.api
			client, err := newAzureClient(opts)
			require.NoError(t, err)
			result := chatResult
			if test.api == "responses" {
				result = responsesResult
			}
			client.transport.client = wireHTTP(t, 200, "application/json", result, func(req *http.Request, body map[string]any) {
				assert.Equal(t, "azure.test", req.URL.Host)
				assert.Equal(t, test.path, req.URL.Path)
				if test.version == "v1" {
					assert.Empty(t, req.URL.Query().Get("api-version"))
				} else {
					assert.Equal(t, test.version, req.URL.Query().Get("api-version"))
				}
				assert.Equal(t, "unit-test-key", req.Header.Get("api-key"))
				assert.Empty(t, req.Header.Get("Authorization"))
				assert.Equal(t, "test-model", body["model"])
			})
			response, err := client.send(context.Background(), userMessages(), nil)
			require.NoError(t, err)
			require.Equal(t, "ok", response.Content)
		})
	}
}
func TestVertexWireContracts(t *testing.T) {
	// A synthetic ADC file exercises credential discovery without reading the
	// developer's Google account. Token acquisition is substituted below.
	dir := t.TempDir()
	path := filepath.Join(dir, "adc.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"type":"authorized_user","client_id":"test","client_secret":"test","refresh_token":"test"}`), 0600))
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)
	t.Setenv("VERTEXAI_PROJECT", "project-test")
	for _, location := range []string{"us-central1", "global"} {
		t.Run(location, func(t *testing.T) {
			t.Setenv("VERTEXAI_LOCATION", location)
			client, err := newVertexAIClient(testOptions(models.ProviderVertexAI))
			require.NoError(t, err)
			require.NotNil(t, client.transport.authorize)
			client.transport.authorize = func(_ context.Context, req *http.Request, _ []byte) error {
				req.Header.Set("Authorization", "Bearer synthetic-adc")
				return nil
			}
			client.transport.client = wireHTTP(t, 200, "application/json", geminiResult, func(req *http.Request, body map[string]any) {
				host := "us-central1-aiplatform.googleapis.com"
				if location == "global" {
					host = "aiplatform.googleapis.com"
				}
				assert.Equal(t, host, req.URL.Host)
				assert.Equal(t, "/v1/projects/project-test/locations/"+location+"/publishers/google/models/test-model:generateContent", req.URL.Path)
				assert.Equal(t, "Bearer synthetic-adc", req.Header.Get("Authorization"))
				assert.Empty(t, req.Header.Get("x-goog-api-key"))
				assert.NotEmpty(t, body["contents"])
			})
			result, err := client.send(context.Background(), userMessages(), nil)
			require.NoError(t, err)
			require.Equal(t, "ok", result.Content)
		})
	}
}
func TestBedrockSigV4AndModelPaths(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "TESTACCESSKEY")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-secret")
	t.Setenv("AWS_SESSION_TOKEN", "session-token")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	for _, test := range []struct{ region, model, want string }{
		{"us-east-1", "anthropic.claude-test-v1:0", "us.anthropic.claude-test-v1:0"},
		{"ap-northeast-1", "anthropic.claude-test-v1:0", "apac.anthropic.claude-test-v1:0"},
		{"us-west-2", "global.anthropic.claude-test-v1:0", "global.anthropic.claude-test-v1:0"},
	} {
		t.Run(test.region, func(t *testing.T) {
			t.Setenv("AWS_REGION", test.region)
			opts := testOptions(models.ProviderBedrock)
			opts.model.APIModel = test.model
			client, err := newBedrockClient(opts)
			require.NoError(t, err)
			client.transport.client = wireHTTP(t, 200, "application/json", anthropicResult, func(req *http.Request, body map[string]any) {
				assert.Equal(t, "bedrock-runtime."+test.region+".amazonaws.com", req.URL.Host)
				assert.Equal(t, "/model/"+test.want+"/invoke", req.URL.Path)
				assert.Contains(t, req.Header.Get("Authorization"), "Credential=TESTACCESSKEY/")
				assert.Contains(t, req.Header.Get("Authorization"), "/"+test.region+"/bedrock/aws4_request")
				assert.Equal(t, "session-token", req.Header.Get("X-Amz-Security-Token"))
				assert.Empty(t, req.Header.Get("x-api-key"))
				assert.NotContains(t, body, "model")
				assert.NotContains(t, body, "stream")
				assert.Equal(t, "bedrock-2023-05-31", body["anthropic_version"])
				assert.NotContains(t, string(rawJSON(body)), "cache_control")
			})
			result, err := client.send(context.Background(), userMessages(), nil)
			require.NoError(t, err)
			require.Equal(t, "ok", result.Content)
		})
	}
}
func bedrockFrames(t *testing.T, chunks []string, exception bool) []byte {
	t.Helper()
	var buffer bytes.Buffer
	encoder := eventstream.NewEncoder()
	for _, chunk := range chunks {
		kind := "event"
		payload := rawJSON(object{"bytes": []byte(chunk)})
		if exception {
			kind = "exception"
			payload = []byte(chunk)
		}
		require.NoError(t, encoder.Encode(&buffer, eventstream.Message{Headers: eventstream.Headers{{Name: ":message-type", Value: eventstream.StringValue(kind)}, {Name: ":event-type", Value: eventstream.StringValue("chunk")}}, Payload: payload}))
	}
	return buffer.Bytes()
}
func TestBedrockEventStream(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "synthetic-bearer")
	opts := testOptions(models.ProviderBedrock)
	opts.model.APIModel = "us.anthropic.claude-test-v1:0"
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "exception"}[broken], func(t *testing.T) {
			client, err := newBedrockClient(opts)
			require.NoError(t, err)
			chunks := []string{`{"type":"message_start","message":{"content":[],"usage":{"input_tokens":7}}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`, `{"type":"content_block_stop","index":0}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`, `{"type":"message_stop"}`}
			if broken {
				chunks = []string{`{"message":"upstream failed"}`}
			}
			wire := bedrockFrames(t, chunks, broken)
			client.transport.client = &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "Bearer synthetic-bearer", req.Header.Get("Authorization"))
				assert.Contains(t, req.URL.Path, "invoke-with-response-stream")
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/vnd.amazon.eventstream"}}, Body: io.NopCloser(bytes.NewReader(wire)), Request: req}, nil
			})}
			events := collectEvents(t, client.stream(context.Background(), userMessages(), nil))
			require.NotEmpty(t, events)
			last := events[len(events)-1]
			if broken {
				require.Equal(t, EventError, last.Type)
				require.ErrorContains(t, last.Error, "upstream failed")
			} else {
				require.Equal(t, EventComplete, last.Type)
				require.Equal(t, "ok", last.Response.Content)
				require.EqualValues(t, 7, last.Response.Usage.InputTokens)
			}
		})
	}
}
func TestCopilotRefreshUpdatesActualRequests(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "synthetic-github-token")
	opts := testOptions(models.ProviderCopilot)
	opts.copilotOptions = []CopilotOption{WithCopilotBearerToken("stale-token")}
	client, err := newCopilotClient(opts)
	require.NoError(t, err)
	exchanges, requests := 0, 0
	client.httpClient = &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		exchanges++
		assert.Equal(t, "Token synthetic-github-token", req.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"token":"fresh-token","expires_at":4102444800}`)), Request: req}, nil
	})}
	client.transport.client = wireHTTP(t, 200, "application/json", chatResult, nil)
	responseTransport := client.transport.client.Transport
	client.transport.client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		assert.Equal(t, "api.githubcopilot.com", req.URL.Host)
		assert.Equal(t, "/chat/completions", req.URL.Path)
		assert.Equal(t, "vscode-chat", req.Header.Get("Copilot-Integration-Id"))
		if requests == 1 {
			assert.Equal(t, "Bearer stale-token", req.Header.Get("Authorization"))
			return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("expired")), Request: req}, nil
		}
		assert.Equal(t, "Bearer fresh-token", req.Header.Get("Authorization"))
		return responseTransport.RoundTrip(req)
	})
	result, err := client.send(context.Background(), userMessages(), nil)
	require.NoError(t, err)
	require.Equal(t, "ok", result.Content)
	require.Equal(t, 1, exchanges)
	require.Equal(t, 2, requests)
	_, err = client.send(context.Background(), userMessages(), nil)
	require.NoError(t, err)
	require.Equal(t, 1, exchanges)
}
