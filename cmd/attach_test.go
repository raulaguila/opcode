package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/stretchr/testify/require"
)

type failingAttachReader struct{ err error }

func (r failingAttachReader) Read([]byte) (int, error) { return 0, r.err }

func TestAttachLoopReportsInputErrorsWithoutSendingCommands(t *testing.T) {
	const secret = "attach-input-private-credential"
	config.RegisterRuntimeCredentials(t.Name(), secret)
	t.Cleanup(func() { config.RegisterRuntimeCredentials(t.Name()) })
	tests := []struct {
		name  string
		input io.Reader
		want  string
	}{
		{"reader failure", io.MultiReader(strings.NewReader("/help\n"), failingAttachReader{errors.New("read failed: " + secret + "\x1b[31m\x00")}), "read failed:"},
		{"line exceeds 32k", strings.NewReader(strings.Repeat("x", 32000) + secret + "\n"), "token too long"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// A nil client ensures this path cannot issue an RPC. The deadline
			// expires before attach's first polling tick if input regresses.
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			var out bytes.Buffer
			err := attachLoop(ctx, cancel, nil, "session", test.input, &out)
			require.ErrorContains(t, err, test.want)
			require.NotContains(t, err.Error(), secret)
			require.NotContains(t, err.Error(), "\x1b")
			require.NotContains(t, err.Error(), "\x00")
			require.NotContains(t, out.String(), secret)
			if test.name == "reader failure" {
				require.Contains(t, err.Error(), "[REDACTED]")
				require.Contains(t, out.String(), "/approve")
			}
		})
	}
}

func TestAttachLoopPreservesCleanEOFAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	require.NoError(t, attachLoop(ctx, cancel, nil, "session", strings.NewReader("/help\n"), io.Discard))
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	require.NoError(t, attachLoop(ctx, cancel, nil, "session", failingAttachReader{context.Canceled}, io.Discard))
}
