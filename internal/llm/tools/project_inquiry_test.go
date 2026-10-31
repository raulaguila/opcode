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

func TestProjectOverviewSeparatesBroadQuestionsFromSpecificFacts(t *testing.T) {
	for _, question := range []string{"o que pode me falar sobre esse projeto?", "Fale sobre o projeto atual", "Descreva o repositório", "Give me an overview of this project", "What can you tell me about this repo?"} {
		require.True(t, ProjectOverview(question), question)
	}
	for _, question := range []string{"qual o nome do projeto?", "quais linguagens o projeto suporta?", "explique main.go neste projeto", "Não explique o projeto; responda OK", "Faça uma alteração no projeto", "me faça uma pergunta"} {
		require.False(t, ProjectOverview(question), question)
	}
}
