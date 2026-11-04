package ui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSandboxResult(t *testing.T) {
	m := &Model{}
	m.sandboxResult(sandboxMsg{})
	require.Empty(t, m.notice, "a working sandbox says nothing at startup")

	m.sandboxResult(sandboxMsg{err: errors.New("cannot start")})
	require.Contains(t, m.notice, "/sandbox")
	require.Nil(t, m.dialog, "startup does not interrupt")

	m.sandboxResult(sandboxMsg{err: errors.New("cannot start"), asked: true})
	require.Equal(t, sandboxDialog{"cannot start"}, m.dialog)
	m.width = 60
	require.Contains(t, sandboxDialog{"cannot start"}.view(m), "cannot start")

	m.dialog = nil
	m.sandboxResult(sandboxMsg{asked: true})
	require.Empty(t, m.notice, "a host fixed now clears the warning")
	require.NotEmpty(t, m.info)
}
