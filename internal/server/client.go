package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client uses a loopback HTTP connection and headers only. It deliberately
// refuses redirects so bearer credentials cannot leave the configured server.
type Client struct {
	base, token string
	http        *http.Client
}

func NewClient(base, token string) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || !net.ParseIP(u.Hostname()).IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("server URL must be http://127.0.0.1:PORT (or another loopback IP)")
	}
	if len(token) < 32 || len(token) > 512 || strings.ContainsAny(token, " \r\n\t") {
		return nil, fmt.Errorf("OPCODE_SERVER_TOKEN must contain 32–512 characters without spaces")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Client{strings.TrimRight(base, "/"), token, &http.Client{Transport: transport, Timeout: 32 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
func (c *Client) Do(ctx context.Context, method, path string, body, result any) error {
	if !strings.HasPrefix(path, "/v1/") || strings.ContainsAny(path, "\r\n") {
		return fmt.Errorf("invalid server API path")
	}
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
		if len(data) > MaxBody {
			return fmt.Errorf("request exceeds 64 KiB")
		}
	}
	r, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(r)
	if err != nil {
		return fmt.Errorf("servidor indisponível; reconecte ao mesmo endereço e sessão")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || len(raw) > 8<<20 {
		return fmt.Errorf("invalid or oversized server response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &failure) != nil || failure.Error == "" {
			return fmt.Errorf("server returned HTTP %d", response.StatusCode)
		}
		return fmt.Errorf("%s", failure.Error)
	}
	if result != nil && json.Unmarshal(raw, result) != nil {
		return fmt.Errorf("invalid server response")
	}
	return nil
}
