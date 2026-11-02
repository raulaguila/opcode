package interaction

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuestionNotesFollowChoicesAndStayShort(t *testing.T) {
	q, err := DecodeQuestion(`{"question":"Quais?","choices":["Testes","IDs"],"notes":["recomendado"]}`)
	require.NoError(t, err)
	require.Equal(t, []string{"recomendado"}, q.Notes)
	_, err = DecodeQuestion(`{"question":"Quais?","choices":["Testes"],"notes":["a","b"]}`)
	require.Error(t, err, "a note per choice at most")
	q.SessionID = "s"
	_, err = normalizeQuestion(q)
	require.NoError(t, err)
	q.Notes = []string{string(make([]byte, 161))}
	_, err = normalizeQuestion(q)
	require.Error(t, err)
}
