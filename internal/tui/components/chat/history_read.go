package chat

import (
	"context"
	"fmt"

	"github.com/raulaguila/opcode/internal/message"
)

const transcriptPageMessages = 128

func readTranscriptHistory(ctx context.Context, service message.Service, session string, target *message.HistoryCursor, all bool) (message.HistoryPage, bool, error) {
	pager, ok := service.(message.HistoryPager)
	if !ok {
		messages, err := service.List(ctx, session)
		return message.HistoryPage{Messages: messages}, false, err
	}
	page, err := pager.ListPage(ctx, session, nil, transcriptPageMessages)
	for err == nil && page.More && (all || cursorAfter(page.Before, target)) {
		next, nextErr := pager.ListPage(ctx, session, page.Before, transcriptPageMessages)
		if nextErr != nil {
			return message.HistoryPage{}, true, nextErr
		}
		if err := validateOlderPage(page.Before, next); err != nil {
			return message.HistoryPage{}, true, err
		}
		page.Messages = append(next.Messages, page.Messages...)
		page.Before, page.More = next.Before, next.More
	}
	return page, true, err
}

func cursorAfter(cursor, target *message.HistoryCursor) bool {
	return cursor != nil && target != nil && (cursor.CreatedAt > target.CreatedAt || cursor.CreatedAt == target.CreatedAt && cursor.RowID > target.RowID)
}

func validateOlderPage(before *message.HistoryCursor, page message.HistoryPage) error {
	if page.More && (len(page.Messages) == 0 || page.Before == nil) {
		return fmt.Errorf("history page made no progress")
	}
	if len(page.Messages) > 0 && before != nil && !cursorAfter(before, page.Before) {
		return fmt.Errorf("history cursor did not move backwards")
	}
	return nil
}
