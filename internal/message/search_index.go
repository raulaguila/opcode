package message

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/raulaguila/opcode/internal/logging"
	"github.com/raulaguila/opcode/internal/memoryindex"
)

type SearchIndexer struct {
	Directory, Project string
	Redact             func(string) string
	mu                 sync.Mutex
}

func WithSearchIndex(directory, project string, redact func(string) string) func(*service) {
	return func(s *service) { s.index = &SearchIndexer{Directory: directory, Project: project, Redact: redact} }
}
func (s *service) projectMessage(ctx context.Context, msg Message) {
	if s.index == nil || msg.Role == Assistant && msg.FinishPart() == nil {
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
	s.index.mu.Lock()
	defer s.index.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	err := memoryindex.Upsert(ctx, s.index.Directory, s.index.Project, memoryindex.Document{ID: "message:" + msg.ID, Kind: "conversation", Session: msg.SessionID, Text: text, Stamp: msg.UpdatedAt})
	if err != nil {
		logging.Warn("Conversation search projection unavailable", "error_kind", fmt.Sprintf("%T", err))
	}
}
