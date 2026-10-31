package agent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/localstate"
	"github.com/stretchr/testify/require"
)

func oauthFixture(t *testing.T) config.MCPServer {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return config.MCPServer{Type: config.MCPHTTP, URL: "https://resource.example/mcp", OAuth: &config.MCPOAuthConfig{ClientID: "opcode-test", Scopes: []string{"read"}}}
}
func TestOAuthPrivateStoreBindsResourceRedactsAndForgets(t *testing.T) {
	server := oauthFixture(t)
	store, err := newOAuthTokenStore(server)
	require.NoError(t, err)
	_, err = store.GetToken(context.Background())
	require.ErrorIs(t, err, transport.ErrNoToken)
	token := &transport.Token{Issuer: "https://issuer.example", AccessToken: "test-oauth-access-secret", RefreshToken: "test-oauth-refresh-secret", ExpiresAt: time.Now().Add(time.Hour)}
	store.generation = "test-generation"
	require.NoError(t, localstate.Write(store.path, oauthDocument{Binding: store.binding, Generation: store.generation}, oauthDocumentLimit))
	require.NoError(t, store.SaveToken(context.Background(), token))
	info, err := os.Stat(store.path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	got, err := store.GetToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, token.Issuer, got.Issuer)
	require.NotContains(t, tools.RedactCredentials("value="+token.AccessToken), token.AccessToken)
	server.URL = "https://another.example/mcp"
	other, err := newOAuthTokenStore(server)
	require.NoError(t, err)
	require.NotEqual(t, store.path, other.path)
	_, err = other.GetToken(context.Background())
	require.ErrorIs(t, err, transport.ErrNoToken)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, store.SaveToken(ctx, token), context.Canceled)
	server.URL = "https://resource.example/mcp"
	require.NoError(t, LogoutMCPOAuth(context.Background(), server))
	require.Error(t, store.SaveToken(context.Background(), token), "an in-flight refresh must not resurrect forgotten credentials")
	require.NoError(t, localstate.Write(store.path, oauthDocument{Binding: store.binding, Generation: "another-login"}, oauthDocumentLimit))
	require.Error(t, store.SaveToken(context.Background(), token), "an old refresh cannot overwrite another login")
	require.NoError(t, LogoutMCPOAuth(context.Background(), server))
	_, err = store.GetToken(context.Background())
	require.ErrorIs(t, err, transport.ErrNoToken)
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("unchanged"), 0600))
	require.NoError(t, os.Symlink(outside, store.path))
	require.Error(t, store.SaveToken(context.Background(), token))
	raw, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "unchanged", string(raw))
}
func TestOAuthLoginRequiresExplicitUIAndProtectedTransport(t *testing.T) {
	server := oauthFixture(t)
	require.Error(t, LoginMCPOAuth(context.Background(), server, nil))
	for _, target := range []string{"http://example.com/mcp", "http://localhost/mcp", "https://user:pass@example.com/mcp"} {
		server.URL = target
		require.Error(t, server.ValidateOAuth())
	}
	server = oauthFixture(t)
	server.Headers = map[string]string{"authorization": "static"}
	require.Error(t, server.ValidateOAuth())
	server = oauthFixture(t)
	server.Type = config.MCPStdio
	require.Error(t, server.ValidateOAuth())
	server = oauthFixture(t)
	ready, err := MCPOAuthStatus(context.Background(), server)
	require.NoError(t, err)
	require.False(t, ready)
	_, err = newMCPClient(server)
	require.ErrorContains(t, err, "login required")
}
func TestOAuthFullPKCEFlowRejectsBadStateAndKeepsIssuer(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var issuer *httptest.Server
	var expectedChallenge string
	var exchanges atomic.Int64
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource"):
			json.NewEncoder(w).Encode(map[string]any{"resource": issuer.URL + "/mcp", "authorization_servers": []string{issuer.URL}})
		case r.URL.Path == "/.well-known/oauth-authorization-server":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer.URL, "authorization_endpoint": issuer.URL + "/authorize", "token_endpoint": issuer.URL + "/token", "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "authorization_response_iss_parameter_supported": true})
		case r.URL.Path == "/token":
			r.ParseForm()
			hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(hash[:]) != expectedChallenge || r.Form.Get("code") != "valid-code" || r.Form.Get("resource") != issuer.URL+"/mcp" {
				http.Error(w, "bad proof", 400)
				return
			}
			exchanges.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"access_token": "pkce-access-token-secret", "refresh_token": "pkce-refresh-token-secret", "token_type": "Bearer", "expires_in": 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	defer issuer.Close()
	server := config.MCPServer{Type: config.MCPHTTP, URL: issuer.URL + "/mcp", OAuth: &config.MCPOAuthConfig{ClientID: "public-client"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	callbackResult := make(chan error, 1)
	err := LoginMCPOAuth(ctx, server, func(link string) error {
		u, err := url.Parse(link)
		if err != nil {
			return err
		}
		values := u.Query()
		expectedChallenge = values.Get("code_challenge")
		if values.Get("code_challenge_method") != "S256" || expectedChallenge == "" {
			return fmt.Errorf("missing PKCE")
		}
		go sendOAuthCallbackFixtures(values, issuer.URL, callbackResult)
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, <-callbackResult)
	require.EqualValues(t, 1, exchanges.Load())
	store, err := newOAuthTokenStore(server)
	require.NoError(t, err)
	token, err := store.GetToken(ctx)
	require.NoError(t, err)
	require.Equal(t, issuer.URL, token.Issuer)
	ready, err := MCPOAuthStatus(ctx, server)
	require.NoError(t, err)
	require.True(t, ready)
}

func TestOAuthCallbackRejectsInvalidIssuerBeforeCodeExchange(t *testing.T) {
	var issuer *httptest.Server
	var exchanges atomic.Int64
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource"):
			json.NewEncoder(w).Encode(map[string]any{"resource": issuer.URL + "/mcp", "authorization_servers": []string{issuer.URL}})
		case r.URL.Path == "/.well-known/oauth-authorization-server":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer.URL, "authorization_endpoint": issuer.URL + "/authorize", "token_endpoint": issuer.URL + "/token", "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "authorization_response_iss_parameter_supported": true})
		case r.URL.Path == "/token":
			exchanges.Add(1)
			http.Error(w, "code must not be exchanged", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer issuer.Close()
	for _, tc := range []struct {
		name   string
		issuer []string
	}{
		{name: "missing required issuer"},
		{name: "different issuer", issuer: []string{issuer.URL + "/lookalike"}},
		{name: "repeated issuer", issuer: []string{issuer.URL, issuer.URL}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			handler := transport.NewOAuthHandler(transport.OAuthConfig{ClientID: "public-client", RedirectURI: "http://127.0.0.1/callback", PKCEEnabled: true, HTTPClient: oauthHTTPClient()})
			handler.SetBaseURL(issuer.URL + "/mcp")
			_, err := handler.GetAuthorizationURL(ctx, "expected-state", "challenge")
			require.NoError(t, err)
			callback := make(chan url.Values, 1)
			callback <- url.Values{"code": {"valid-code"}, "state": {"expected-state"}, "iss": tc.issuer}
			require.ErrorContains(t, awaitOAuthAuthorization(ctx, handler, "verifier", callback), "code exchange failed")
			require.Zero(t, exchanges.Load(), "invalid issuer must be rejected before reaching the token endpoint")
		})
	}
}

func TestOAuthResponseLimitPropagatesError(t *testing.T) {
	body := &oauthLimitedBody{ReadCloser: io.NopCloser(strings.NewReader("12345")), remaining: 4}
	_, err := io.ReadAll(body)
	require.ErrorContains(t, err, "exceeds")
	require.False(t, errors.Is(err, io.EOF))
}

func TestOAuthHTTPClientNeverFollowsRedirectsWithBearer(t *testing.T) {
	var forwarded atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	request, err := http.NewRequest("GET", source.URL, nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer private-oauth-token")
	client := oauthHTTPClient()
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	require.NoError(t, err)
	response.Body.Close()
	require.Equal(t, 307, response.StatusCode)
	require.Zero(t, forwarded.Load())
}

func sendOAuthCallbackFixtures(values url.Values, issuerURL string, callbackResult chan<- error) {
	callback, _ := url.Parse(values.Get("redirect_uri"))
	q := url.Values{"code": {"valid-code"}, "state": {"wrong-state"}, "iss": {issuerURL}}
	callback.RawQuery = q.Encode()
	response, err := http.Get(callback.String())
	if err != nil {
		callbackResult <- err
		return
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 400 {
		callbackResult <- fmt.Errorf("bad state accepted")
		return
	}
	q.Set("state", values.Get("state"))
	callback.RawQuery = q.Encode()
	response, err = http.Get(callback.String())
	if err == nil {
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}
	callbackResult <- err
}
