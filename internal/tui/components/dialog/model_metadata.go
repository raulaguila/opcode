package dialog

import (
	"context"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/models"
)

type modelMetadataLoadedMsg struct {
	request uint64
	model   models.Model
	err     error
}

func (m *modelDialogCmp) scheduleMetadata() tea.Cmd {
	if m.selecting {
		return nil
	}
	if m.selectedIdx < 0 || m.selectedIdx >= len(m.filtered) {
		if m.metadataCancel != nil {
			m.metadataCancel()
			m.metadataCancel = nil
		}
		m.metadataRequest++
		m.metadataFor, m.metadataNote = "", ""
		return nil
	}
	selected := m.filtered[m.selectedIdx]
	identity := string(m.provider) + "/" + string(selected.ID)
	if m.metadataFor == identity {
		return nil
	}
	if m.metadataCancel != nil {
		m.metadataCancel()
		m.metadataCancel = nil
	}
	m.metadataRequest++
	m.metadataFor = identity
	m.metadataNote = ""
	lookup := config.ModelMetadata(selected)
	if lookup == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.metadataCancel = cancel
	request := m.metadataRequest
	m.metadataNote = config.Text("Consultando metadados…", "Reading metadata…")
	return func() tea.Msg {
		defer cancel()
		timer := time.NewTimer(200 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return modelMetadataLoadedMsg{request: request, model: selected, err: ctx.Err()}
		case <-timer.C:
		}
		found, err := lookup(ctx)
		return modelMetadataLoadedMsg{request: request, model: found, err: err}
	}
}

func (m *modelDialogCmp) acceptMetadata(msg modelMetadataLoadedMsg) tea.Cmd {
	if msg.request != m.metadataRequest {
		return nil
	}
	m.metadataCancel = nil
	if msg.err != nil {
		m.metadataNote = config.Text("Metadados indisponíveis; valores anteriores preservados.", "Metadata unavailable; previous values preserved.")
		m.keepVisible()
		return nil
	}
	for i := range m.models {
		if m.models[i].ID == msg.model.ID {
			m.models[i] = msg.model
		}
	}
	for i := range m.filtered {
		if m.filtered[i].ID == msg.model.ID {
			m.filtered[i] = msg.model
		}
	}
	m.metadataNote = ""
	m.keepVisible()
	return nil
}

// Matching is Unicode-aware and bounded by the input/catalog limits. Exact
// substrings rank before subsequences, then compact matches before scattered ones.
func modelMatchScore(query, label string) (int, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	label = strings.ToLower(label)
	if query == "" {
		return 0, true
	}
	if offset := strings.Index(label, query); offset >= 0 {
		return offset, true
	}
	chars := []rune(label)
	next, first, last, count := 0, -1, -1, 0
	for _, r := range query {
		if unicode.IsSpace(r) {
			continue
		}
		found := false
		for next < len(chars) {
			pos := next
			next++
			if chars[pos] == r {
				if first < 0 {
					first = pos
				}
				last = pos
				count++
				found = true
				break
			}
		}
		if !found {
			return 0, false
		}
	}
	if count == 0 {
		return 0, true
	}
	return 1000 + first + (last-first+1-count)*4, true
}
