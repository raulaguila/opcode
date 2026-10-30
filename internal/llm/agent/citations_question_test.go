package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/raulaguila/opcode/internal/interaction"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func questionDiagnosis(t *testing.T, diagnosis string) (*agent, *controlledProvider, *memoryMessages, <-chan AgentEvent, string) {
	t.Helper()
	a, p, saved, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# Opcode\nTerminal development agent.\n"), 0600))
	a.tools = []tools.BaseTool{tools.NewViewTool(nil)}
	a.questions.SetInteractive(true)
	done, err := a.Run(context.Background(), "s", "Explique o projeto atual e pergunte se quero implementar as melhorias.")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	first.reply <- provider.ProviderResponse{Content: diagnosis, ToolCalls: []message.ToolCall{toolCall("ask_user_question", `{"question":"Quer implementar as melhorias?","choices":["Sim","Agora não"]}`)}, FinishReason: message.FinishReasonToolUse}
	var q interaction.Question
	require.Eventually(t, func() bool { var ok bool; q, ok = a.questions.Pending("s"); return ok }, time.Second, time.Millisecond)
	require.NoError(t, a.questions.Answer("s", q.ID, "Agora não"))
	return a, p, saved, done, root
}

func TestQuestionAcknowledgmentPreservesValidatedDiagnosis(t *testing.T) {
	_, p, saved, done, _ := questionDiagnosis(t, "O projeto é um agente de desenvolvimento para terminal (README.md:1-2).")
	last := nextGeneration(t, p)
	last.reply <- provider.ProviderResponse{Content: "Perfeito — fica o diagnóstico. Não farei alterações agora.", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Empty(t, result.Message.CitationValidation(), "a neutral acknowledgment has no citation obligation of its own")
	history, err := saved.List(context.Background(), "s")
	require.NoError(t, err)
	var diagnosisFound bool
	for _, item := range history {
		if item.Content().Text == "O projeto é um agente de desenvolvimento para terminal (README.md:1-2)." {
			diagnosisFound = true
			require.Equal(t, message.CitationValid, item.CitationValidation())
		}
	}
	require.True(t, diagnosisFound)
}

func TestQuestionAcknowledgmentCannotConcealUnverifiedDiagnosis(t *testing.T) {
	_, p, saved, done, _ := questionDiagnosis(t, "O projeto é um agente para terminal.")
	nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "Perfeito — fica o diagnóstico.", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	require.Contains(t, repair.history[len(repair.history)-1].Content().Text, "README.md")
	repair.reply <- provider.ProviderResponse{Content: "O projeto é um agente para terminal (README.md:1-2). Nenhuma alteração foi feita.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	history, err := saved.List(context.Background(), "s")
	require.NoError(t, err)
	for _, item := range history {
		require.Empty(t, item.RevisionOf(), "an intermediate diagnosis cannot infer a final-answer revision relationship")
		if item.Content().Text == "O projeto é um agente para terminal." {
			require.Equal(t, message.CitationInvalid, item.CitationValidation(), "a new valid answer cannot prove that an earlier diagnosis was corrected")
		}
		if item.Content().Text == "Perfeito — fica o diagnóstico." {
			require.Empty(t, item.CitationValidation(), "the diagnosis's failure must not be assigned to the acknowledgment")
		}
	}
}

func TestQuestionAcknowledgmentWithNewClaimsStillNeedsCitations(t *testing.T) {
	_, p, _, done, _ := questionDiagnosis(t, "O projeto é um agente para terminal (README.md:1-2).")
	nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "Perfeito. O projeto só suporta a linguagem Go.", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	require.Contains(t, repair.history[len(repair.history)-1].Content().Text, "No file:line reference")
	repair.reply <- provider.ProviderResponse{Content: "O README descreve um agente para terminal (README.md:1-2). Não farei alterações agora.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

func TestPriorDiagnosisFailureKeepsCurrentCandidateValidationIndependent(t *testing.T) {
	for _, test := range []struct {
		name, text, state string
	}{
		{"neutral", "Perfeito — fica o diagnóstico.", ""},
		{"new verified claim", "O nome é Opcode (README.md:1).", message.CitationValid},
		{"new unverified claim", "Perfeito. O projeto só suporta Go.", message.CitationInvalid},
		{"mixed malicious reference", "Perfeito — fica o diagnóstico. secret.go:999", message.CitationInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, p, _, done, _ := questionDiagnosis(t, "O projeto só suporta Go.")
			nextGeneration(t, p).reply <- provider.ProviderResponse{Content: test.text, FinishReason: message.FinishReasonEndTurn}
			repair := nextGeneration(t, p)
			var found bool
			for _, item := range repair.history {
				if item.Content().Text == test.text {
					found = true
					require.Equal(t, test.state, item.CitationValidation())
				}
				if item.Content().Text == "O projeto só suporta Go." {
					require.Equal(t, message.CitationInvalid, item.CitationValidation())
				}
			}
			require.True(t, found)
			repair.reply <- provider.ProviderResponse{Content: "O README descreve um agente para terminal (README.md:1-2).", FinishReason: message.FinishReasonEndTurn}
			require.NoError(t, finishGeneration(t, done).Error)
		})
	}
}

func TestQuestionAcknowledgmentRevalidatesDiagnosisAfterFileChange(t *testing.T) {
	_, p, _, done, root := questionDiagnosis(t, "O projeto é um agente para terminal (README.md:1-2).")
	last := nextGeneration(t, p)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# Changed\nDifferent snapshot.\n"), 0600))
	last.reply <- provider.ProviderResponse{Content: "Perfeito — fica o diagnóstico.", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	repair.reply <- provider.ProviderResponse{Content: "O projeto é um agente para terminal (README.md:1-2).", FinishReason: message.FinishReasonEndTurn}
	require.ErrorIs(t, finishGeneration(t, done).Error, ErrSourceCitation)
}

func TestCitationAcknowledgmentIsClosedConversationalGrammar(t *testing.T) {
	for _, text := range []string{"Perfeito — fica o diagnóstico.", "Entendido. Não farei alterações agora.", "Understood. No changes will be made now."} {
		require.True(t, citationAcknowledgment.MatchString(text), text)
	}
	for _, text := range []string{"Perfeito. O projeto só suporta Go.", "OK. source.go:999", "Obrigado, o arquivo não existe.", "Certo; a validação está correta.", "No changes will be made now because the code is safe."} {
		require.False(t, citationAcknowledgment.MatchString(text), text)
	}
}

func TestUnrelatedRepairCannotSupersedeEarlierDiagnosis(t *testing.T) {
	_, p, saved, done, _ := questionDiagnosis(t, "O projeto só suporta a linguagem Go.")
	nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "Perfeito — fica o diagnóstico.", FinishReason: message.FinishReasonEndTurn}
	nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "O nome do projeto é Opcode (README.md:1).", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	history, err := saved.List(context.Background(), "s")
	require.NoError(t, err)
	for _, item := range history {
		if item.Content().Text == "O projeto só suporta a linguagem Go." {
			require.Equal(t, message.CitationInvalid, item.CitationValidation())
		}
	}
}

func TestQuestionAcknowledgmentCannotAddUnverifiedReference(t *testing.T) {
	_, p, _, done, _ := questionDiagnosis(t, "O projeto é um agente para terminal (README.md:1-2).")
	nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "Perfeito — fica o diagnóstico. secret.go:999", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	require.Contains(t, repair.history[len(repair.history)-1].Content().Text, "Unverified references")
	repair.reply <- provider.ProviderResponse{Content: "Perfeito. README.md:1-2", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

func TestLegacyHumanDeclineStartsIndependentCitationScope(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# Opcode\nTerminal agent.\n"), 0600))
	a.tools = []tools.BaseTool{tools.NewViewTool(nil)}
	done, err := a.Run(context.Background(), "s", "Explique o projeto atual.")
	require.NoError(t, err)
	nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "O projeto é um agente (README.md:1-2). Quer implementar as melhorias?", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	done, err = a.Run(context.Background(), "s", "Agora não.")
	require.NoError(t, err)
	nextGeneration(t, p).reply <- provider.ProviderResponse{Content: "Perfeito — fica o diagnóstico.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
}

func TestInterruptedToolMessageDoesNotRemainPendingCitationValidation(t *testing.T) {
	a, _, saved, _ := runtimeAgent(t, workflow.Ask)
	item, err := saved.Create(context.Background(), "s", message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Ainda será verificado.", CitationStatus: message.CitationPending}}})
	require.NoError(t, err)
	failure := context.Canceled
	var result *message.Message
	a.preserveInterruptedStream("s", &item, &result, &failure)
	history, err := saved.List(context.Background(), "s")
	require.NoError(t, err)
	require.Len(t, history, 1)
	restored := history[0]
	require.Equal(t, message.CitationInvalid, restored.CitationValidation())
	require.Equal(t, message.FinishReasonCanceled, restored.FinishReason())
}

func TestWorkflowRepairResolvesPreviousCitationBadge(t *testing.T) {
	a, _, saved, root := runtimeAgent(t, workflow.Ask)
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.go"), []byte("package source\n"), 0600))
	history, _ := citationHistory(t, "view", `{"file_path":"source.go"}`)
	for _, text := range []string{"source.go:1", "Uncited explanation"} {
		item, err := saved.Create(context.Background(), "s", message.CreateMessageParams{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: text, CitationStatus: message.CitationPending}}})
		require.NoError(t, err)
		g := generationState{agent: a, ctx: context.Background(), run: &generationRun{requiresCitation: true}, history: append(history, item)}
		require.NoError(t, g.resolvePendingCitation(item))
		for _, current := range g.history {
			if current.ID == item.ID {
				require.NotEqual(t, message.CitationPending, current.CitationValidation())
				if text == "source.go:1" {
					require.Equal(t, message.CitationValid, current.CitationValidation())
				} else {
					require.Equal(t, message.CitationInvalid, current.CitationValidation())
				}
			}
		}
	}
}
