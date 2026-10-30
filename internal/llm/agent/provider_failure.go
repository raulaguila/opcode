package agent

import (
	"context"
	"errors"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/llm/provider"
)

// An API error event can contain arbitrary echoed prompts/keys, even over HTTP
// 200. Public text is a classification; Unwrap retains the private cause.
type providerEventError struct {
	cause  error
	public string
}

func (e *providerEventError) Error() string { return e.public }
func (e *providerEventError) Unwrap() error { return e.cause }

func publicProviderFailure(cause error, model models.Model) error {
	message := "provider response failed"
	var httpError *provider.HTTPError
	switch {
	case errors.As(cause, &httpError):
		message = httpError.Error()
	case errors.Is(cause, context.DeadlineExceeded):
		message = "provider response timed out"
	case errors.Is(cause, provider.ErrInvalidFinalAnswer):
		message = "provider returned an invalid final reply"
	case contextFailure(cause, model):
		message = "provider context or memory limit exceeded"
	}
	return &providerEventError{cause: cause, public: message}
}

func privateProviderCause(err error) error {
	var published *providerEventError
	if errors.As(err, &published) {
		return published.cause
	}
	return err
}
