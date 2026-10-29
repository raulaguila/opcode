package httpclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type tlsScope struct {
	origin  string
	enabled bool
}
type scopeKey struct{}

// WithInsecureTLS affects only clients explicitly constructed from this context.
func WithInsecureTLS(ctx context.Context, endpoint string, enabled bool) context.Context {
	if !enabled {
		return context.WithValue(ctx, scopeKey{}, tlsScope{})
	}
	u, _ := url.Parse(endpoint)
	origin := ""
	if u != nil && u.Scheme == "https" && u.Host != "" && u.User == nil {
		origin = strings.ToLower(u.Host)
	}
	return context.WithValue(ctx, scopeKey{}, tlsScope{origin: origin, enabled: true})
}

type scopedTransport struct {
	base   http.RoundTripper
	origin string
	err    error
}

func (t scopedTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

func (t scopedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.err != nil {
		return nil, t.err
	}
	if r.URL.Scheme != "https" || strings.ToLower(r.URL.Host) != t.origin {
		return nil, fmt.Errorf("insecure TLS is restricted to the configured HTTPS origin")
	}
	return t.base.RoundTrip(r)
}

func Transport(ctx context.Context, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	scope, enabled := ctx.Value(scopeKey{}).(tlsScope)
	if !enabled || !scope.enabled {
		return base
	}
	standard, ok := base.(*http.Transport)
	if !ok || scope.origin == "" {
		return scopedTransport{err: fmt.Errorf("cannot configure scoped insecure TLS transport")}
	}
	clone := standard.Clone()
	if clone.TLSClientConfig == nil {
		clone.TLSClientConfig = &tls.Config{}
	} else {
		clone.TLSClientConfig = clone.TLSClientConfig.Clone()
	}
	clone.TLSClientConfig.InsecureSkipVerify = true // Explicit per-connection opt-in; never mutates the default transport.
	return scopedTransport{base: clone, origin: scope.origin}
}
