//go:build !desktop

package cmd

import (
	"context"
	"errors"
)

// Without the desktop build tag, opcode app opens a browser's app window: the
// binary then needs no GTK or WebKit, as on servers reached through SSH.
const nativeWindowAvailable = false

func nativeWindow(context.Context, string) error {
	return errors.New("built without the desktop tag")
}
