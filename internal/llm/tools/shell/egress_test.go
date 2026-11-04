package shell

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if IsEgressBridge(os.Args) {
		RunEgressBridge(os.Args)
	}
	os.Exit(m.Run())
}

func TestDomainAllowedMatchesWholeNames(t *testing.T) {
	allowed := []string{"pypi.org", "*.corp.example"}
	for host, want := range map[string]bool{
		"pypi.org": true, "PyPI.org.": true, "notpypi.org": false, "pypi.org.evil.example": false,
		"nexus.corp.example": true, "a.b.corp.example": true, "corp.example": false, "evilcorp.example": false, "": false,
	} {
		require.Equal(t, want, domainAllowed(allowed, host), host)
	}
	require.False(t, domainAllowed([]string{"*"}, "pypi.org"), "nothing allows every host")
	require.False(t, domainAllowed([]string{"*."}, "pypi.org"))
}

func direct(*url.URL) (*url.URL, error) { return nil, nil }

func TestEgressProxyAnswersOnlyForAllowedDomains(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "package") }))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	proxy := httptest.NewServer(newEgressProxy(func() []string { return []string{target.Hostname()} }, direct))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	client := http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

	response, err := client.Get(upstream.URL)
	require.NoError(t, err)
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	require.Equal(t, "package", string(body))

	response, err = client.Get("http://blocked.example/")
	require.NoError(t, err)
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	require.Equal(t, http.StatusForbidden, response.StatusCode)
	require.Contains(t, string(body), "security.allowedDomains")

	conn, err := net.Dial("tcp", proxyURL.Host)
	require.NoError(t, err)
	defer conn.Close()
	fmt.Fprint(conn, "CONNECT blocked.example:443 HTTP/1.1\r\nHost: blocked.example:443\r\n\r\n")
	refused, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, refused.StatusCode)
}

func TestEgressProxyChainsThroughTheConfiguredProxy(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	asked := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadString('\n')
		asked <- strings.TrimSpace(line)
		fmt.Fprint(conn, "HTTP/1.1 403 Blocked\r\n\r\n")
	}()
	corporate := &url.URL{Scheme: "http", Host: listener.Addr().String()}
	proxy := newEgressProxy(func() []string { return []string{"pypi.org"} }, func(*url.URL) (*url.URL, error) { return corporate, nil })

	_, err = proxy.dial(context.Background(), "pypi.org:443")
	require.ErrorContains(t, err, "403 Blocked", "the corporate refusal reaches the command")
	require.Equal(t, "CONNECT pypi.org:443 HTTP/1.1", <-asked)
}

func TestSandboxCommandStartsThroughTheBridgeWithoutNetwork(t *testing.T) {
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(bin+"/bwrap", []byte("#!/bin/sh\nexit 0\n"), 0700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	cache, err := prepareCommandCache(root)
	require.NoError(t, err)

	command, err := sandboxCommand(context.Background(), root, "/bin/sh", []string{"-c", "true"}, nil, false, false, false, cache)
	require.NoError(t, err)
	require.NotContains(t, command.Args, "--share-net")
	require.Contains(t, command.Args, sandboxEgressSocket)
	require.Contains(t, command.Args, sandboxEgressBridge)
	require.Contains(t, command.Args, EgressBridgeCommand)

	command, err = sandboxCommand(WithApprovedNetwork(context.Background()), root, "/bin/sh", []string{"-c", "true"}, nil, false, false, false, cache)
	require.NoError(t, err)
	require.NotContains(t, command.Args, EgressBridgeCommand, "a network grant needs no bridge")

	command, err = sandboxCommand(context.Background(), root, "/bin/sh", []string{"-c", "true"}, &config.Config{Security: config.SecurityConfig{AllowedDomains: []string{}}}, false, false, false, cache)
	require.NoError(t, err)
	require.NotContains(t, command.Args, EgressBridgeCommand, "an empty list turns it off")
}

// Real bubblewrap: the command has no route out, yet reaches an allowed host.
func TestSandboxReachesAnAllowedHostOnlyThroughTheProxy(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bubblewrap unavailable")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "from-the-registry") }))
	defer server.Close()
	cfg, err := config.LoadOffline(t.TempDir())
	require.NoError(t, err)
	old := cfg.Security
	t.Cleanup(func() { cfg.Security = old })
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	cfg.Security.Sandbox, cfg.Security.AllowedDomains = "bubblewrap", []string{target.Hostname()}
	root := t.TempDir()

	script := fmt.Sprintf(`port=${HTTP_PROXY##*:}
exec 3<>/dev/tcp/127.0.0.1/$port || exit 7
printf 'GET %[1]s/ HTTP/1.0\r\nHost: %[2]s\r\n\r\n' >&3
cat <&3
exec 4<>/dev/tcp/127.0.0.1/$port
printf 'GET http://blocked.example/ HTTP/1.0\r\n\r\n' >&4
cat <&4
(exec 5<>/dev/tcp/%[3]s/%[4]s) 2>/dev/null && echo DIRECT-ROUTE-OPEN
true`, server.URL, target.Host, target.Hostname(), target.Port())
	command, err := commandProcess(WithCommandCache(context.Background()), root, script, false, false, false)
	require.NoError(t, err)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "from-the-registry")
	require.Contains(t, string(output), "blocked.example is not in security.allowedDomains")
	require.NotContains(t, string(output), "DIRECT-ROUTE-OPEN", "the proxy is the only way out")
}

func TestRegistriesReachTheirMirrorThroughOneSetting(t *testing.T) {
	t.Setenv("GOMODCACHE", t.TempDir())
	cfg := &config.Config{Security: config.SecurityConfig{AllowedDomains: []string{}, Registries: map[string]string{
		"go": "https://nexus.corp.example/go", "npm": "https://nexus.corp.example/npm/", "pip": "https://pypi.corp.example/simple",
	}}}
	require.ElementsMatch(t, []string{"nexus.corp.example", "nexus.corp.example", "pypi.corp.example"}, egressDomains(cfg))
	require.ElementsMatch(t, []string{"NPM_CONFIG_REGISTRY=https://nexus.corp.example/npm/", "PIP_INDEX_URL=https://pypi.corp.example/simple"}, registryEnvironment(cfg))
	require.Equal(t, []string{"GOPROXY=https://nexus.corp.example/go", "GOSUMDB=off"}, goProxyEnvironment(cfg, true))
	require.Nil(t, goProxyEnvironment(&config.Config{}, true), "nothing cached and no mirror leaves Go alone")
}

func TestEgressDomainsDefaultsKeepTheirListWhenAMirrorIsAdded(t *testing.T) {
	cfg := &config.Config{Security: config.SecurityConfig{Registries: map[string]string{"npm": "https://nexus.corp.example/npm/"}}}
	require.Contains(t, egressDomains(cfg), "pypi.org")
	require.Contains(t, egressDomains(cfg), "nexus.corp.example")
	require.NotContains(t, defaultAllowedDomains, "nexus.corp.example", "the default list stays untouched")
	require.Equal(t, []string{"pypi.org"}, EgressDomains(&config.Config{Security: config.SecurityConfig{AllowedDomains: []string{"pypi.org", "*.corp.example"}}}))
}
