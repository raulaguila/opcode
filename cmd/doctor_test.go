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
			if strings.HasPrefix(text, "Call opcode_health_check with challenge ") {
				challenge = strings.TrimPrefix(text, "Call opcode_health_check with challenge ")
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
