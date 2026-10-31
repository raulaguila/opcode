//go:build integration

package tui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"
)

const proposalDiagnosis = "## DIAGNOSIS: Fluxo de dados\n\nO projeto coleta sensores em SQLite.\n\n1. **Coleta de sensores**\n   O projeto reúne dados de sensores para armazenamento em SQLite, conforme a descrição que foi lida no repositório.\n\n   `README.md:1-2`\n\n2. **Armazenamento**\n   SQLite é o armazenamento descrito no projeto.\n\n   `README.md:1-2`\n\n### Recomendação\n\nRevise o diagnóstico antes de decidir o próximo passo."

// Provider text is deterministic; evidence gates, UI rendering, resizing and
// answer submission still run in the shipped process with its normal services.
func TestPTYMarkdownProposalAndQuestionDivider(t *testing.T) {
	if os.Getenv("OPCODE_LIVE_TESTS") != "1" || runtime.GOOS != "linux" {
		t.Skip("explicit Linux PTY acceptance opt-in required")
	}
	binary := os.Getenv("OPCODE_EVAL_BINARY")
	require.True(t, filepath.IsAbs(binary))
	var generations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded := httptest.NewRecorder()
		conversationProvider(t, &generations, recorded, r)
		oldText, _ := json.Marshal(conversationDiagnosis)
		newText, _ := json.Marshal(proposalDiagnosis)
		w.Header().Set("Content-Type", recorded.Header().Get("Content-Type"))
		w.WriteHeader(recorded.Code)
		_, _ = w.Write(bytes.ReplaceAll(recorded.Body.Bytes(), oldText, newText))
	}))
	defer server.Close()
	home, workspace, data := conversationWorkspace(t, server.URL)
	h := newPTYUIHarnessAtSize(t, binary, home, workspace, 120, 44, "COLORTERM=truecolor", "COLORFGBG=15;0")
	h.wait("initialization", 20*time.Second, func(frame string) bool { return strings.Contains(frame, "Initialize Project") })
	h.send("n")
	h.wait("ready", 5*time.Second, func(frame string) bool {
		return !strings.Contains(frame, "Initialize Project") && ptyUIHasFooter(frame)
	})
	h.paste("Explique o projeto atual e pergunte se quero implementar melhorias.")
	h.send("\r")
	h.wait("readable diagnosis and question card", 20*time.Second, func(frame string) bool {
		return strings.Contains(frame, "DIAGNOSIS: Fluxo de dados") && strings.Contains(frame, "Question for you") && strings.Contains(frame, "Other answer:")
	})
	frame := h.frame()
	require.NotContains(t, frame, "## DIAGNOSIS")
	require.NotContains(t, frame, "### Recomendação")
	require.Contains(t, frame, "README.md:1-2")
	require.Contains(t, frame, "1. Coleta de sensores")
	require.Contains(t, frame, "2. Armazenamento")
	require.EqualValues(t, 2, generations.Load(), "formatting must not trigger citation repair")
	require.Contains(t, frame, "choose or type")
	require.Contains(t, frame, "F2 details")
	lines := strings.Split(frame, "\n")
	for i, line := range lines {
		if strings.Contains(line, "choose or type") {
			require.Greater(t, i, 0)
			require.Contains(t, lines[i-1], strings.Repeat("─", 50), "divider must directly precede controls")
		}
	}
	h.send("\x1b[B\x1b[B")
	previous := ""
	h.wait("explicit choice fully painted", 5*time.Second, func(frame string) bool {
		stable := frame == previous
		previous = frame
		return stable && strings.Contains(frame, "› ● Agora não") && strings.Contains(frame, "Enter sends choice")
	})
	saveProposalPTYFrame(t, h)
	h.resize(60, 20)
	h.wait("compact card keeps selected answer", 5*time.Second, func(frame string) bool {
		return strings.Contains(frame, "Question for you") && strings.Contains(frame, "› ● Agora não")
	})
	h.send("\r")
	h.resize(120, 44)
	h.wait("question answered without repair", 15*time.Second, func(frame string) bool { return questionExchangeCompleted(frame, "Agora não") })
	require.EqualValues(t, 3, generations.Load())
	require.Equal(t, []string{"Agora não"}, persistedQuestionResponses(t, data))
	conversationQuit(t, h, true)
}

func saveProposalPTYFrame(t *testing.T, h *ptyUIHarness) {
	t.Helper()
	h.mu.Lock()
	width, height := h.screen.Size()
	cells := make([][]vt10x.Glyph, height)
	for y := range height {
		cells[y] = make([]vt10x.Glyph, width)
		for x := range width {
			cells[y][x] = h.screen.Cell(x, y)
		}
	}
	h.mu.Unlock()
	data, err := json.Marshal(cells)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(os.TempDir(), "opcode-proposal-pty-frame.json"), data, 0600))
}
