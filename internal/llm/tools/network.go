package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
)

// sharedAddressSpace is carrier NAT and Tailscale, and holds some cloud metadata.
var sharedAddressSpace = &net.IPNet{IP: net.IPv4(100, 64, 0, 0).To4(), Mask: net.CIDRMask(10, 32)}

// environmentProxy is a variable because net/http reads the environment once.
var environmentProxy = http.ProxyFromEnvironment

func networkAddressAllowed(address net.IP, allowPrivate bool) bool {
	if address == nil || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsUnspecified() || address.IsMulticast() {
		return false
	}
	if address.IsLoopback() || address.IsPrivate() || sharedAddressSpace.Contains(address) {
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
	transport.Proxy = policy.proxy
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
	proxies     sync.Map // addresses of proxies chosen for this request
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

// resolveAllowed returns host's addresses, refused when any of them is not allowed.
func resolveAllowed(ctx context.Context, host string) ([]net.IPAddr, error) {
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
	return addresses, nil
}

// proxy keeps the policy for traffic the configured proxy will carry: dial sees
// only the proxy's address, so the destination is checked here instead.
func (p *networkPolicy) proxy(req *http.Request) (*url.URL, error) {
	proxy, err := environmentProxy(req)
	if proxy == nil || err != nil {
		return proxy, err
	}
	// shortcut: a name only the proxy can resolve is not checked, because networks
	// behind a proxy often lack public DNS; each host still needs approval.
	if _, err := resolveAllowed(req.Context(), req.URL.Hostname()); err != nil {
		if _, unresolved := errors.AsType[*net.DNSError](err); !unresolved {
			return nil, err
		}
	}
	port := proxy.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443", "socks5": "1080", "socks5h": "1080"}[proxy.Scheme]
	}
	p.proxies.Store(net.JoinHostPort(proxy.Hostname(), port), true)
	return proxy, nil
}

func (p *networkPolicy) dial(ctx context.Context, network, address string) (net.Conn, error) {
	// The user chose this proxy, and corporate ones live on private addresses.
	if _, chosen := p.proxies.Load(address); chosen {
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, address)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := resolveAllowed(ctx, host)
	if err != nil {
		return nil, err
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
