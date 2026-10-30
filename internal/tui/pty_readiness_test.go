//go:build integration

package tui

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPTYClearDraftRequiresEmptyComposerInsteadOfDraftMarker(t *testing.T) {
	footer := " Ask ▾ / commands · Alt+Enter new line  llama3.1:8b ▾ "
	for _, tc := range []struct {
		name, body string
		empty      bool
	}{
		{"empty", ">\n\n\n", true},
		{"arbitrary draft", "> UNSENT_DRAFT ação\n\n\n", false},
		{"first row cleared but more rows remain", ">\n  última linha 漢字\n\n", false},
		{"old marker removed but draft remains", "> ft-01\n\n\n", false},
		{"transcript marker outside empty composer", "┃ draft-01 is an earlier message\n──────\n>\n\n\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.empty, ptyUIComposerEmpty(tc.body+footer)) })
	}
	require.False(t, ptyUIComposerEmpty(">\n\n"), "a pre-initialization frame is not ready")
}

func TestPTYCancelConnectionRequiresClosedConfirmation(t *testing.T) {
	list := "Connections and preferences\nNavigation fixture"
	require.False(t, testLiveOllamaPTYUICancelPreservesConnection(list+"\nDelete connection?\n[Cancel] [Delete]"))
	require.True(t, testLiveOllamaPTYUICancelPreservesConnection(list))
	require.False(t, testLiveOllamaPTYUICancelPreservesConnection("Connections and preferences"))
}

func TestPTYSteeredFinalRequiresPersistedOrderAndIdleUI(t *testing.T) {
	conn, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	_, err = conn.Exec("CREATE TABLE messages (role TEXT, parts TEXT)")
	require.NoError(t, err)
	for _, row := range []struct{ role, parts string }{
		{"tool", `[{"type":"tool_result","data":{"content":"LIVE_INPUT:HUMAN_PTY_OK"}}]`},
		{"user", `[{"type":"text","data":{"text":"Nova instrução: LIVE_STEERING_OK"}}]`},
		{"assistant", `[{"type":"text","data":{"text":"LIVE_STEERING_OK"}},{"type":"finish","data":{"reason":"end_turn"}}]`},
	} {
		_, err = conn.Exec("INSERT INTO messages(role,parts) VALUES(?,?)", row.role, row.parts)
		require.NoError(t, err)
	}
	idle := "Ask ▾ / commands · Alt+Enter new line  llama3.1:8b ▾"
	require.False(t, testLiveOllamaPTYUISteeredResponseAndPersistedToolResult(&conn, strings.Replace(idle, "/ commands", "Checking answer and sources · Esc cancels", 1)), "SQL finish precedes validation and UI completion")
	require.False(t, testLiveOllamaPTYUISteeredResponseAndPersistedToolResult(&conn, ""), "a missing footer is not ready")
	require.True(t, testLiveOllamaPTYUISteeredResponseAndPersistedToolResult(&conn, idle))
	_, err = conn.Exec("DELETE FROM messages WHERE role='tool'")
	require.NoError(t, err)
	require.False(t, testLiveOllamaPTYUISteeredResponseAndPersistedToolResult(&conn, idle), "the original persisted tool/instruction/final ordering remains mandatory")
}
