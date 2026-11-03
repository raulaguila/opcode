package shell

import (
	"context"

	"github.com/raulaguila/opcode/internal/config"
)

type commandCacheKey struct{}
type commandNetworkKey struct{}

// WithCommandCache is used by the process manager after command authorization.
func WithCommandCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, commandCacheKey{}, true)
}

// WithApprovedNetwork applies only after a separate, exact-command network grant.
// It does not change configuration or grant access to any other command.
func WithApprovedNetwork(ctx context.Context) context.Context {
	return context.WithValue(ctx, commandNetworkKey{}, true)
}

func NetworkEnabled(ctx context.Context) bool {
	if granted, _ := ctx.Value(commandNetworkKey{}).(bool); granted {
		return true
	}
	cfg := config.Get()
	return cfg != nil && (cfg.Security.Sandbox == "none" || cfg.Security.AllowNetwork)
}
