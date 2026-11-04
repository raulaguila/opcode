package shell

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/logging"
)

// A sandboxed command has no network. Package managers reach the domains below
// through a proxy that runs outside the sandbox, so the list holds whatever a
// command does and only this process talks to a corporate proxy.
const (
	sandboxEgressSocket = "/tmp/opcode-egress.sock"
	sandboxEgressBridge = "/tmp/opcode-egress-bridge"
	egressDialTimeout   = 30 * time.Second
)

var defaultAllowedDomains = []string{
	"proxy.golang.org", "sum.golang.org",
	"registry.npmjs.org", "registry.yarnpkg.com",
	"pypi.org", "files.pythonhosted.org",
	"crates.io", "index.crates.io", "static.crates.io",
	"repo.maven.apache.org", "repo1.maven.org", "plugins.gradle.org",
	"rubygems.org",
}

// egressDomains is the configured list, or the public registries when it is
// not set. An empty list turns the proxy off.
func egressDomains(cfg *config.Config) []string {
	if cfg == nil || cfg.Security.AllowedDomains == nil {
		return defaultAllowedDomains
	}
	return cfg.Security.AllowedDomains
}

// domainAllowed takes an exact name or *.example.com, which covers subdomains
// only. Nothing allows every host.
func domainAllowed(allowed []string, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host != "" && slices.ContainsFunc(allowed, func(entry string) bool {
		entry = strings.ToLower(entry)
		if suffix, wild := strings.CutPrefix(entry, "*."); wild {
			return suffix != "" && strings.HasSuffix(host, "."+suffix)
		}
		return host == entry
	})
}

type egressProxy struct {
	allowed   func() []string
	upstream  func(*url.URL) (*url.URL, error)
	transport *http.Transport
}

func newEgressProxy(allowed func() []string, upstream func(*url.URL) (*url.URL, error)) *egressProxy {
	return &egressProxy{allowed: allowed, upstream: upstream, transport: &http.Transport{
		Proxy: func(request *http.Request) (*url.URL, error) { return upstream(request.URL) }, TLSHandshakeTimeout: egressDialTimeout,
	}}
}

func (p *egressProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !domainAllowed(p.allowed(), r.URL.Hostname()) {
		http.Error(w, fmt.Sprintf("opcode: %s is not in security.allowedDomains", r.URL.Hostname()), http.StatusForbidden)
		return
	}
	if r.Method == http.MethodConnect {
		p.tunnel(w, r)
		return
	}
	if !r.URL.IsAbs() {
		http.Error(w, "opcode: proxy requests need an absolute URL", http.StatusBadRequest)
		return
	}
	outgoing := r.Clone(r.Context())
	outgoing.RequestURI = ""
	outgoing.Header.Del("Proxy-Connection")
	response, err := p.transport.RoundTrip(outgoing)
	if err != nil {
		http.Error(w, "opcode: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	for name, values := range response.Header {
		w.Header()[name] = values
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (p *egressProxy) tunnel(w http.ResponseWriter, r *http.Request) {
	target, err := p.dial(r.Context(), r.URL.Host)
	if err != nil {
		http.Error(w, "opcode: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer target.Close()
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "opcode: this connection cannot tunnel", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(target, buffered); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, target); done <- struct{}{} }()
	<-done
}

// dial reaches address directly or through the proxy this process was given.
// shortcut: speaks plain HTTP to that proxy, with no credentials; a TLS proxy or
// one that asks for a login needs its own step here.
func (p *egressProxy) dial(ctx context.Context, address string) (net.Conn, error) {
	proxy, err := p.upstream(&url.URL{Scheme: "https", Host: address})
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: egressDialTimeout}
	if proxy == nil {
		return dialer.DialContext(ctx, "tcp", address)
	}
	conn, err := dialer.DialContext(ctx, "tcp", proxy.Host)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(egressDialTimeout))
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", address, address)
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err == nil && response.StatusCode != http.StatusOK {
		err = fmt.Errorf("proxy %s refused %s: %s", proxy.Host, address, response.Status)
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

var egress struct {
	once   sync.Once
	socket string
}

// egressSocket starts the proxy on first use, for the life of this process.
// Failing leaves commands with no network, as they would be without it.
// shortcut: the socket and its private directory stay in the temp folder after
// exit, a few bytes per run; removing them needs a process-wide shutdown hook.
func egressSocket() string {
	egress.once.Do(func() {
		directory, err := os.MkdirTemp("", "opcode-egress-")
		if err == nil {
			var listener net.Listener
			socket := filepath.Join(directory, "proxy.sock")
			if listener, err = net.Listen("unix", socket); err == nil {
				egress.socket = socket
				proxy := newEgressProxy(func() []string { return egressDomains(config.Get()) }, func(target *url.URL) (*url.URL, error) {
					return http.ProxyFromEnvironment(&http.Request{URL: target})
				})
				go func() { _ = (&http.Server{Handler: proxy, ReadHeaderTimeout: egressDialTimeout}).Serve(listener) }()
			}
		}
		if err != nil {
			logging.Warn("sandbox egress proxy unavailable", "error", err)
		}
	})
	return egress.socket
}

// hostCertificates finds the bundle holding a corporate CA. /etc/ssl/certs is
// in every sandbox; the others are mounted when they are the one in use.
func hostCertificates() string {
	candidates := []string{os.Getenv("SSL_CERT_FILE"), "/etc/ssl/certs/ca-certificates.crt", "/etc/pki/tls/certs/ca-bundle.crt", "/etc/ssl/ca-bundle.pem"}
	index := slices.IndexFunc(candidates, func(path string) bool {
		info, err := os.Stat(path)
		return path != "" && err == nil && !info.IsDir()
	})
	if index < 0 {
		return ""
	}
	return candidates[index]
}

// appendEgress starts the command through the bridge, which listens on the
// sandbox's own loopback and hands each connection to the proxy's socket.
func appendEgress(wrapped []string, path string, args []string, cfg *config.Config) ([]string, string, []string) {
	if len(egressDomains(cfg)) == 0 {
		return wrapped, path, args
	}
	executable, err := os.Executable()
	if err == nil {
		_, err = os.Stat(executable)
	}
	socket := egressSocket()
	if err != nil || socket == "" {
		return wrapped, path, args
	}
	wrapped = append(wrapped, "--bind", socket, sandboxEgressSocket, "--ro-bind", executable, sandboxEgressBridge)
	certificates := hostCertificates()
	if certificates != "" && !SensitivePath(certificates) {
		wrapped = append(wrapped, "--ro-bind", certificates, certificates)
	} else {
		certificates = ""
	}
	return wrapped, sandboxEgressBridge, append([]string{EgressBridgeCommand, sandboxEgressSocket, certificates, path}, args...)
}
