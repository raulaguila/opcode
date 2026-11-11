package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestImplementationCannotFinishWithoutChanges(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	_, err := a.workflow.SetContract(context.Background(), "s", &workflow.Contract{Kind: "change"})
	require.NoError(t, err)
	done, err := a.Run(context.Background(), "s", "Corrija o arquivo main.go")
	require.NoError(t, err)
	for i := range 3 {
		call := nextGeneration(t, p)
		if i > 0 {
			require.Contains(t, lastSent(call.history).Content().String(), "no file changes")
		}
		call.reply <- provider.ProviderResponse{Content: "Corrigido.", FinishReason: message.FinishReasonEndTurn}
	}
	require.ErrorContains(t, finishGeneration(t, done).Error, "no file changes")
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, "blocked", task.Status)
}
func TestExplicitNoChangeReasonAllowsVerifiedExistingBehavior(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	done, err := a.Run(context.Background(), "s", "Implemente a validação")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	replyTools(call, toolCall("finish_task", `{"status":"completed","summary":"Já implementado","no_change_reason":"A validação solicitada já existe; os arquivos foram inspecionados."}`))
	next := nextGeneration(t, p)
	next.reply <- provider.ProviderResponse{Content: "A validação já existe.", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Equal(t, "completed", result.Outcome)
}
func TestPartialTaskIsNotReportedAsCompleted(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	done, err := a.Run(context.Background(), "s", "Implemente a validação")
	require.NoError(t, err)
	call := nextGeneration(t, p)
	replyTools(call, toolCall("finish_task", `{"status":"partial","summary":"Falta uma decisão do usuário"}`))
	next := nextGeneration(t, p)
	next.reply <- provider.ProviderResponse{Content: "Trabalho parcial.", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Equal(t, "partial", result.Outcome)
}
func TestLiveEditFixtureLoadsEditAndRejectsUnchangedFinal(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	a.tools = []tools.BaseTool{&countedTool{name: "edit"}}
	_, err := a.workflow.SetContract(context.Background(), "s", &workflow.Contract{Kind: "change"})
	require.NoError(t, err)
	const request = "Leia counter.py e corrija add para somar seus argumentos, usando return a + b. Altere somente esse arquivo. Não é necessário executar comandos."
	done, err := a.Run(context.Background(), "s", request)
	require.NoError(t, err)
	first := nextGeneration(t, p)
	require.Contains(t, toolNames(first), "edit")
	first.reply <- provider.ProviderResponse{Content: "Corrigido.", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	require.Contains(t, lastSent(repair.history).Content().String(), "no file changes")
	replyTools(repair, toolCall("finish_task", `{"status":"blocked","summary":"Nenhuma alteração foi aplicada"}`))
	final := nextGeneration(t, p)
	final.reply <- provider.ProviderResponse{Content: "A implementação não foi concluída.", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Equal(t, "blocked", result.Outcome)
}

func TestInvalidStructuredPlanCannotFinishAsOrdinarySuccess(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Plan)
	const request = "Leia README.md. Use update_plan para salvar objetivo 'Revisar igarape-42', uma etapa pendente de inspeção e critério de leitura, sem checks. Finalize como planned, sem modificar arquivos."
	done, err := a.Run(context.Background(), "s", request)
	require.NoError(t, err)
	first := nextGeneration(t, p)
	replyTools(first, toolCall("update_plan", `{"objective":"Revisar igarape-42","steps":[{"title":"Inspecionar","status":"pending"}],"checks":[]}`), toolCall("finish_task", `{"status":"planned","summary":"Plano salvo"}`))
	ordinary := nextGeneration(t, p)
	for _, result := range lastSent(ordinary.history).ToolResults() {
		require.True(t, result.IsError)
	}
	ordinary.reply <- provider.ProviderResponse{Content: "Plano salvo para revisar igarape-42.", FinishReason: message.FinishReasonEndTurn}
	for range 2 {
		repair := nextGeneration(t, p)
		require.Contains(t, lastSent(repair.history).Content().String(), "no valid objective and steps were saved")
		repair.reply <- provider.ProviderResponse{Content: "Plano concluído.", FinishReason: message.FinishReasonEndTurn}
	}
	require.ErrorContains(t, finishGeneration(t, done).Error, "no valid objective and steps were saved")
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Empty(t, task.Objective)
	require.Empty(t, task.Steps)
	require.Equal(t, "blocked", task.Status)
}
func TestOrdinaryPlanAdviceDoesNotRequireInventedWorkflow(t *testing.T) {
	a, p, _, root := runtimeAgent(t, workflow.Plan)
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# Project\nA small application.\n"), 0600))
	a.tools = []tools.BaseTool{tools.NewViewTool(nil)}
	done, err := a.Run(context.Background(), "s", "Explique as opções para organizar este projeto usando @README.md para contexto.")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	first.reply <- provider.ProviderResponse{Content: "Estas são as opções para a aplicação de README.md:1-2.", FinishReason: message.FinishReasonEndTurn}
	require.NoError(t, finishGeneration(t, done).Error)
	require.False(t, requiresStructuredPlan("Explique como usar update_plan para salvar um plano."))
}

func TestStructuredPlanRepairCanSaveAndFinishNormally(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Plan)
	done, err := a.Run(context.Background(), "s", "Use update_plan para salvar um plano estruturado e finalize como planned.")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	first.reply <- provider.ProviderResponse{Content: "Plano pronto.", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	require.Contains(t, lastSent(repair.history).Content().String(), "structured saved plan")
	replyTools(repair, toolCall("update_plan", `{"objective":"Revisar igarape-42","steps":[{"title":"Inspecionar","status":"pending"}],"criteria":["Arquivo lido"],"checks":[]}`), toolCall("finish_task", `{"status":"planned","summary":"Plano salvo para revisão"}`))
	final := nextGeneration(t, p)
	final.reply <- provider.ProviderResponse{Content: "Plano salvo para revisão.", FinishReason: message.FinishReasonEndTurn}
	result := finishGeneration(t, done)
	require.NoError(t, result.Error)
	require.Equal(t, "planned", result.Outcome)
	task, err := a.workflow.Get(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, "Revisar igarape-42", task.Objective)
	require.Len(t, task.Steps, 1)
	require.Equal(t, "pending", task.Steps[0].Status)
}
