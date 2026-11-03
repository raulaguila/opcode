//go:build linux

package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/tools/shell"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/stretchr/testify/require"
)

func networkFixture(t *testing.T) string {
	t.Helper()
	root := workspaceTestDir(t)
	cfg := config.Get()
	old := *cfg
	cfg.WorkingDir = root
	cfg.Security = config.SecurityConfig{Sandbox: "bubblewrap"}
	t.Cleanup(func() { *cfg = old })
	return root
}

func TestCommandGoTestsReuseDownloadedModulesOffline(t *testing.T) {
	root := networkFixture(t)
	config.Get().Security = config.SecurityConfig{Sandbox: "none", EnvAllow: []string{"GOPROXY", "GOSUMDB"}}
	module := "module example.com/opcodefixture\n\ngo 1.23.0\n"
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	for name, contents := range map[string]string{"go.mod": module, "value.go": "package opcodefixture\nfunc Value() int { return 42 }\n"} {
		file, err := w.Create("example.com/opcodefixture@v1.0.0/" + name)
		require.NoError(t, err)
		_, err = fmt.Fprint(file, contents)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	files := map[string][]byte{"v1.0.0.mod": []byte(module), "v1.0.0.info": []byte(`{"Version":"v1.0.0","Time":"2026-01-01T00:00:00Z"}`), "v1.0.0.zip": archive.Bytes()}
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		data, ok := files[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(proxy.Close)
	t.Setenv("GOPROXY", proxy.URL)
	t.Setenv("GOSUMDB", "off")
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.23.0\n\nrequire example.com/opcodefixture v1.0.0\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "value_test.go"), []byte("package fixture\nimport (\"testing\"; dep \"example.com/opcodefixture\")\nfunc TestValue(t *testing.T) { if dep.Value() != 42 { t.Fatal(\"wrong value\") } }\n"), 0600))
	p := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	tool := NewBashTool(p)
	t.Cleanup(func() { _ = tool.(interface{ Close() error }).Close() })
	params := BashParams{Command: "go test -mod=mod -count=1 ./...", Timeout: 120000}
	response, err := tool.Run(toolContext(), toolInput(params))
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.Positive(t, requests.Load(), "the first command must download the fixture module")
	proxy.Close()
	t.Setenv("GOPROXY", "off")
	response, err = tool.Run(toolContext(), toolInput(params))
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.Contains(t, response.Content, "fixture")
}

func TestCommandNetworkDenialDoesNotStartOrCreateCache(t *testing.T) {
	root := networkFixture(t)
	p := &denyingPermissions{allow: func(r permission.CreatePermissionRequest) bool { return r.Action == "execute" }}
	tool := NewBashTool(p)
	t.Cleanup(func() { _ = tool.(interface{ Close() error }).Close() })
	_, err := tool.Run(toolContext(), toolInput(BashParams{Command: "touch denied-marker", Network: true}))
	require.ErrorIs(t, err, permission.ErrorPermissionDenied)
	require.Len(t, p.requests, 2)
	require.Equal(t, "network", p.requests[1].Action)
	require.True(t, p.requests[1].Params.(BashPermissionsParams).Network)
	for _, path := range []string{"denied-marker", ".opcode"} {
		_, err := os.Stat(filepath.Join(root, path))
		require.True(t, os.IsNotExist(err))
	}
}

func TestCommandNetworkGrantIsScopedAndChecksForwardIt(t *testing.T) {
	networkFixture(t)
	// This fixture prints the actual sandbox argv; it never executes the command.
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "bwrap"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	p := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	tool := NewBashTool(p)
	t.Cleanup(func() { _ = tool.(interface{ Close() error }).Close() })
	response, err := tool.Run(toolContext(), toolInput(BashParams{Command: "go test ./...", Network: true}))
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Contains(t, response.Content, "--share-net")
	require.Contains(t, response.Content, "--unshare-all")
	var metadata BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Metadata), &metadata))
	require.True(t, metadata.NetworkEnabled)
	require.Len(t, p.requests, 2)
	require.Equal(t, p.requests[0].Params, p.requests[1].Params)
	require.NotEqual(t, p.requests[0].Action, p.requests[1].Action)
	require.False(t, config.Get().Security.AllowNetwork)
	response, err = tool.Run(toolContext(), toolInput(BashParams{Command: "go test ./..."}))
	require.NoError(t, err)
	require.NotContains(t, response.Content, "--share-net")
	require.Len(t, p.requests, 3, "the ordinary second command must not inherit the grant")
	config.Get().Checks = map[string]config.CheckConfig{"unit": {Command: "go test ./..."}}
	checks := NewChecksTool(p)
	t.Cleanup(func() { _ = checks.(interface{ Close() error }).Close() })
	response, err = checks.Run(toolContext(), toolInput(map[string]any{"names": []string{"unit"}, "network": true}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "--share-net")
	require.Len(t, p.requests, 5)
	require.Equal(t, "network", p.requests[4].Action)
}

func TestCommandNetworkHintRequiresFailedDownloadAndLiveContext(t *testing.T) {
	networkFixture(t)
	output := "go: downloading example.com/module v1.0.0: dial tcp: network is unreachable"
	require.Contains(t, dependencyNetworkHint(toolContext(), 1, output), "network=true")
	require.Empty(t, dependencyNetworkHint(toolContext(), 0, output))
	require.Empty(t, dependencyNetworkHint(toolContext(), 1, "test assertion failed"))
	require.Empty(t, dependencyNetworkHint(shell.WithApprovedNetwork(toolContext()), 1, output))
	ctx, cancel := context.WithCancel(toolContext())
	cancel()
	require.Empty(t, dependencyNetworkHint(ctx, 1, output))
}

func TestCommandCachePersistsAcrossSeparateApprovedCommands(t *testing.T) {
	root := networkFixture(t)
	config.Get().Security.Sandbox = "none"
	p := &denyingPermissions{allow: func(permission.CreatePermissionRequest) bool { return true }}
	tool := NewBashTool(p)
	t.Cleanup(func() { _ = tool.(interface{ Close() error }).Close() })
	response, err := tool.Run(toolContext(), toolInput(BashParams{Command: `mkdir -p "$GOCACHE" "$GOMODCACHE" "$GOPATH/pkg/sumdb"; printf module > "$GOMODCACHE/fixture"; printf build > "$GOCACHE/fixture"; printf checksum > "$GOPATH/pkg/sumdb/fixture"`}))
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	response, err = tool.Run(toolContext(), toolInput(BashParams{Command: `cat "$GOMODCACHE/fixture" "$GOCACHE/fixture" "$GOPATH/pkg/sumdb/fixture"; printf '\n%s' "$HOME"`}))
	require.NoError(t, err)
	require.Contains(t, response.Content, "modulebuildchecksum")
	require.Contains(t, response.Content, os.DevNull)
	_, err = os.Stat(filepath.Join(root, ".opcode", "command-cache", "go", "path", "pkg", "mod", "fixture"))
	require.NoError(t, err)
}
