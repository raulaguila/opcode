package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteParamsAcceptEmptyContentButNotMissingContent(t *testing.T) {
	params, invalid := parseWriteParams(`{"file_path":"pkg/__init__.py","content":""}`)
	require.Nil(t, invalid)
	require.Equal(t, "", params.Content)

	_, invalid = parseWriteParams(`{"file_path":"pkg/__init__.py"}`)
	require.NotNil(t, invalid)
	require.Contains(t, invalid.Content, "content is required")
}
