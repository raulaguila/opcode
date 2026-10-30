package message

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/stretchr/testify/require"
)

func historyPageFixture(t *testing.T) (*sql.DB, *service) {
	t.Helper()
	connection, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "history.sqlite"))
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { connection.Close() })
	goose.SetBaseFS(db.FS)
	require.NoError(t, goose.SetDialect("sqlite3"))
	require.NoError(t, goose.Up(connection, "migrations"))
	_, err = connection.Exec(`INSERT INTO sessions(id,title,created_at,updated_at) VALUES('A','History',1,1),('B','Other',1,1)`)
	require.NoError(t, err)
	s := NewService(db.New(connection)).(*service)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return connection, s
}

func insertHistoryMessage(t *testing.T, connection *sql.DB, id, session string, role MessageRole, parts []ContentPart) {
	t.Helper()
	raw, err := marshallParts(parts)
	require.NoError(t, err)
	_, err = connection.Exec(`INSERT INTO messages(id,session_id,role,parts,created_at,updated_at) VALUES(?,?,?,?,1,1)`, id, session, string(role), string(raw))
	require.NoError(t, err)
}

func TestHistoryPageStableInsertionOrderAndDeletedCursor(t *testing.T) {
	connection, store := historyPageFixture(t)
	ctx := context.Background()
	for i := range 11 {
		role := Assistant
		if i%2 == 0 {
			role = User
		}
		// Reverse lexical IDs prove ties use insertion order, not UUID ordering.
		insertHistoryMessage(t, connection, fmt.Sprintf("id-%02d", 11-i), "A", role, []ContentPart{TextContent{Text: fmt.Sprint(i)}})
	}
	insertHistoryMessage(t, connection, "other", "B", User, []ContentPart{TextContent{Text: "other session"}})
	first, err := store.ListPage(ctx, "A", nil, 4)
	require.NoError(t, err)
	require.True(t, first.More)
	require.Equal(t, "8", first.Messages[0].Content().Text, "a normal initial page starts with its human turn")
	require.Equal(t, "10", first.Messages[len(first.Messages)-1].Content().Text)
	cursor := first.Before
	_, err = connection.Exec(`DELETE FROM messages WHERE id=?`, first.Messages[0].ID)
	require.NoError(t, err)
	insertHistoryMessage(t, connection, "later", "A", User, []ContentPart{TextContent{Text: "later"}})
	older := []Message{}
	for first.More {
		first, err = store.ListPage(ctx, "A", cursor, 4)
		require.NoError(t, err)
		older = append(first.Messages, older...)
		cursor = first.Before
	}
	require.Len(t, older, 8, "rows trimmed from the first partial turn remain reachable")
	for i, item := range older {
		require.Equal(t, fmt.Sprint(i), item.Content().Text)
	}
	full, err := store.List(ctx, "A")
	require.NoError(t, err)
	require.Len(t, full, 11)
	require.Equal(t, "later", full[len(full)-1].Content().Text)
}

func TestHistoryPageBoundsCancellationAndMalformedRows(t *testing.T) {
	connection, store := historyPageFixture(t)
	ctx := context.Background()
	for _, limit := range []int{0, MaxHistoryPageMessages + 1} {
		_, err := store.ListPage(ctx, "A", nil, limit)
		require.Error(t, err)
	}
	_, err := store.ListPage(ctx, "B", &HistoryCursor{SessionID: "A", RowID: 1}, 2)
	require.Error(t, err)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.ListPage(canceled, "A", nil, 2)
	require.ErrorIs(t, err, context.Canceled)
	_, err = connection.Exec(`INSERT INTO messages(id,session_id,role,parts,created_at,updated_at) VALUES('bad','A','user','not-json',0,0)`)
	require.NoError(t, err)
	for i := range 3 {
		insertHistoryMessage(t, connection, fmt.Sprint(i), "A", User, []ContentPart{TextContent{Text: "valid"}})
	}
	page, err := store.ListPage(ctx, "A", nil, 2)
	require.NoError(t, err, "old malformed rows must not be decoded during the bounded initial page")
	_, err = store.ListPage(ctx, "A", page.Before, 2)
	require.Error(t, err, "paging to a malformed row must report its error")
}

func TestHistoryPageKeepsAttachmentMetadataAndCompletionWithoutPayloads(t *testing.T) {
	connection, store := historyPageFixture(t)
	data := []byte(strings.Repeat("private-attachment", 1<<17))
	state := &ProviderState{Provider: "openai", Data: json.RawMessage(`{"opaque":"continuation"}`)}
	parts := []ContentPart{TextContent{Text: "attachment evidence"}, BinaryContent{Path: "/image.png", MIMEType: "image/png", Data: data}, Finish{Reason: FinishReasonEndTurn, Time: 42, ProviderState: state}}
	insertHistoryMessage(t, connection, "image", "A", Assistant, parts)
	page, err := store.ListPage(context.Background(), "A", nil, 4)
	require.NoError(t, err)
	assertHistoryPayloadProjection(t, page.Messages[0])
	live := page.Messages[0]
	live.Parts = parts
	live.AppendContent(" live")
	store.PublishUpdate(live)
	page, err = store.ListPage(context.Background(), "A", nil, 4)
	require.NoError(t, err)
	assertHistoryPayloadProjection(t, page.Messages[0])
	require.Contains(t, page.Messages[0].Content().Text, "live")
	full, err := store.List(context.Background(), "A")
	require.NoError(t, err)
	require.Equal(t, data, full[0].BinaryContent()[0].Data)
	require.Equal(t, state, full[0].FinishPart().ProviderState)
}

func assertHistoryPayloadProjection(t *testing.T, item Message) {
	t.Helper()
	require.Equal(t, "/image.png", item.BinaryContent()[0].Path)
	require.Equal(t, "image/png", item.BinaryContent()[0].MIMEType)
	require.Empty(t, item.BinaryContent()[0].Data)
	require.Equal(t, FinishReasonEndTurn, item.FinishPart().Reason)
	require.EqualValues(t, 42, item.FinishPart().Time)
	require.Nil(t, item.FinishPart().ProviderState)
}

func TestHistoryPageAllAssistantSessionRemainsBounded(t *testing.T) {
	connection, store := historyPageFixture(t)
	for i := range 9 {
		insertHistoryMessage(t, connection, fmt.Sprint(i), "A", Assistant, []ContentPart{TextContent{Text: fmt.Sprint(i)}})
	}
	page, err := store.ListPage(context.Background(), "A", nil, 4)
	require.NoError(t, err)
	require.True(t, page.More)
	require.Len(t, page.Messages, 4)
	require.Equal(t, "5", page.Messages[0].Content().Text)
}
