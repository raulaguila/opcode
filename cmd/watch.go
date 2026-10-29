package cmd

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/spf13/cobra"
)

const watchTextLimit = 16 * 1024
const watchSnapshotLimit = 128 * 1024
const watchPartsLimit = 256 * 1024

type watchOptions struct {
	Database, Session, Format string
	Limit, Offset             int
	Follow                    bool
}
type watchSession struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt int64  `json:"updated_at"`
}
type watchMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
	Finished  bool   `json:"finished"`
}
type watchSnapshot struct {
	SessionID  string         `json:"session_id,omitempty"`
	Sessions   []watchSession `json:"sessions,omitempty"`
	Messages   []watchMessage `json:"messages,omitempty"`
	State      string         `json:"state,omitempty"`
	Mode       string         `json:"mode,omitempty"`
	Offset     int            `json:"offset"`
	NextOffset *int           `json:"next_offset,omitempty"`
	Truncated  bool           `json:"truncated"`
	ReadOnly   bool           `json:"read_only"`
}

func newWatchCommand() *cobra.Command {
	o := watchOptions{}
	c := &cobra.Command{Use: "watch", Short: "Observe saved sessions without starting an agent or granting permissions", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		c.SilenceUsage = true
		if o.Database == "" {
			return fmt.Errorf("--database must name an existing Opcode SQLite file")
		}
		if o.Limit < 1 || o.Limit > 50 || o.Offset < 0 || o.Offset > 10000 {
			return fmt.Errorf("--limit must be 1–50 and --offset 0–10000")
		}
		if o.Format != "text" && o.Format != "json" {
			return fmt.Errorf("--format must be text or json")
		}
		if len(o.Session) > 128 {
			return fmt.Errorf("session ID is too long")
		}
		ctx, cancel := signal.NotifyContext(c.Context(), os.Interrupt)
		defer cancel()
		conn, err := openWatchDatabase(ctx, o.Database)
		if err != nil {
			return err
		}
		defer conn.Close()
		return streamWatch(ctx, conn, o, c.OutOrStdout(), time.Second)
	}}
	c.Flags().StringVar(&o.Database, "database", "", "Existing database file; opened read-only without migration")
	c.Flags().StringVar(&o.Session, "session", "", "Session to observe; omit to list recent sessions")
	c.Flags().BoolVar(&o.Follow, "follow", false, "Poll saved snapshots and emit only changed content; Ctrl+C stops")
	c.Flags().StringVarP(&o.Format, "format", "f", "text", "Output format: text or newline-delimited json")
	c.Flags().IntVar(&o.Limit, "limit", 20, "Page size, 1–50")
	c.Flags().IntVar(&o.Offset, "offset", 0, "Page offset, 0–10000; pages start at the most recent entries")
	return c
}
func init() { rootCmd.AddCommand(newWatchCommand()) }

// Do not use db.Connect here: it creates directories, changes pragmas and runs
// migrations. mode=ro plus query_only applies to every connection in the pool.
func openWatchDatabase(ctx context.Context, path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid database path")
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, fmt.Errorf("database must already exist")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("database must be a regular file, not a symlink")
	}
	u := url.URL{Scheme: "file", Path: absolute}
	q := url.Values{}
	q.Set("mode", "ro")
	q.Add("_pragma", "query_only(1)")
	q.Add("_pragma", "busy_timeout(1000)")
	u.RawQuery = q.Encode()
	conn, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, fmt.Errorf("cannot open read-only database")
	}
	conn.SetMaxOpenConns(1)
	ping, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err = conn.PingContext(ping); err != nil {
		conn.Close()
		return nil, fmt.Errorf("cannot access existing database read-only")
	}
	return conn, nil
}

func streamWatch(ctx context.Context, conn *sql.DB, o watchOptions, out io.Writer, interval time.Duration) error {
	var previous [32]byte
	sent := false
	for {
		if ctx.Err() != nil {
			return nil
		}
		query, cancel := context.WithTimeout(ctx, 3*time.Second)
		snapshot, err := readWatchSnapshot(query, conn, o)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("cannot encode observer snapshot")
		}
		digest := sha256.Sum256(encoded)
		if !sent || digest != previous {
			if o.Format == "json" {
				_, err = fmt.Fprintln(out, string(encoded))
			} else {
				err = printWatchText(out, snapshot)
			}
			if err != nil {
				return err
			}
			previous = digest
			sent = true
		}
		if !o.Follow {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func readWatchSnapshot(ctx context.Context, conn *sql.DB, o watchOptions) (watchSnapshot, error) {
	result := watchSnapshot{SessionID: watchSafe(o.Session, 128), Offset: o.Offset, ReadOnly: true}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, fmt.Errorf("cannot start read-only snapshot")
	}
	defer tx.Rollback()
	if o.Session == "" {
		rows, err := tx.QueryContext(ctx, `SELECT substr(id,1,128),substr(title,1,1024),updated_at FROM sessions ORDER BY updated_at DESC,id LIMIT ? OFFSET ?`, o.Limit+1, o.Offset)
		if err != nil {
			return result, fmt.Errorf("cannot read sessions; database schema may be unsupported")
		}
		defer rows.Close()
		for rows.Next() {
			var item watchSession
			if err = rows.Scan(&item.ID, &item.Title, &item.UpdatedAt); err != nil {
				return result, fmt.Errorf("cannot decode saved session")
			}
			if len(result.Sessions) == o.Limit {
				next := o.Offset + o.Limit
				result.NextOffset = &next
				result.Truncated = true
				break
			}
			item.ID = watchSafe(item.ID, 128)
			item.Title = watchSafe(item.Title, 1024)
			result.Sessions = append(result.Sessions, item)
		}
		if rows.Err() != nil {
			return result, fmt.Errorf("cannot finish session snapshot")
		}
		return result, nil
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id=?`, o.Session).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return result, fmt.Errorf("session not found")
	} else if err != nil {
		return result, fmt.Errorf("cannot read session; database schema may be unsupported")
	}
	// Read text/settled messages only. Oversized parts are not fetched or parsed.
	rows, err := tx.QueryContext(ctx, `SELECT substr(id,1,128),role,CASE WHEN length(CAST(parts AS BLOB))<=? THEN parts ELSE NULL END,finished_at IS NOT NULL FROM messages WHERE session_id=? AND (role='user' OR (role='assistant' AND finished_at IS NOT NULL)) ORDER BY created_at DESC,rowid DESC LIMIT ? OFFSET ?`, watchPartsLimit, o.Session, o.Limit+1, o.Offset)
	if err != nil {
		return result, fmt.Errorf("cannot read messages; database schema may be unsupported")
	}
	consumed, remaining := 0, watchSnapshotLimit
	for rows.Next() {
		if consumed == o.Limit {
			result.Truncated = true
			break
		}
		var item watchMessage
		var raw sql.NullString
		if err = rows.Scan(&item.ID, &item.Role, &raw, &item.Finished); err != nil {
			rows.Close()
			return result, fmt.Errorf("cannot decode saved message")
		}
		consumed++
		if !raw.Valid {
			item.Text = "[saved message exceeds observer limit]"
			item.Truncated = true
		} else {
			text, application, invalid := watchMessageText(raw.String)
			if application {
				continue
			}
			if invalid {
				item.Text = "[saved message text unavailable]"
				item.Truncated = true
			} else {
				item.Truncated = len(text) > watchTextLimit
				item.Text = watchSafe(text, watchTextLimit)
			}
		}
		if len(item.Text) > remaining {
			result.Truncated = true
			consumed--
			break
		}
		remaining -= len(item.Text)
		item.ID = watchSafe(item.ID, 128)
		result.Messages = append(result.Messages, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, fmt.Errorf("cannot finish message snapshot")
	}
	if result.Truncated {
		next := o.Offset + consumed
		result.NextOffset = &next
	}
	// Optional workflow state is projected to known non-executable labels only.
	var table int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type='table' AND name='agent_workflows'`).Scan(&table); err == nil {
		var raw sql.NullString
		if tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(state AS BLOB))<=? THEN state ELSE NULL END FROM agent_workflows WHERE session_id=?`, watchPartsLimit, o.Session).Scan(&raw) == nil && raw.Valid {
			var state struct{ Status, Mode string }
			if json.Unmarshal([]byte(raw.String), &state) == nil {
				result.State = watchSafe(state.Status, 64)
				result.Mode = watchSafe(state.Mode, 32)
			}
		}
	}
	return result, nil
}
func watchMessageText(raw string) (string, bool, bool) {
	var parts []struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &parts) != nil {
		return "", false, true
	}
	var b strings.Builder
	for _, part := range parts {
		if part.Type != "text" {
			continue
		}
		var data struct{ Text, Origin string }
		if json.Unmarshal(part.Data, &data) != nil {
			return "", false, true
		}
		if data.Origin == "application" {
			return "", true, false
		}
		b.WriteString(data.Text)
		b.WriteByte('\n')
	}
	return strings.TrimSuffix(b.String(), "\n"), false, false
}
func watchSafe(value string, limit int) string {
	value = tools.RedactCredentials(ansi.Strip(value))
	var b strings.Builder
	for _, r := range value {
		text := string(r)
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.Is(unicode.Cf, r) {
			text = fmt.Sprintf("\\u%04x", r)
		}
		if b.Len()+len(text) > limit {
			break
		}
		b.WriteString(text)
	}
	return b.String()
}
func printWatchText(out io.Writer, s watchSnapshot) error {
	if _, err := fmt.Fprintf(out, "Read-only snapshot · offset %d\n", s.Offset); err != nil {
		return err
	}
	for _, item := range s.Sessions {
		if _, err := fmt.Fprintf(out, "%s · %s\n", item.ID, strings.ReplaceAll(item.Title, "\n", "\\n")); err != nil {
			return err
		}
	}
	if s.SessionID != "" {
		if _, err := fmt.Fprintf(out, "Session %s · %s %s\n", s.SessionID, s.Mode, s.State); err != nil {
			return err
		}
	}
	for _, item := range s.Messages {
		if _, err := fmt.Fprintf(out, "%s %s\n  %s\n", item.Role, item.ID, strings.ReplaceAll(item.Text, "\n", "\n  ")); err != nil {
			return err
		}
	}
	if s.NextOffset != nil {
		_, err := fmt.Fprintf(out, "More: --offset %d\n", *s.NextOffset)
		return err
	}
	return nil
}
