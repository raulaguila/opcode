package desktop

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestHandlerRefusesForeignHostOriginAndMissingKey(t *testing.T) {
	s := &Server{key: "secret", host: "127.0.0.1:4000", feeds: map[chan string]struct{}{}, failures: map[string]string{}}
	call := func(host, origin, key, path string) int {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if key != "" {
			r.Header.Set("X-Opcode-Key", key)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	require.Equal(t, http.StatusMisdirectedRequest, call("evil.example:4000", "", "secret", "/api/state"), "a rebound host name is refused")
	require.Equal(t, http.StatusForbidden, call("127.0.0.1:4000", "http://evil.example", "secret", "/api/state"), "another origin is refused")
	require.Equal(t, http.StatusUnauthorized, call("127.0.0.1:4000", "", "", "/api/state"), "the API needs the key")
	require.Equal(t, http.StatusUnauthorized, call("127.0.0.1:4000", "", "wrong", "/api/state"))
	require.Equal(t, http.StatusOK, call("127.0.0.1:4000", "", "", "/"), "the page itself carries no data")
}

func TestProjectFoldsToolResultsAndRendersRepliesWithoutRawHTML(t *testing.T) {
	messages := []message.Message{
		{ID: "u", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "Adicione Remove"}}},
		{ID: "a1", Role: message.Assistant, Parts: []message.ContentPart{
			message.ToolCall{ID: "c1", Name: "view", Input: `{"file_path":"/w/store.go"}`, Finished: true},
			message.ToolCall{ID: "c2", Name: "edit", Input: `{"file_path":"/w/store.go"}`, Finished: true},
			message.ToolCall{ID: "c3", Name: "bash", Input: `{"command":"go  test ./..."}`, Finished: true},
		}},
		{ID: "r", Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "c1"}, message.ToolResult{ToolCallID: "c2", IsError: true}}},
		{ID: "a2", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "**feito** <script>x()</script>"}}},
	}
	out := project(messages)
	require.Len(t, out, 3)
	require.Equal(t, []Step{{"view", "store.go", "ok"}, {"edit", "store.go", "failed"}, {"bash", "go test ./...", "running"}}, out[1].Steps)
	require.Contains(t, out[2].HTML, "<strong>feito</strong>")
	require.NotContains(t, out[2].HTML, "<script>")
}
