package interaction

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type Question struct {
	ID          string         `json:"id"`
	SessionID   string         `json:"session_id"`
	Question    string         `json:"question"`
	Explanation string         `json:"explanation,omitempty"`
	Choices     []string       `json:"choices,omitempty"`
	Questions   []QuestionItem `json:"questions,omitempty"`
}
type pending struct {
	question Question
	answer   chan string
}
type Questions struct {
	mu          sync.Mutex
	interactive bool
	pending     map[string]pending
}

func NewQuestions() *Questions { return &Questions{pending: map[string]pending{}} }
func (s *Questions) SetInteractive(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interactive = enabled
}
func (s *Questions) Pending(session string) (Question, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[session]
	q := p.question
	q = cloneQuestion(q)
	return q, ok
}
func (s *Questions) Ask(ctx context.Context, q Question) (string, error) {
	if strings.TrimSpace(q.SessionID) == "" {
		return "", fmt.Errorf("question requires a session")
	}
	if len(q.Questions) > 0 {
		var err error
		q, err = normalizeBatch(q)
		if err != nil {
			return "", err
		}
	} else if strings.TrimSpace(q.Question) == "" || len(q.Question) > 500 || len(q.Explanation) > 1000 || len(q.Choices) > 5 {
		return "", fmt.Errorf("question requires text (up to 500 bytes), explanation up to 1000 and at most five choices")
	}
	for _, choice := range q.Choices {
		if strings.TrimSpace(choice) == "" || len(choice) > 300 {
			return "", fmt.Errorf("invalid question choice")
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	if !s.interactive {
		s.mu.Unlock()
		return "", fmt.Errorf("user question requires interactive mode; no answer or permission was granted")
	}
	if _, exists := s.pending[q.SessionID]; exists {
		s.mu.Unlock()
		return "", fmt.Errorf("a question is already pending")
	}
	q.ID = uuid.NewString()
	q = cloneQuestion(q)
	p := pending{question: q, answer: make(chan string, 1)}
	s.pending[q.SessionID] = p
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.pending[q.SessionID].question.ID == q.ID {
			delete(s.pending, q.SessionID)
		}
		s.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case text := <-p.answer:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return text, nil
	}
}
func (s *Questions) Answer(session, id, text string) error {
	if strings.TrimSpace(text) == "" || len(text) > 20000 {
		return fmt.Errorf("answer must contain 1–4000 bytes")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[session]
	if !ok || p.question.ID != id {
		return fmt.Errorf("question is no longer pending")
	}
	if len(p.question.Questions) > 0 {
		batch, err := parseAnswerBatch(text)
		if err != nil {
			return fmt.Errorf("invalid question answers")
		}
		if err := ValidateAnswers(p.question, batch.Answers); err != nil {
			return err
		}
	} else if len(text) > 4000 {
		return fmt.Errorf("answer must contain 1–4000 bytes")
	}
	select {
	case p.answer <- text:
		delete(s.pending, session)
		return nil
	default:
		return fmt.Errorf("question already answered")
	}
}
