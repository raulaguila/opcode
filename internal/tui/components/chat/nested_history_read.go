package chat

import (
	"context"

	"github.com/raulaguila/opcode/internal/message"
)

func readNestedHistory(ctx context.Context, service message.Service, session string) ([]message.Message, int, error) {
	pager, ok := service.(message.HistoryPager)
	if !ok {
		items, err := service.List(ctx, session)
		history := compactTaskHistory(items)
		return history, messageHistoryBytes(history), err
	}
	history := []message.Message{}
	total := 0
	var before *message.HistoryCursor
	for {
		page, err := pager.ListPage(ctx, session, before, transcriptPageMessages)
		if err != nil {
			return nil, 0, err
		}
		if err := validateOlderPage(before, page); err != nil {
			return nil, 0, err
		}
		compact := compactTaskHistory(page.Messages)
		total += messageHistoryBytes(compact)
		if total > nestedHistoryLimit {
			return nil, total, nil
		}
		history = append(compact, history...)
		if !page.More {
			return history, total, nil
		}
		before = page.Before
	}
}
