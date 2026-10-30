package message

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCitationStatePersistsWithoutChangingTextOrLegacyParts(t *testing.T) {
	parts, err := unmarshallParts([]byte(`[{"type":"text","data":{"text":"diagnosis"}}]`))
	require.NoError(t, err)
	m := Message{Role: Assistant, Parts: parts}
	require.Empty(t, m.CitationValidation())
	for _, state := range []string{CitationPending, CitationValid, CitationInvalid, CitationSuperseded} {
		require.True(t, m.SetCitationValidation(state))
		require.False(t, m.SetCitationValidation(state))
		raw, err := marshallParts(m.Parts)
		require.NoError(t, err)
		m.Parts, err = unmarshallParts(raw)
		require.NoError(t, err)
		require.Equal(t, state, m.CitationValidation())
		require.Equal(t, "diagnosis", m.Content().Text)
		var legacy []struct {
			Type string
			Data struct{ Text string }
		}
		require.NoError(t, json.Unmarshal(raw, &legacy))
		require.Equal(t, "diagnosis", legacy[0].Data.Text)
	}
}

func TestStreamingPreservesCitationPresentationMetadata(t *testing.T) {
	m := Message{Parts: []ContentPart{TextContent{Text: "prefix", Origin: "application", CitationStatus: CitationPending}}}
	stream := NewStreamBuffer(&m)
	stream.AppendText(" suffix")
	stream.Materialize()
	require.Equal(t, CitationPending, m.CitationValidation())
	require.Equal(t, "application", m.Content().Origin)
	require.Equal(t, "prefix suffix", m.Content().Text)
}
