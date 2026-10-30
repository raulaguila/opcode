//go:build integration

package tui

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
)

func TestPTYQuestionCustomRowAndSuggestedChoiceSubmitOnlyActiveAnswer(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" {
		t.Skip("explicit PTY acceptance opt-in required")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Linux PTY acceptance")
	}
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary))
	for _, suggested := range []bool{false, true} {
		name := "custom row"
		if suggested {
			name = "suggested choice ignores inactive custom draft"
		}
		t.Run(name, func(t *testing.T) { testPTYQuestionChoice(t, binary, suggested) })
	}
}

func testPTYQuestionChoice(t *testing.T, binary string, suggested bool) {
	var generations, requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			requests.Add(1)
		}
		conversationProvider(t, &generations, w, r)
	}))
	defer server.Close()
	home, workspace, data := conversationWorkspace(t, server.URL)
	h := newPTYUIHarnessAtSize(t, binary, home, workspace, 160, 44)
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("ready", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.paste("Explique o projeto atual e pergunte se quero implementar as melhorias.")
	h.send("\r")
	h.wait("question with visible custom answer row", 20*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Question for you") && strings.Contains(frame, "Other answer:") && strings.Contains(frame, "DIAGNOSIS:")
	})
	h.send("\x1b[A") // Up from no selection chooses the final, custom-answer row.
	h.wait("custom answer selected by keyboard", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "› ● Other answer:") && strings.Contains(frame, "Enter sends your text")
	})
	h.send("\r")
	h.wait("empty custom answer rejected without submitting", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Question for you") && strings.Contains(frame, "answer must contain")
	})
	require.EqualValues(t, 2, generations.Load(), "empty answers must not resume model generation")
	require.Empty(t, persistedQuestionResponses(t, data))
	want := "Agora não; preciso revisar primeiro."
	h.paste(want)
	h.wait("typed custom answer remains active", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, want) && strings.Contains(frame, "› ● Other answer:")
	})
	if suggested {
		h.send("\x1b[A")
		h.wait("suggested choice selected with custom draft retained", 5*time.Second, func(frame string) bool {
			return strings.Contains(frame, "› ● Agora não") && strings.Contains(frame, want) && strings.Contains(frame, "Enter sends choice")
		})
		want = "Agora não"
	}
	h.send("\r")
	h.wait("original question and selected answer retained", 15*time.Second, func(frame string) bool {
		return questionExchangeCompleted(frame, want)
	})
	require.Equal(t, []string{want}, persistedQuestionResponses(t, data), "the stored tool result must match only the active answer row")
	require.EqualValues(t, 3, generations.Load(), "selection must not cause repair or additional model requests")
	assertQuestionExchangeOrder(t, h.frame())
	conversationQuit(t, h, true)
	before := requests.Load()
	h = newPTYUIHarnessWithArgs(t, binary, home, workspace, 160, 44, []string{"--resume"})
	h.wait("question and answer survive history reload", 15*time.Second, func(frame string) bool {
		return questionExchangeCompleted(frame, want)
	})
	assertQuestionExchangeOrder(t, h.frame())
	require.Equal(t, before, requests.Load(), "restoring the question exchange must not call the model")
	require.Equal(t, []string{want}, persistedQuestionResponses(t, data))
	h.assertComposer()
	conversationQuit(t, h, true)
}

func questionExchangeCompleted(frame, answer string) bool {
	return strings.Contains(frame, "DIAGNOSIS:") && strings.Contains(frame, "Agent question") && strings.Contains(frame, "Quer implementar as melhorias?") && strings.Contains(frame, "You answered: "+answer) && strings.Contains(frame, "Perfeito") && !strings.Contains(frame, "Question for you") && ptyUIIdleFooter(frame)
}

func assertQuestionExchangeOrder(t *testing.T, frame string) {
	t.Helper()
	previous := -1
	for _, text := range []string{"DIAGNOSIS:", "Agent question", "Quer implementar as melhorias?", "You answered:", "Perfeito"} {
		index := strings.Index(frame, text)
		require.Greater(t, index, previous, "question exchange order must remain intact: %s\n%s", text, frame)
		previous = index
	}
	require.GreaterOrEqual(t, strings.Count(frame, "› Process"), 2)
	require.NotContains(t, frame, "Citations not validated")
}

func persistedQuestionResponses(t *testing.T, data string) []string {
	t.Helper()
	address := (&url.URL{Scheme: "file", Path: filepath.Join(data, "opcode.db")}).String() + "?mode=ro"
	conn, err := sql.Open("sqlite3", address)
	require.NoError(t, err)
	defer conn.Close()
	var responses []string
	for _, item := range ptyUIMessages(conn) {
		if item.Role != "tool" {
			continue
		}
		for _, part := range item.Parts {
			if part.Type != "tool_result" {
				continue
			}
			var result message.ToolResult
			require.NoError(t, json.Unmarshal(part.Data, &result))
			var answer struct {
				Response string `json:"user_response"`
			}
			if json.Unmarshal([]byte(result.Content), &answer) == nil && answer.Response != "" {
				responses = append(responses, answer.Response)
			}
		}
	}
	return responses
}
