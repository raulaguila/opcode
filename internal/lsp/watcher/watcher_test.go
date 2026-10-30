package watcher

import (
	"context"
	"errors"
	"testing"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/require"
)

type collidingContext struct{ context.Context }

func (c collidingContext) Value(key any) any {
	if key == "serverName" {
		return "untrusted-string-key"
	}
	return c.Context.Value(key)
}

func TestServerNameContextUsesPrivateTypedKey(t *testing.T) {
	ctx := WithServerName(collidingContext{context.Background()}, "PyRight")
	require.Equal(t, "pyright", getServerNameFromContext(ctx))
	require.Equal(t, "untrusted-string-key", ctx.Value("serverName"))
}

func TestWatchWorkspaceReturnsWhenNativeWatcherCannotBeCreated(t *testing.T) {
	watcher := NewWorkspaceWatcher(nil)
	called := 0
	watcher.watcherFactory = func() (*fsnotify.Watcher, error) {
		called++
		return nil, errors.New("fixture watcher creation failure")
	}
	ctx := WithServerName(context.Background(), "fixture")
	require.NotPanics(t, func() { watcher.WatchWorkspace(ctx, t.TempDir()) })
	require.Equal(t, 1, called)
}
