package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/localstate"
)

const oauthDocumentLimit = 128 << 10

type oauthDocument struct {
	Binding      string           `json:"binding"`
	Generation   string           `json:"generation"`
	ClientID     string           `json:"client_id,omitempty"`
	ClientSecret string           `json:"client_secret,omitempty"`
	Issuer       string           `json:"issuer,omitempty"`
	AuthMethod   string           `json:"auth_method,omitempty"`
	Token        *transport.Token `json:"token,omitempty"`
}
type oauthTokenStore struct{ path, binding, generation string }

func newOAuthTokenStore(server config.MCPServer) (*oauthTokenStore, error) {
	if server.OAuth == nil {
		return nil, fmt.Errorf("MCP server does not enable OAuth")
	}
	if err := server.ValidateOAuth(); err != nil {
		return nil, err
	}
	directory, err := config.AuthDirectory()
	if err != nil {
		return nil, err
	}
	// Bind credentials to the resource, client, requested scopes and secret
	// reference. Renaming an integration neither duplicates nor widens its token.
	key, _ := json.Marshal(struct {
		URL   string
		OAuth *config.MCPOAuthConfig
	}{server.URL, server.OAuth})
	hash := sha256.Sum256(key)
	binding := hex.EncodeToString(hash[:])
	store := &oauthTokenStore{path: filepath.Join(directory, "mcp-"+binding+".json"), binding: binding}
	unlock := localstate.Lock(store.path)
	d, readErr := store.load()
	unlock()
	if readErr != nil && !errors.Is(readErr, transport.ErrNoToken) {
		return nil, readErr
	}
	store.generation = d.Generation
	return store, nil
}
func (s *oauthTokenStore) load() (oauthDocument, error) {
	d := oauthDocument{Binding: s.binding}
	err := localstate.Read(s.path, oauthDocumentLimit, &d)
	if errors.Is(err, os.ErrNotExist) {
		return d, transport.ErrNoToken
	}
	if err != nil {
		return d, fmt.Errorf("cannot read private MCP OAuth state")
	}
	if d.Binding != s.binding {
		return d, fmt.Errorf("MCP OAuth resource binding mismatch")
	}
	values := []string{d.ClientSecret}
	if d.Token != nil {
		values = append(values, d.Token.AccessToken, d.Token.RefreshToken)
	}
	config.RegisterRuntimeCredentials(s.binding, values...)
	return d, nil
}
func (s *oauthTokenStore) GetToken(ctx context.Context) (*transport.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	unlock := localstate.Lock(s.path)
	defer unlock()
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	if s.generation == "" || d.Generation != s.generation {
		return nil, transport.ErrNoToken
	}
	if d.Token == nil || d.Token.AccessToken == "" {
		return nil, transport.ErrNoToken
	}
	return d.Token, nil
}
func (s *oauthTokenStore) SaveToken(ctx context.Context, token *transport.Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if token == nil || len(token.AccessToken) > 65536 || len(token.RefreshToken) > 65536 {
		return fmt.Errorf("invalid MCP OAuth token")
	}
	unlock := localstate.Lock(s.path)
	defer unlock()
	d, err := s.load()
	if err != nil {
		return err
	}
	if s.generation == "" || d.Generation != s.generation {
		return fmt.Errorf("MCP OAuth credentials were forgotten or replaced; login again")
	}
	d.Token = token
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := localstate.Write(s.path, d, oauthDocumentLimit); err != nil {
		return fmt.Errorf("cannot save private MCP OAuth token")
	}
	config.RegisterRuntimeCredentials(s.binding, d.ClientSecret, token.AccessToken, token.RefreshToken)
	return nil
}
func (s *oauthTokenStore) saveClient(ctx context.Context, h *transport.OAuthHandler) error {
	metadata, err := h.GetServerMetadata(ctx)
	if err != nil {
		return fmt.Errorf("MCP OAuth issuer discovery failed")
	}
	unlock := localstate.Lock(s.path)
	defer unlock()
	d, err := s.load()
	if err != nil && !errors.Is(err, transport.ErrNoToken) {
		return err
	}
	d.ClientID, d.ClientSecret, d.Issuer, d.AuthMethod = h.GetClientID(), h.GetClientSecret(), metadata.Issuer, h.GetTokenEndpointAuthMethod()
	d.Generation = rand.Text()
	d.Token = nil
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := localstate.Write(s.path, d, oauthDocumentLimit); err != nil {
		return err
	}
	s.generation = d.Generation
	return nil
}

type oauthRoundTripper struct{ base http.RoundTripper }

func (t oauthRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	u := r.URL
	if u == nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && !(u.Scheme == "http" && net.ParseIP(u.Hostname()).IsLoopback())) {
		return nil, fmt.Errorf("OAuth endpoint must use HTTPS or a loopback IP")
	}
	response, err := t.base.RoundTrip(r)
	if err == nil {
		response.Body = &oauthLimitedBody{ReadCloser: response.Body, remaining: 2 << 20}
	}
	return response, err
}

type oauthLimitedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *oauthLimitedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		var probe [1]byte
		n, err := b.ReadCloser.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("OAuth response exceeds 2 MiB")
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}
func oauthHTTPClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	return &http.Client{Transport: oauthRoundTripper{t}, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func oauthClientConfig(server config.MCPServer, redirect string) (transport.OAuthConfig, *oauthTokenStore, error) {
	store, err := newOAuthTokenStore(server)
	if err != nil {
		return transport.OAuthConfig{}, nil, err
	}
	o := server.OAuth
	result := transport.OAuthConfig{ClientID: o.ClientID, ClientSecret: config.Environment(o.ClientSecretEnv), RedirectURI: redirect, Scopes: append([]string(nil), o.Scopes...), TokenStore: store, PKCEEnabled: true, HTTPClient: oauthHTTPClient()}
	unlock := localstate.Lock(store.path)
	d, err := store.load()
	unlock()
	if err != nil && !errors.Is(err, transport.ErrNoToken) {
		return result, nil, err
	}
	if d.ClientID != "" {
		result.ClientID, result.CredentialsIssuer, result.TokenEndpointAuthMethod = d.ClientID, d.Issuer, d.AuthMethod
		if result.ClientSecret == "" {
			result.ClientSecret = d.ClientSecret
		}
	}
	if o.ClientSecretEnv != "" && result.ClientSecret == "" {
		return result, nil, fmt.Errorf("OAuth client secret environment variable is not set")
	}
	return result, store, nil
}

// LoginMCPOAuth is a human-invoked operation only. No tool can initiate login,
// open a browser or grant tool permission by receiving an authorization URL.
// The callback listener exists for at most five minutes and validates state and
// issuer before exchanging the authorization code with its PKCE verifier.
func LoginMCPOAuth(parent context.Context, server config.MCPServer, notify func(string) error) error {
	if notify == nil {
		return fmt.Errorf("OAuth login requires a user-visible authorization URL")
	}
	if err := server.ValidateOAuth(); err != nil {
		return err
	}
	if server.OAuth == nil {
		return fmt.Errorf("enable oauth in this MCP server configuration first")
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(server.OAuth.RedirectPort)))
	if err != nil {
		return fmt.Errorf("cannot open MCP OAuth loopback callback")
	}
	defer listener.Close()
	redirect := "http://" + listener.Addr().String() + "/oauth/callback"
	cfg, store, err := oauthClientConfig(server, redirect)
	if err != nil {
		return err
	}
	handler := transport.NewOAuthHandler(cfg)
	handler.SetBaseURL(server.URL)
	if cfg.ClientID == "" {
		if err := handler.RegisterClient(ctx, "Opcode"); err != nil {
			return fmt.Errorf("MCP OAuth registration failed; configure a pre-registered clientID if required")
		}
	}
	var random [64]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	state := base64.RawURLEncoding.EncodeToString(random[:32])
	verifier := base64.RawURLEncoding.EncodeToString(random[32:])
	hash := sha256.Sum256([]byte(verifier))
	link, err := handler.GetAuthorizationURL(ctx, state, base64.RawURLEncoding.EncodeToString(hash[:]))
	if err != nil {
		return fmt.Errorf("MCP OAuth authorization discovery failed")
	}
	authorizationURL, parseErr := url.Parse(link)
	if parseErr != nil || len(link) > 16384 || authorizationURL.User != nil || authorizationURL.Hostname() == "" || (authorizationURL.Scheme != "https" && !(authorizationURL.Scheme == "http" && net.ParseIP(authorizationURL.Hostname()).IsLoopback())) {
		return fmt.Errorf("invalid MCP OAuth authorization URL")
	}
	if err := store.saveClient(ctx, handler); err != nil {
		return err
	}
	if err := notify(link); err != nil {
		return err
	}
	callback := make(chan url.Values, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		query := r.URL.Query()
		if len(r.URL.RawQuery) > 8192 || r.Host != listener.Addr().String() || subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(state)) != 1 || len(query["state"]) != 1 || len(query["code"]) > 1 || len(query["iss"]) > 1 {
			http.Error(w, "Invalid OAuth callback", 400)
			return
		}
		select {
		case callback <- query:
			fmt.Fprintln(w, "Autorização recebida. Volte ao Opcode.")
		default:
			http.Error(w, "Callback already received", 409)
		}
	})
	web := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: 16 << 10}
	defer web.Close()
	go web.Serve(listener)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case query := <-callback:
		if query.Get("error") != "" {
			return fmt.Errorf("MCP OAuth authorization was denied")
		}
		if strings.TrimSpace(query.Get("code")) == "" {
			return fmt.Errorf("MCP OAuth callback has no authorization code")
		}
		if err := handler.ProcessAuthorizationCallback(ctx, query, verifier); err != nil {
			return fmt.Errorf("MCP OAuth code exchange failed; no tool permission was granted")
		}
		return nil
	}
}

func MCPOAuthStatus(ctx context.Context, server config.MCPServer) (bool, error) {
	store, err := newOAuthTokenStore(server)
	if err != nil {
		return false, err
	}
	token, err := store.GetToken(ctx)
	if errors.Is(err, transport.ErrNoToken) {
		return false, nil
	}
	return err == nil && token != nil && (!token.IsExpired() || token.RefreshToken != ""), err
}

// Logout forgets local credentials. It does not claim to revoke a remote grant;
// the authorization server's account settings remain authoritative for that.
func LogoutMCPOAuth(ctx context.Context, server config.MCPServer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store, err := newOAuthTokenStore(server)
	if err != nil {
		return err
	}
	unlock := localstate.Lock(store.path)
	defer unlock()
	root, err := localstate.OpenPrivateRoot(filepath.Dir(store.path), false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	err = root.Remove(filepath.Base(store.path))
	if os.IsNotExist(err) {
		err = nil
	}
	if err == nil {
		config.RegisterRuntimeCredentials(store.binding)
	}
	return err
}
