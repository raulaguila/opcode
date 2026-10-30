package message

import (
	"context"
	"fmt"
	"slices"

	"github.com/raulaguila/opcode/internal/db"
)

const MaxHistoryPageMessages = 256

// HistoryCursor is a keyset within one session, including insertion order for
// messages created in the same second. It remains valid if its row is deleted.
type HistoryCursor struct {
	SessionID        string
	CreatedAt, RowID int64
}

type HistoryPage struct {
	Messages []Message
	Before   *HistoryCursor
	More     bool
}

// HistoryPager is optional UI presentation history. List/Get remain complete,
// authoritative provider input, including attachments and continuation data.
type HistoryPager interface {
	ListPage(context.Context, string, *HistoryCursor, int) (HistoryPage, error)
}

type pageQueries interface {
	ListMessagePage(context.Context, db.MessagePageParams) ([]db.MessagePageRow, error)
}

func (s *service) ListPage(ctx context.Context, session string, before *HistoryCursor, limit int) (HistoryPage, error) {
	if limit < 1 || limit > MaxHistoryPageMessages || before != nil && (before.SessionID != session || before.RowID < 1) {
		return HistoryPage{}, fmt.Errorf("invalid history page bounds")
	}
	queries, ok := s.q.(pageQueries)
	if !ok {
		return HistoryPage{}, fmt.Errorf("message store does not support history paging")
	}
	params := db.MessagePageParams{SessionID: session, Limit: limit + 1}
	if before != nil {
		params.BeforeCreatedAt, params.BeforeRowID = before.CreatedAt, before.RowID
	}
	rows, err := queries.ListMessagePage(ctx, params)
	if err != nil {
		return HistoryPage{}, err
	}
	page := HistoryPage{More: len(rows) > limit}
	rows = rows[:min(len(rows), limit)]
	slices.Reverse(rows)
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return HistoryPage{}, err
		}
		item, err := s.fromDBItem(row.Message)
		if err != nil {
			return HistoryPage{}, err
		}
		page.Messages = append(page.Messages, s.liveHistoryMessage(item))
	}
	if before == nil && page.More {
		start := initialHistoryBoundary(page.Messages)
		page.Messages, rows = page.Messages[start:], rows[start:]
	}
	if len(rows) > 0 {
		page.Before = &HistoryCursor{SessionID: session, CreatedAt: rows[0].CreatedAt, RowID: rows[0].RowID}
	}
	return page, nil
}

func initialHistoryBoundary(messages []Message) int {
	for i, item := range messages {
		if item.Role == User && !item.IsApplication() {
			return i
		}
	}
	return 0 // An unusually long turn remains bounded; scrolling finds its user.
}

func (s *service) liveHistoryMessage(persisted Message) Message {
	s.streamMu.RLock()
	defer s.streamMu.RUnlock()
	if live, ok := s.streams[persisted.ID]; ok {
		return HistorySnapshot(live)
	}
	return persisted
}

// HistorySnapshot owns presentation parts without copying binary payloads or
// retaining private provider continuation data. Text and tool state are exact.
func HistorySnapshot(msg Message) Message {
	msg.Parts = slices.Clone(msg.Parts)
	for i, part := range msg.Parts {
		switch value := part.(type) {
		case BinaryContent:
			value.Data = nil
			msg.Parts[i] = value
		case Finish:
			value.ProviderState = nil
			msg.Parts[i] = value
		}
	}
	return msg
}
