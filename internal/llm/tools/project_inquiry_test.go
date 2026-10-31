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

func TestProjectFlowQuestionsUseBoundedTaskTerms(t *testing.T) {
	for _, question := range []string{"quero que analise o fluxo de chat e chamada de tools", "Trace the payment flow and cancellation", "Explique o fluxo de autorização do projeto", "Analyze the executeToolBatch pipeline", "Qual dispatchTool é realmente chamado por Submit neste projeto? Leia o caminho de execução e explique os retornos.", "Which handler is used? Trace its execution path.", "Explique a call chain de Submit"} {
		require.NotEmpty(t, ProjectFlowQuery(question), question)
		require.True(t, ProjectInquiry(question), question)
		require.False(t, ProjectOverview(question), question)
		require.Equal(t, ProjectFlowQuery(question), ProjectEvidenceQuery(question))
	}
	for _, question := range []string{"Adicione um fluxo ao projeto", "Não analise o fluxo de chat", "Don't analyze this project flow", "me faça uma pergunta", "Explain this project", "How do flowers grow?"} {
		require.Empty(t, ProjectFlowQuery(question), question)
	}
	require.Contains(t, ProjectFlowQuery("analise o fluxo de conversa e ferramentas"), "conversation")
	require.Contains(t, ProjectFlowQuery("analise o fluxo de conversa e ferramentas"), "tool")
}
