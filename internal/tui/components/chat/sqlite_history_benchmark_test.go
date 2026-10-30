package chat

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pressly/goose/v3"
	"github.com/raulaguila/opcode/internal/app"
	"github.com/raulaguila/opcode/internal/db"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/session"
)

// Keep this fixture compatible with the full-List implementation so before and
// after builds execute identical real persistence and UI command boundaries.
func BenchmarkSQLiteChatSessionLoad(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			_, service := sqliteHistoryFixture(b, count)
			var displayed *messagesCmp
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				displayed = NewMessagesCmp(&app.App{Messages: service}).(*messagesCmp)
				displayed.SetSize(100, 30)
				runSQLiteHistoryCommand(displayed, displayed.SetSession(session.Session{ID: "A"}))
			}
			b.StopTimer()
			if displayed.rendering || displayed.currentMsgID != fmt.Sprintf("%06d", count-1) {
				b.Fatal("initial persistence snapshot did not display the latest message")
			}
			full, err := service.List(context.Background(), "A")
			if err != nil || len(full) != count {
				b.Fatalf("authoritative history changed: count=%d err=%v", len(full), err)
			}
		})
	}
}

func sqliteHistoryFixture(tb testing.TB, count int) (*sql.DB, message.Service) {
	tb.Helper()
	connection, err := sql.Open("sqlite3", filepath.Join(tb.TempDir(), "history.sqlite"))
	if err != nil {
		tb.Fatal(err)
	}
	connection.SetMaxOpenConns(1)
	tb.Cleanup(func() { connection.Close() })
	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(db.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		tb.Fatal(err)
	}
	if err := goose.Up(connection, "migrations"); err != nil {
		tb.Fatal(err)
	}
	if _, err := connection.Exec(`INSERT INTO sessions(id,title,created_at,updated_at) VALUES('A','History',1,1)`); err != nil {
		tb.Fatal(err)
	}
	seedSQLiteHistory(tb, connection, count)
	service := message.NewService(db.New(connection))
	tb.Cleanup(func() {
		if closer, ok := service.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				tb.Error(err)
			}
		}
	})
	return connection, service
}

func seedSQLiteHistory(tb testing.TB, connection *sql.DB, count int) {
	tb.Helper()
	tx, err := connection.Begin()
	if err != nil {
		tb.Fatal(err)
	}
	defer tx.Rollback()
	statement, err := tx.Prepare(`INSERT INTO messages(id,session_id,role,parts,created_at,updated_at) VALUES(?,'A',?,?,1,1)`)
	if err != nil {
		tb.Fatal(err)
	}
	defer statement.Close()
	for i := range count {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		parts := fmt.Sprintf(`[{"type":"text","data":{"Text":"Historical evidence message %d."}}]`, i)
		if _, err := statement.Exec(fmt.Sprintf("%06d", i), role, parts); err != nil {
			tb.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		tb.Fatal(err)
	}
}

func runSQLiteHistoryCommand(m *messagesCmp, command tea.Cmd) {
	if command == nil {
		return
	}
	switch result := command().(type) {
	case tea.BatchMsg:
		for _, next := range result {
			runSQLiteHistoryCommand(m, next)
		}
	default:
		_, next := m.Update(result)
		runSQLiteHistoryCommand(m, next)
	}
}
