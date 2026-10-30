package page

import (
	"context"
	"errors"
	"strings"

	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/provider"
)

type recoveryKind uint8

const (
	recoveryInterrupted recoveryKind = iota
	recoveryCitation
	recoveryCapacity
	recoveryAuthentication
	recoveryMemory
	recoveryCancelled
	recoveryUnavailable
	recoveryConflict
	recoveryBudget
)

func classifyRecovery(err error) recoveryKind {
	if errors.Is(err, agent.ErrSourceCitation) {
		return recoveryCitation
	}
	if errors.Is(err, agent.ErrContextLimit) {
		return recoveryCapacity
	}
	if errors.Is(err, context.Canceled) {
		return recoveryCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return recoveryUnavailable
	}
	var httpError *provider.HTTPError
	if errors.As(err, &httpError) {
		switch httpError.StatusCode {
		case 401, 403:
			return recoveryAuthentication
		case 413:
			return recoveryCapacity
		case 408, 429, 500, 502, 503, 504:
			return recoveryUnavailable
		}
	}
	return classifyRecoveryText(strings.ToLower(err.Error()))
}

// Specific legacy/provider phrases keep diagnostics containing "context" from
// being confused with capacity failures.
func classifyRecoveryText(text string) recoveryKind {
	switch {
	case recoveryContains(text, "source citation", "invalid source citations"):
		return recoveryCitation
	case recoveryContains(text, "401 unauthorized", "provider http 401", "provider http 403", "api key", "unauthorized", "authentication"):
		return recoveryAuthentication
	case recoveryContains(text, "out of memory", "insufficient memory", "requires more system memory"):
		return recoveryMemory
	case recoveryContains(text, "cancel"):
		return recoveryCancelled
	case recoveryContains(text, "connection", "timeout", "deadline exceeded", "provider http 429", "provider http 503"):
		return recoveryUnavailable
	case recoveryContains(text, "context budget exceeded", "context_length_exceeded", "maximum context length", "context length exceeded", "context window exceeded", "prompt is too long", "provider http 413"):
		return recoveryCapacity
	case recoveryContains(text, "agent step limit reached", "tool result budget reached", "tool call budget exceeded"):
		return recoveryBudget
	case recoveryContains(text, "conflict", "modified externally"):
		return recoveryConflict
	default:
		return recoveryInterrupted
	}
}

func recoveryContains(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}
