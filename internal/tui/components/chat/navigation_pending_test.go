package chat

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func TestClosingFindCancelsOnlyDeferredSearchNavigation(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyRunes, Alt: true, Runes: []rune{'f'}}} {
		t.Run(key.String(), func(t *testing.T) {
			m := sqliteTranscriptFixture(t, 400)
			m.StartSearch("Historical evidence message 0.")
			command := PendingHistoryCommand(m)
			require.NotNil(t, command)
			require.True(t, m.reconcile.pending)
			require.True(t, m.TranscriptKey(key))
			require.False(t, m.find.open)
			require.True(t, m.reconcile.pending, "closing find must not cancel the snapshot read")
			runSQLiteHistoryCommand(m, command)
			require.False(t, m.find.open, "completing an already queued page must not reopen find")
			require.Nil(t, m.history.resume)
			require.False(t, m.history.all)
			require.True(t, m.history.more, "canceled navigation must not keep loading every older page")
			require.Len(t, m.messages, 2*transcriptPageMessages)
		})
	}
}

func TestClosingFindKeepsGapReconciliation(t *testing.T) {
	m := sqliteTranscriptFixture(t, 400)
	m.StartSearch("old history")
	command := PendingHistoryCommand(m)
	m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: m.messages[len(m.messages)-1], ReconcileRequired: true})
	require.True(t, m.reconcile.needed)
	require.True(t, m.TranscriptKey(tea.KeyMsg{Type: tea.KeyEsc}))
	runSQLiteHistoryCommand(m, command)
	require.False(t, m.find.open)
	require.False(t, m.reconcile.needed, "the authoritative retry must finish after find closes")
	require.False(t, m.reconcile.pending)
	require.Equal(t, "000399", m.currentMsgID)
}

func TestDeferredKeyboardSelectionCanBeCanceled(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyRunes, Alt: true, Runes: []rune{'s'}}} {
		t.Run(key.String(), func(t *testing.T) {
			m := sqliteTranscriptFixture(t, 400)
			m.ToggleKeyboardSelection()
			command := PendingHistoryCommand(m)
			require.NotNil(t, command)
			require.True(t, m.TranscriptKey(key))
			runSQLiteHistoryCommand(m, command)
			require.False(t, m.selection.keyboard)
			require.False(t, m.HasSelection())
			require.False(t, m.history.all)
			require.True(t, m.history.more)
		})
	}
}

func TestClosingFindPreservesDifferentDeferredSelection(t *testing.T) {
	m := sqliteTranscriptFixture(t, 400)
	m.StartSearch("history")
	command := PendingHistoryCommand(m)
	m.ToggleKeyboardSelection()
	require.True(t, m.TranscriptKey(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'f'}}))
	require.False(t, m.find.open)
	runSQLiteHistoryCommand(m, command)
	require.True(t, m.selection.keyboard, "closing find must not cancel a separately requested selection")
	require.False(t, m.history.more)
}
