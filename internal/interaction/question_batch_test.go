package interaction

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestBatchQuestionsRequireCompleteValidatedHumanAnswers(t *testing.T) {
	s := NewQuestions()
	s.SetInteractive(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan string, 1)
	go func() {
		response, err := s.Ask(ctx, Question{SessionID: "s", Questions: []QuestionItem{
			{ID: "features", Question: "Quais recursos?", Type: "multi_choice", Choices: []string{"A", "B", "C"}},
			{ID: "reason", Question: "Por quê?", Type: "free_text"},
			{ID: "confirm", Question: "Está correto?", Type: "yes_no"},
		}})
		if err != nil {
			done <- err.Error()
		} else {
			done <- response
		}
	}()
	var q Question
	require.Eventually(t, func() bool { var ok bool; q, ok = s.Pending("s"); return ok }, time.Second, time.Millisecond)
	snapshot, _ := s.Pending("s")
	snapshot.Questions[0].Choices[0] = "injected"
	again, _ := s.Pending("s")
	require.Equal(t, "A", again.Questions[0].Choices[0])
	require.Error(t, s.Answer("s", q.ID, "approved"), "a plain approval cannot answer a structured batch")
	valid := []AnswerItem{{QuestionID: "features", Choices: []string{"A", "C"}}, {QuestionID: "reason", Text: "Preciso de ambos"}, {QuestionID: "confirm", Choices: []string{"yes"}}}
	require.Error(t, s.AnswerBatch("s", q.ID, valid[:2]))
	invalid := append([]AnswerItem(nil), valid...)
	invalid[0].Choices = []string{"injected"}
	require.Error(t, s.AnswerBatch("s", q.ID, invalid))
	invalid[0].Choices = []string{"A", "A"}
	require.Error(t, s.AnswerBatch("s", q.ID, invalid))
	invalid = append([]AnswerItem(nil), valid...)
	invalid[1].QuestionID = "features"
	require.Error(t, s.AnswerBatch("s", q.ID, invalid))
	select {
	case <-done:
		t.Fatal("batch completed before valid human response")
	default:
	}
	require.NoError(t, s.AnswerBatch("s", q.ID, valid))
	var actual AnswerBatch
	require.NoError(t, json.Unmarshal([]byte(<-done), &actual))
	require.Equal(t, valid, actual.Answers)
	require.Error(t, s.AnswerBatch("s", q.ID, valid))
}

func TestBatchValidationAndCancellation(t *testing.T) {
	for _, items := range [][]QuestionItem{
		{{Question: "Missing type"}},
		{{Question: "Pick", Type: "single_choice", Choices: []string{"one"}}},
		{{Question: "Pick", Type: "single_choice", Choices: []string{"one", "one"}}},
		{{ID: "same", Question: "A", Type: "free_text"}, {ID: "same", Question: "B", Type: "free_text"}},
		{{Question: "Yes?", Type: "yes_no", Choices: []string{"custom", "choices"}}},
	} {
		s := NewQuestions()
		s.SetInteractive(true)
		_, err := s.Ask(context.Background(), Question{SessionID: "s", Questions: items})
		require.Error(t, err)
	}
	s := NewQuestions()
	s.SetInteractive(true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.Ask(ctx, Question{SessionID: "s", Questions: []QuestionItem{{Question: "Details", Type: "free_text"}}})
		done <- err
	}()
	require.Eventually(t, func() bool { q, ok := s.Pending("s"); return ok && q.Questions[0].ID == "q1" }, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	_, ok := s.Pending("s")
	require.False(t, ok)
}
