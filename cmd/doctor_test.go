package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func doctorFixture(t *testing.T, data string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".opcode.json"), []byte(data), 0600))
	return dir
}

// A fresh process reflects CLI lifetime and avoids replacing a live config singleton.
func doctorProcess(t *testing.T, dir string, canceled bool, args ...string) (string, error) {
	t.Helper()
	encoded, _ := json.Marshal(append([]string{"--cwd", dir, "--json"}, args...))
	command := exec.Command(os.Args[0], "-test.run=^TestDoctorSubprocess$")
	command.Env = append(os.Environ(), "OPCODE_DOCTOR_TEST_CONFIG="+filepath.Join(dir, ".opcode.json"), "OPCODE_DOCTOR_TEST_ARGS="+string(encoded))
	if canceled {
		command.Env = append(command.Env, "OPCODE_DOCTOR_TEST_CANCEL=1")
	}
	output, err := command.CombinedOutput()
	return string(output), err
}
func TestDoctorSubprocess(t *testing.T) {
	path := os.Getenv("OPCODE_DOCTOR_TEST_CONFIG")
	if path == "" {
		return
	}
	viper.SetConfigFile(path)
	var args []string
	if json.Unmarshal([]byte(os.Getenv("OPCODE_DOCTOR_TEST_ARGS")), &args) != nil {
		os.Exit(3)
	}
	command := newDoctorCommand()
	command.SetOut(os.Stdout)
	command.SetErr(os.Stderr)
	command.SetArgs(args)
	if os.Getenv("OPCODE_DOCTOR_TEST_CANCEL") == "1" {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		command.SetContext(ctx)
	}
	if command.Execute() != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
func TestDoctorWorksWithoutCoderAndNeverStartsModel(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "no request expected", 500)
	}))
	defer server.Close()
	dir := doctorFixture(t, `{"providers":{"local":{"baseURL":"`+server.URL+`/v1"}}}`)
	output, _ := doctorProcess(t, dir, false)
	require.Zero(t, requests.Load())
	require.Contains(t, output, `"configuration"`)
	require.Contains(t, output, `"sandbox"`)
	require.NotContains(t, output, "agent coder not found")
}

func TestDoctorLanguageSetupIsOfflineAndDoesNotStartServers(t *testing.T) {
	dir := doctorFixture(t, `{"lsp":{"ruby":{"command":"ruby-lsp"}},"checks":{"test":{"command":"touch check-was-run"}}}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ledger.rb"), []byte("class Ledger\nend\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Gemfile"), []byte("source 'https://example.invalid'\n"), 0600))
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "ruby-lsp"), []byte("#!/bin/sh\ntouch '"+filepath.Join(dir, "server-was-run")+"'\n"), 0700))
	t.Setenv("PATH", bin)
	output, _ := doctorProcess(t, dir, false, "--languages")
	var report doctorReport
	start := strings.Index(output, "{")
	require.GreaterOrEqual(t, start, 0, output)
	require.NoError(t, json.NewDecoder(strings.NewReader(output[start:])).Decode(&report), output)
	require.NotNil(t, report.Languages)
	require.True(t, report.Languages.ProjectInspected)
	var found bool
	for _, entry := range report.Languages.Languages {
		if entry.Language != "ruby" {
			continue
		}
		found = true
		require.True(t, entry.LocalParser)
		require.Equal(t, 2, entry.ProjectFiles)
		require.Len(t, entry.ConfiguredServers, 1)
		require.True(t, entry.ConfiguredServers[0].ExecutableAvailable)
		require.False(t, entry.ConfiguredServers[0].StartedByInventory)
		require.Equal(t, []string{"test"}, entry.ConfiguredChecks)
	}
	require.True(t, found, output)
	for _, marker := range []string{"server-was-run", "check-was-run"} {
		_, err := os.Stat(filepath.Join(dir, marker))
		require.True(t, os.IsNotExist(err), marker)
	}
}
func TestDoctorConfigurationErrorsDoNotEchoLiteralCredentials(t *testing.T) {
	dir := doctorFixture(t, `{"providers":{"unknown-credential-do-not-echo":{"apiKey":"literal-private-credential-do-not-echo"}}}`)
	output, err := doctorProcess(t, dir, false)
	require.Error(t, err)
	require.NotContains(t, output, "literal-private-credential-do-not-echo")
	require.NotContains(t, output, "unknown-credential-do-not-echo")
}
func TestDoctorProbeRequiresExplicitModel(t *testing.T) {
	dir := doctorFixture(t, `{}`)
	output, err := doctorProcess(t, dir, false, "--probe-model")
	require.Error(t, err)
	require.Contains(t, output, "requires --model")
}
func TestModelProbeChecksRealStructuredChallengeWithoutExecuting(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		challenge := ""
		for _, m := range body.Messages {
			var text string
			if json.Unmarshal(m.Content, &text) != nil {
				var parts []struct{ Text string }
				require.NoError(t, json.Unmarshal(m.Content, &parts))
				for _, part := range parts {
					text += part.Text
				}
			}
			if after, ok := strings.CutPrefix(text, "Call opcode_health_check with challenge "); ok {
				challenge = after
			}
		}
		args, _ := json.Marshal(map[string]string{"challenge": challenge})
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "probe", "type": "function", "function": map[string]any{"name": "opcode_health_check", "arguments": string(args)}}}}}}}))
	}))
	defer server.Close()
	dir := doctorFixture(t, `{"providers":{"local":{"baseURL":"`+server.URL+`/v1"}},"models":{"probe.local":{"provider":"local","api_model":"probe","context_window":4096,"default_max_tokens":256}}}`)
	output, _ := doctorProcess(t, dir, false, "--probe-model", "--model", "probe.local")
	require.Contains(t, output, "The model returned the requested tool name", output)
	require.EqualValues(t, 1, requests.Load())
	output, err := doctorProcess(t, dir, true, "--probe-model", "--model", "probe.local")
	require.Error(t, err)
	require.NotContains(t, output, "The model returned the requested tool name")
	require.EqualValues(t, 1, requests.Load())
}
