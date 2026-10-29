package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func watchFixture(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "observer.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE sessions(id TEXT PRIMARY KEY,title TEXT,updated_at INTEGER);CREATE TABLE messages(id TEXT PRIMARY KEY,session_id TEXT,role TEXT,parts TEXT,created_at INTEGER,finished_at INTEGER);CREATE TABLE agent_workflows(session_id TEXT,state TEXT);INSERT INTO sessions VALUES ('one','A project',1);INSERT INTO sessions VALUES ('two','Second project',2);INSERT INTO agent_workflows VALUES ('one','{"status":"planning","mode":"ask"}')`)
	require.NoError(t, err)
	return path, db
}
func insertWatchText(t *testing.T, db *sql.DB, id, role, text, origin string, finished any) {
	t.Helper()
	raw, _ := json.Marshal([]any{map[string]any{"type": "text", "data": map[string]any{"text": text, "origin": origin}}})
	_, err := db.Exec(`INSERT INTO messages VALUES (?,?,?,?,?,?)`, id, "one", role, string(raw), time.Now().UnixNano(), finished)
	require.NoError(t, err)
}
func TestWatchNeverCreatesOrWritesDatabase(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.db")
	_, err := openWatchDatabase(context.Background(), missing)
	require.Error(t, err)
	require.NoFileExists(t, missing)
	path, writer := watchFixture(t)
	ro, err := openWatchDatabase(context.Background(), path)
	require.NoError(t, err)
	defer ro.Close()
	var queryOnly int
	require.NoError(t, ro.QueryRow(`PRAGMA query_only`).Scan(&queryOnly))
	require.Equal(t, 1, queryOnly)
	_, err = ro.Exec(`UPDATE sessions SET title='changed'`)
	require.Error(t, err)
	_, err = ro.Exec(`CREATE TABLE forbidden(id TEXT)`)
	require.Error(t, err)
	var title string
	require.NoError(t, writer.QueryRow(`SELECT title FROM sessions WHERE id='one'`).Scan(&title))
	require.Equal(t, "A project", title)
}
func TestWatchProjectionIsBoundedRedactedAndControlSafe(t *testing.T) {
	t.Setenv("OPCODE_TEST_API_KEY", "observer-private-credential")
	path, writer := watchFixture(t)
	insertWatchText(t, writer, "user", "user", "Explain \x1b[31mhello\x1b[0m observer-private-credential\x00\u202e", "", 1)
	insertWatchText(t, writer, "private", "user", "internal repair", "application", 1)
	insertWatchText(t, writer, "partial", "assistant", "not settled", "", nil)
	insertWatchText(t, writer, "large", "assistant", strings.Repeat("x", watchTextLimit+10), "", 1)
	raw := `[{"type":"tool_call","data":{"name":"bash","input":"hidden parameters"}},{"type":"reasoning","data":{"text":"private reasoning"}},{"type":"text","data":{"text":"final answer"}}]`
	_, err := writer.Exec(`INSERT INTO messages VALUES ('final','one','assistant',?,99,1)`, raw)
	require.NoError(t, err)
	ro, err := openWatchDatabase(context.Background(), path)
	require.NoError(t, err)
	defer ro.Close()
	snapshot, err := readWatchSnapshot(context.Background(), ro, watchOptions{Session: "one", Limit: 20})
	require.NoError(t, err)
	require.Equal(t, "planning", snapshot.State)
	require.Equal(t, "ask", snapshot.Mode)
	data, _ := json.Marshal(snapshot)
	text := string(data)
	for _, unwanted := range []string{"observer-private-credential", "internal repair", "not settled", "hidden parameters", "private reasoning", "\\u001b"} {
		require.NotContains(t, text, unwanted)
	}
	require.Contains(t, text, "[REDACTED]")
	require.Contains(t, text, `\\u0000`)
	require.Contains(t, text, `\\u202e`)
	require.Contains(t, text, "final answer")
	var large watchMessage
	for _, m := range snapshot.Messages {
		if m.ID == "large" {
			large = m
		}
	}
	require.True(t, large.Truncated)
	require.LessOrEqual(t, len(large.Text), watchTextLimit)
	require.LessOrEqual(t, len(data), 256*1024)
}
func TestWatchPaginationAndOversizedParts(t *testing.T) {
	path, writer := watchFixture(t)
	for i := 0; i < 5; i++ {
		insertWatchText(t, writer, string(rune('a'+i)), "user", "message", "", 1)
	}
	_, err := writer.Exec(`INSERT INTO messages VALUES ('oversized','one','assistant',?,999999999999999999,1)`, strings.Repeat("x", watchPartsLimit+1))
	require.NoError(t, err)
	ro, err := openWatchDatabase(context.Background(), path)
	require.NoError(t, err)
	defer ro.Close()
	first, err := readWatchSnapshot(context.Background(), ro, watchOptions{Session: "one", Limit: 2})
	require.NoError(t, err)
	require.Len(t, first.Messages, 2)
	require.NotNil(t, first.NextOffset)
	require.Equal(t, 2, *first.NextOffset)
	second, err := readWatchSnapshot(context.Background(), ro, watchOptions{Session: "one", Limit: 2, Offset: *first.NextOffset})
	require.NoError(t, err)
	require.NotEqual(t, first.Messages[0].ID, second.Messages[0].ID)
	all, err := readWatchSnapshot(context.Background(), ro, watchOptions{Session: "one", Limit: 20})
	require.NoError(t, err)
	found := false
	for _, m := range all.Messages {
		if m.ID == "oversized" {
			found = true
			require.True(t, m.Truncated)
			require.Contains(t, m.Text, "exceeds observer limit")
		}
	}
	require.True(t, found)
	sessions, err := readWatchSnapshot(context.Background(), ro, watchOptions{Limit: 1})
	require.NoError(t, err)
	require.Len(t, sessions.Sessions, 1)
	require.Equal(t, "two", sessions.Sessions[0].ID)
	require.NotNil(t, sessions.NextOffset)
}

type watchBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *watchBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *watchBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buf.String() }
func TestWatchFollowPublishesOnlyChangesAndCancels(t *testing.T) {
	path, writer := watchFixture(t)
	ro, err := openWatchDatabase(context.Background(), path)
	require.NoError(t, err)
	defer ro.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &watchBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- streamWatch(ctx, ro, watchOptions{Session: "one", Limit: 20, Format: "json", Follow: true}, out, 5*time.Millisecond)
	}()
	require.Eventually(t, func() bool { return strings.Count(out.String(), "\n") == 1 }, time.Second, time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 1, strings.Count(out.String(), "\n"))
	insertWatchText(t, writer, "new", "user", "second snapshot", "", 1)
	require.Eventually(t, func() bool { return strings.Count(out.String(), "\n") == 2 }, time.Second, time.Millisecond)
	cancel()
	select {
	case err = <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("follow did not cancel")
	}
}
func TestWatchCommandDoesNotInitializeConfiguration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	path, _ := watchFixture(t)
	c := newWatchCommand()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs([]string{"--database", path, "--format", "json", "--limit", "1"})
	require.NoError(t, c.Execute())
	require.Contains(t, out.String(), `"read_only":true`)
	_, err := os.Stat(filepath.Join(home, ".opcode"))
	require.True(t, os.IsNotExist(err))
}
