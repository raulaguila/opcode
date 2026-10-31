package tools

import (
	"github.com/stretchr/testify/require"
	"strings"
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

func TestProjectReviewHintsStayBoundedAndSeparateEditsFromAdvice(t *testing.T) {
	for _, question := range []string{
		"sugira melhorias no próprio código do opcode",
		"Suggest improvements to this project", "Review the current project",
		"Review SetMode for complexity",
		"Faça uma revisão do código de auth.go",
		"Quais melhorias podemos fazer neste projeto?",
		"Não altere arquivos; sugira melhorias no código",
	} {
		t.Run(question, func(t *testing.T) {
			query := ProjectReviewQuery(question)
			require.NotEmpty(t, query)
			require.LessOrEqual(t, len(query), 500)
			require.LessOrEqual(t, len(strings.Fields(query)), 12)
			require.True(t, ProjectInquiry(question))
			require.False(t, ProjectOverview(question))
		})
	}
	require.Equal(t, "request run execute", ProjectReviewQuery("sugira melhorias no próprio código do opcode"))
	require.Equal(t, "request run execute", ProjectReviewQuery("Suggest improvements to this PROJECT OPCODE"))
	require.Contains(t, ProjectReviewQuery("Review SetMode for complexity"), "SetMode")
	require.Equal(t, ProjectReviewQuery("Review SetMode for complexity"), ProjectEvidenceQuery("Review SetMode for complexity"))
	require.Equal(t, "language languages linguagens parser support", ProjectEvidenceQuery("Analise as linguagens do projeto"))
	require.Equal(t, ProjectFlowQuery("Review SetMode; analyze the request flow"), ProjectEvidenceQuery("Review SetMode; analyze the request flow"))
	for _, question := range []string{
		"Olá", "me faça uma pergunta", "Implemente melhorias no projeto", "Adicione testes para SetMode", "Improve this project",
		"Não sugira melhorias no projeto", "Don't review SetMode", "Do not suggest improvements to this project", "Não quero sugestões no código", "Não faça uma revisão do código", "Não quero que sugira melhorias no projeto",
		"Sugira melhorias para minha saúde", "Review my holiday plan", "Suggest improvements to my breakfast",
	} {
		require.Empty(t, ProjectReviewQuery(question), question)
	}
}

func TestTestSourceClassificationMatchesStructuralMap(t *testing.T) {
	for _, path := range []string{"tests/unit.go", "test/service.py", "src/service_test.go", "src/service.test.ts", "spec/service.spec.ts", "test_service.py"} {
		require.True(t, IsTestSource(path), path)
	}
	for _, path := range []string{"service.go", "docs/testing.md", "src/service.py", "contest.go"} {
		require.False(t, IsTestSource(path), path)
	}
}

func TestProjectReviewQueryCapsTaskTermsAndIdentifiers(t *testing.T) {
	query := ProjectReviewQuery("Review SetMode in this project for " + strings.Repeat("VeryLongIdentifierName ", 1000))
	require.Contains(t, query, "SetMode")
	require.LessOrEqual(t, len(query), 500)
	require.LessOrEqual(t, len(strings.Fields(query)), 12)
}

func TestGenericReviewMetricsUseExecutableSeedAndPreserveSpecificTopics(t *testing.T) {
	for _, question := range []string{
		"Sugira melhorias de lógica e redução de complexidade no próprio código do Opcode",
		"Suggest improvements to this project for complexity and readability",
		"Sugira melhorias de refatoração no código do projeto",
	} {
		require.Equal(t, "request run execute", ProjectReviewQuery(question), question)
	}
	query := ProjectReviewQuery("Review SetMode for complexity")
	require.Contains(t, query, "SetMode")
	require.NotContains(t, query, "complexity")
	query = ProjectReviewQuery("Sugira melhorias no SetMode para reduzir complexidade")
	require.Contains(t, query, "SetMode")
	require.NotContains(t, query, "complexidade")
	query = ProjectReviewQuery("Sugira melhorias no estado, modo e concorrência do projeto")
	for _, term := range []string{"state", "mode", "concurrency"} {
		require.Contains(t, strings.Fields(query), term)
	}
}
