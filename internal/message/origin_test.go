package message

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestApplicationOriginRoundTrip(t *testing.T) {
	parts := []ContentPart{TextContent{Text: "repair", Origin: "application"}}
	raw, err := marshallParts(parts)
	require.NoError(t, err)
	parts, err = unmarshallParts(raw)
	require.NoError(t, err)
	m := Message{Role: User, Parts: parts}
	require.True(t, m.IsApplication())
	require.Equal(t, "repair", m.Content().Text)
	m.AppendContent(" instruction")
	require.True(t, m.IsApplication())
	m.Parts = []ContentPart{TextContent{Text: "Application validation: user"}}
	require.False(t, m.IsApplication())
}
