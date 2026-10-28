package tools

import "context"

type mutationKey struct{}

// WithMutationObserver attaches task invalidation to successful file commits,
// including approved paths outside the workspace and partial tool failures.
func WithMutationObserver(ctx context.Context, observer func() error) context.Context {
	return context.WithValue(ctx, mutationKey{}, observer)
}
func notifyMutation(ctx context.Context) error {
	if observer, ok := ctx.Value(mutationKey{}).(func() error); ok {
		return observer()
	}
	return nil
}
