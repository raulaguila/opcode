//go:build !windows

package shell

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Reproduce Darwin pty.Open's blocking os.NewFile wrapper on any Unix host.
// This catches the bug on Linux too, independently of Linux pty.Open's pollable
// os.OpenFile implementation or timing of a particular child shell.
func blockingNewFilePipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	descriptor, err := unix.FcntlInt(reader.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	require.NoError(t, reader.Close())
	require.NoError(t, err)
	original := os.NewFile(uintptr(descriptor), "blocking-new-file-pipe")
	require.NotNil(t, original)
	t.Cleanup(func() { _ = original.Close(); _ = writer.Close() })
	require.ErrorIs(t, original.SetReadDeadline(time.Time{}), os.ErrNoDeadline, "fixture must reproduce a wrapper outside the Go poller")
	return original, writer
}
func TestNativeSmokePTYMasterTransferRegistersBlockingNewFileWithPoller(t *testing.T) {
	original, writer := blockingNewFilePipe(t)
	reader, err := pollablePTYMaster(original)
	require.NoError(t, err)
	defer reader.Close()
	require.NoError(t, reader.SetReadDeadline(time.Now().Add(time.Second)))
	_, err = original.Read(make([]byte, 1))
	require.ErrorIs(t, err, os.ErrClosed, "old wrapper must no longer own a live descriptor")
	type result struct {
		n    int
		err  error
		data string
	}
	done := make(chan result, 1)
	go func() {
		buffer := make([]byte, 32)
		n, err := reader.Read(buffer)
		done <- result{n, err, string(buffer[:n])}
	}()
	select {
	case result := <-done:
		t.Fatalf("read returned before data arrived: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}
	_, err = writer.Write([]byte("PTY_POLL_INPUT"))
	require.NoError(t, err)
	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, "PTY_POLL_INPUT", result.data)
	case <-time.After(time.Second):
		t.Fatal("registered reader did not receive input")
	}
}
func TestNativeSmokePTYMasterTransferCloseInterruptsPollingRead(t *testing.T) {
	original, _ := blockingNewFilePipe(t)
	reader, err := pollablePTYMaster(original)
	require.NoError(t, err)
	defer reader.Close()
	require.NoError(t, reader.SetReadDeadline(time.Time{}))
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(started); _, err := reader.Read(make([]byte, 1)); done <- err }()
	<-started
	select {
	case err := <-done:
		t.Fatalf("read ended before close: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	require.NoError(t, reader.Close())
	select {
	case err := <-done:
		require.ErrorIs(t, err, os.ErrClosed)
	case <-time.After(time.Second):
		t.Fatal("close did not interrupt registered reader")
	}
}
