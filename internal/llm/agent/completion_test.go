package agent

import (
	"context"
	"github.com/raulaguila/opcode/internal/llm/provider"
	"github.com/raulaguila/opcode/internal/llm/tools"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/raulaguila/opcode/internal/workflow"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestImplementationCannotFinishWithoutChanges(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	done, err := a.Run(context.Background(), "s", "Corrija o arquivo main.go")
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		call := nextGeneration(t, p)
		if i > 0 {
			require.Contains(t, call.history[len(call.history)-1].Content().String(), "no file changes")
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
func TestChangeIntentIsBookkeepingAndDoesNotMisclassifyQuestions(t *testing.T) {
	for _, request := range []string{"Pode corrigir o arquivo?", "Implemente x", "Please update config", "Faça tudo", "Crie o teste", "Quero que você corrija o arquivo", "Renomeie o símbolo"} {
		require.True(t, requiresFileChanges(request), request)
	}
	for _, request := range []string{"Como corrigir o arquivo?", "O que falta implementar?", "Planeje uma alteração", "Explique edit", "Faça uma nova análise", "Faça uma lista", "Crie um plano", "Create a plan"} {
		require.False(t, requiresFileChanges(request), request)
	}
}

func TestSecondaryImperativesAndLiveEditFixtureRequireChanges(t *testing.T) {
	for _, request := range []string{
		"Leia counter.py e corrija add para somar seus argumentos, usando return a + b. Altere somente esse arquivo. Não é necessário executar comandos.",
		"Read counter.py and fix add to sum its arguments.",
		"Leia o código. Corrija a função add.",
		"Tarefas:\n- Leia o arquivo\n- Corrija a função add",
		"Tarefas:\n1. Leia o arquivo\n2. Implemente a validação",
	} {
		require.True(t, requiresFileChanges(request), request)
	}
	for _, request := range []string{
		"Como ler counter.py e corrigir a função?", "Explain how to read and fix counter.py", "Planeje a alteração:\n- Corrija add", "Leia counter.py e explique a operação errada, sem editar arquivos.", "Read and fix nothing; do not edit files", "O que falta?\n- Corrija o arquivo seria uma opção?",
	} {
		require.False(t, requiresFileChanges(request), request)
	}
}

func TestLiveEditFixtureLoadsEditAndRejectsUnchangedFinal(t *testing.T) {
	a, p, _, _ := runtimeAgent(t, workflow.Execute)
	a.tools = []tools.BaseTool{&countedTool{name: "edit"}}
	const request = "Leia counter.py e corrija add para somar seus argumentos, usando return a + b. Altere somente esse arquivo. Não é necessário executar comandos."
	done, err := a.Run(context.Background(), "s", request)
	require.NoError(t, err)
	first := nextGeneration(t, p)
	require.Contains(t, toolNames(first), "edit")
	first.reply <- provider.ProviderResponse{Content: "Corrigido.", FinishReason: message.FinishReasonEndTurn}
	repair := nextGeneration(t, p)
	require.Contains(t, repair.history[len(repair.history)-1].Content().String(), "no file changes")
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
	for _, result := range ordinary.history[len(ordinary.history)-1].ToolResults() {
		require.True(t, result.IsError)
	}
	ordinary.reply <- provider.ProviderResponse{Content: "Plano salvo para revisar igarape-42.", FinishReason: message.FinishReasonEndTurn}
	for i := 0; i < 2; i++ {
		repair := nextGeneration(t, p)
		require.Contains(t, repair.history[len(repair.history)-1].Content().String(), "no valid objective and steps were saved")
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
	a, p, _, _ := runtimeAgent(t, workflow.Plan)
	done, err := a.Run(context.Background(), "s", "Explique as opções para organizar este projeto.")
	require.NoError(t, err)
	first := nextGeneration(t, p)
	first.reply <- provider.ProviderResponse{Content: "Estas são as opções.", FinishReason: message.FinishReasonEndTurn}
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
	require.Contains(t, repair.history[len(repair.history)-1].Content().String(), "structured saved plan")
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
