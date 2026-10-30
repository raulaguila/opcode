package chat

import (
	"context"
	"slices"

	"github.com/raulaguila/opcode/internal/message"
)

func readPromptHistory(ctx context.Context, service message.Service, session string) ([]string, error) {
	pager, ok := service.(message.HistoryPager)
	if !ok {
		items, err := service.List(ctx, session)
		if err != nil {
			return nil, err
		}
		return lastPrompts(items), nil
	}
	history := []string{}
	var before *message.HistoryCursor
	for len(history) < 200 {
		page, err := pager.ListPage(ctx, session, before, transcriptPageMessages)
		if err != nil {
			return nil, err
		}
		if err := validateOlderPage(before, page); err != nil {
			return nil, err
		}
		history = appendRecentPrompts(history, page.Messages)
		if !page.More {
			break
		}
		before = page.Before
	}
	slices.Reverse(history)
	return history, nil
}

func appendRecentPrompts(history []string, items []message.Message) []string {
	for i := len(items) - 1; i >= 0 && len(history) < 200; i-- {
		if prompt, ok := historicalPrompt(items[i]); ok {
			history = append(history, prompt)
		}
	}
	return history
}

func historicalPrompt(item message.Message) (string, bool) {
	text := item.Content().Text
	return text, item.Role == message.User && !item.IsApplication() && text != ""
}

func lastPrompts(items []message.Message) []string {
	history := []string{}
	for _, item := range items {
		if prompt, ok := historicalPrompt(item); ok {
			history = append(history, prompt)
		}
	}
	return history[max(0, len(history)-200):]
}
