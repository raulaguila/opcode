package interaction

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeQuestionRepairsOnlyEncodedChoiceArrays(t *testing.T) {
	for _, raw := range []string{
		`{"question":"me faça uma pergunta","choices":"[\"Sim\", \"Não\"]","explanation":""}`,
		`{"question":"me faça uma pergunta","choices":["Sim","Não"]}`,
	} {
		q, err := DecodeQuestion(raw)
		require.NoError(t, err)
		require.Equal(t, []string{"Sim", "Não"}, q.Choices)
		require.Equal(t, "me faça uma pergunta", q.Question)
	}
	q, err := DecodeQuestion(`{"questions":[{"question":"Recursos?","type":"multi_choice","choices":"[\"A\",\"B\"]"}]}`)
	require.NoError(t, err)
	require.Equal(t, []string{"A", "B"}, q.Questions[0].Choices)
	for _, raw := range []string{
		`null`, `[]`, `{"choices":"Sim,Não"}`, `{"choices":"null"}`,
		`{"choices":"[1,2]"}`, `{"choices":{}}`, `{"choices":[null]}`,
		`{"choices":"\"[\\\"A\\\"]\""}`, `{"choices":["1","2","3","4","5","6"]}`,
		`{"questions":[null]}`, `{"question":42}`, `{"choices":["` + strings.Repeat("x", 301) + `"]}`,
		strings.Repeat(" ", 32<<10+1),
	} {
		_, err := DecodeQuestion(raw)
		require.Error(t, err, raw[:min(100, len(raw))])
		require.NotContains(t, err.Error(), "unmarshal", "do not echo raw payloads through decoder errors")
	}
}
