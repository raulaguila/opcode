package lsp

import (
	"bufio"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type discardWriteCloser struct{ io.Writer }

func (discardWriteCloser) Close() error { return nil }
func TestCallRespectsDeadlineWhenServerNeverResponds(t *testing.T) {
	client := &Client{stdin: discardWriteCloser{Writer: io.Discard}, handlers: map[int32]chan *Message{}, transportDone: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := client.Call(ctx, "textDocument/definition", map[string]any{}, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, client.handlers)
}
func TestConcurrentWritesRemainFramed(t *testing.T) {
	reader, writer := io.Pipe()
	client := &Client{stdin: writer}
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			require.NoError(t, client.writeMessage(context.Background(), &Message{JSONRPC: "2.0", Method: "test"}))
		}()
	}
	input := bufio.NewReader(reader)
	for i := 0; i < 20; i++ {
		message, err := ReadMessage(input)
		require.NoError(t, err)
		require.Equal(t, "test", message.Method)
	}
	group.Wait()
	reader.Close()
	writer.Close()
}
func TestMalformedOversizedProtocolMessagesFail(t *testing.T) {
	for _, header := range []string{"Content-Length: -1\r\n\r\n", "Content-Length: 900000000\r\n\r\n", "Content-Length: 0\r\n\r\n"} {
		_, err := ReadMessage(bufio.NewReader(strings.NewReader(header)))
		require.Error(t, err)
	}
	require.Equal(t, `'it'\''s'`, quoteCommand("it's"))
}
