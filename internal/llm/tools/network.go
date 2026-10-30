package tools

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
)

func networkAddressAllowed(address net.IP, allowPrivate bool) bool {
	if address == nil || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsUnspecified() || address.IsMulticast() {
		return false
	}
	if address.IsLoopback() || address.IsPrivate() {
		return allowPrivate
	}
	return address.IsGlobalUnicast()
}
func networkDo(ctx context.Context, permissions permission.Service, tool string, req *http.Request, timeout time.Duration) (*http.Response, error) {
	policy := networkPolicy{ctx: ctx, permissions: permissions, tool: tool, approved: map[string]bool{}}
	if err := policy.approve(req.URL); err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = policy.dial
	client := &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: policy.redirect}

	response, err := client.Do(req.WithContext(ctx))
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	response.Body = &networkBody{ReadCloser: response.Body, closeIdle: transport.CloseIdleConnections}
	return response, nil
}

type networkBody struct {
	io.ReadCloser
	closeIdle func()
}

func (b *networkBody) Close() error { err := b.ReadCloser.Close(); b.closeIdle(); return err }
func boundedBody(r io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("network response exceeds size limit")
	}
	return raw, nil
}

type networkPolicy struct {
	ctx         context.Context
	permissions permission.Service
	tool        string
	approved    map[string]bool
}

func (p *networkPolicy) approve(target *url.URL) error {
	if target.Scheme != "https" && target.Scheme != "http" || target.User != nil || target.Hostname() == "" {
		return fmt.Errorf("invalid network destination")
	}
	scope := "network:" + target.Scheme + "://" + strings.ToLower(target.Host)
	if p.approved[scope] {
		return nil
	}
	session, _ := GetContextValues(p.ctx)
	if p.permissions == nil || !p.permissions.Request(p.ctx, permission.CreatePermissionRequest{SessionID: session, ToolName: p.tool, Action: "network", Path: scope, Description: "Connect to " + scope}) {
		return permission.ErrorPermissionDenied
	}
	p.approved[scope] = true
	return nil

}

func (p *networkPolicy) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	allowPrivate := config.Get() != nil && config.Get().Security.AllowPrivateNetwork
	for _, address := range addresses {
		if !networkAddressAllowed(address.IP, allowPrivate) {
			return nil, fmt.Errorf("private, local or metadata network destination blocked by policy")
		}
	}
	var failure error
	for _, address := range addresses {
		conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		failure = err
	}
	return nil, failure

}

func (p *networkPolicy) redirect(next *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("too many redirects")
	}
	if via[0].URL.Scheme == "https" && next.URL.Scheme != "https" {
		return fmt.Errorf("HTTPS downgrade blocked")
	}
	return p.approve(next.URL)

}
