package message

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/memoryindex"
)

type SearchIndexer struct {
	Directory, Project string
	Redact             func(string) string
	store              *memoryindex.Store
}

func WithSearchIndex(directory, project string, redact func(string) string) func(*service) {
	return func(s *service) {
		s.index = &SearchIndexer{Directory: directory, Project: project, Redact: redact, store: memoryindex.NewStore(directory, project)}
	}
}

func (s *service) Close() error {
	if s.Broker != nil {
		s.Broker.Shutdown()
	}
	if s.index != nil {
		return s.index.store.Close()
	}
	return nil
}
func (s *service) projectMessage(ctx context.Context, msg Message) {
	if s.index == nil || msg.IsApplication() || msg.Role == Assistant && msg.FinishPart() == nil {
		return
	}
	var b strings.Builder
	b.WriteString(msg.Content().Text)
	for _, r := range msg.ToolResults() {
		b.WriteString("\n")
		b.WriteString(r.Content)
	}
	text := b.String()
	if s.index.Redact != nil {
		text = s.index.Redact(text)
	}
	if len(text) > 8192 {
		text = strings.ToValidUTF8(text[:8192], "")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	err := s.index.store.Upsert(ctx, memoryindex.Document{ID: "message:" + msg.ID, Kind: "conversation", Session: msg.SessionID, Text: text, Stamp: msg.UpdatedAt})
	if err != nil {
		logging.Warn("Conversation search projection unavailable", "error_kind", fmt.Sprintf("%T", err))
	}
}
