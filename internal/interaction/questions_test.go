package interaction

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQuestionRequiresActualAnswerAndRejectsStaleReplies(t *testing.T) {
	s := NewQuestions()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := s.Ask(ctx, Question{SessionID: "s", Question: "Qual banco?"})
	require.ErrorContains(t, err, "interactive")
	s.SetInteractive(true)
	done := make(chan string, 1)
	go func() {
		answer, err := s.Ask(ctx, Question{SessionID: "s", Question: "Qual banco?", Choices: []string{"SQLite", "Postgres"}})
		if err != nil {
			done <- err.Error()
		} else {
			done <- answer
		}
	}()
	var q Question
	require.Eventually(t, func() bool { var ok bool; q, ok = s.Pending("s"); return ok }, time.Second, time.Millisecond)
	select {
	case <-done:
		t.Fatal("question completed without an answer")
	default:
	}
	require.Error(t, s.Answer("other", q.ID, "SQLite"))
	require.Error(t, s.Answer("s", "stale", "SQLite"))
	require.Error(t, s.Answer("s", q.ID, ""))
	require.NoError(t, s.Answer("s", q.ID, "Minha resposta livre"))
	require.Equal(t, "Minha resposta livre", <-done)
	require.Error(t, s.Answer("s", q.ID, "SQLite"))
	_, ok := s.Pending("s")
	require.False(t, ok)
}
func TestCancellationReleasesPendingQuestion(t *testing.T) {
	s := NewQuestions()
	s.SetInteractive(true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.Ask(ctx, Question{SessionID: "s", Question: "Continuar?"}); done <- err }()
	require.Eventually(t, func() bool { _, ok := s.Pending("s"); return ok }, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	_, ok := s.Pending("s")
	require.False(t, ok)
}
