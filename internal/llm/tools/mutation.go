package tools

import "context"

type mutationKey struct{}

// WithMutationObserver attaches task invalidation to successful file commits,
// including approved paths outside the workspace and partial tool failures.
func WithMutationObserver(ctx context.Context, observer func([]FileChange) error) context.Context {
	return context.WithValue(ctx, mutationKey{}, observer)
}
func notifyMutation(ctx context.Context, changes []FileChange) error {
	if observer, ok := ctx.Value(mutationKey{}).(func([]FileChange) error); ok {
		return observer(changes)
	}
	return nil
}
