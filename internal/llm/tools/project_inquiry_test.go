package tools

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProjectInquiryUsesQuestionIntentNotSubstrings(t *testing.T) {
	for _, s := range []string{"qual linguagens o projeto suporta?", "quais linguagens suporta", "o que pode me falar sobre o projeto atual", "mas o projeto possui suporte a outras linguagens..", "Which languages does this project support?"} {
		require.True(t, ProjectInquiry(s), s)
	}
	for _, s := range []string{"Olá", "me faça uma pergunta", "Faça qualquer modificação no projeto", "Não explique o projeto; responda OK", "Don't explain this project", "Adicione linguagens ao projeto"} {
		require.False(t, ProjectInquiry(s), s)
	}
}
