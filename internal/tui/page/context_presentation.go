package page

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
)

// Keep the request lifecycle and stale-result checks of all workspace panels.
func (p *chatPage) workspaceDetailedAsync(run func(context.Context) (string, string, error)) tea.Cmd {
	var details string
	cmd := p.workspaceAsync(func(ctx context.Context) (string, bool, error) {
		body, extra, err := run(ctx)
		details = extra
		return body, false, err
	})
	return func() tea.Msg {
		result := cmd().(WorkspaceResultMsg)
		result.details = details
		return result
	}
}

func contextSummary(status agent.ContextStatus) string {
	decision := config.Text("Há espaço estimado para continuar.", "Estimated room to continue.")
	if !status.Fits {
		decision = config.Text("O limite previsto foi excedido. Compacte ou escolha outro modelo antes de continuar.", "The estimated limit is exceeded. Compact or choose another model before continuing.")
	}
	total := status.EstimatedInputTokens + status.ReservedOutputTokens + status.SafetyTokens
	source := config.Text("metadados disponíveis", "available metadata")
	switch {
	case strings.Contains(status.ContextSource, "learned"):
		source = config.Text("limite aprendido nas respostas do servidor", "limit learned from server responses")
	case strings.Contains(status.ContextSource, "parameter"):
		source = config.Text("parâmetro configurado no servidor", "configured server parameter")
	case strings.Contains(status.ContextSource, "loaded"):
		source = config.Text("alocação informada pelo servidor", "allocation reported by the server")
	case strings.Contains(status.ContextSource, "model"):
		source = config.Text("capacidade anunciada pelo modelo", "advertised model capacity")
	case strings.Contains(status.ContextSource, "configuration"):
		source = config.Text("configuração explícita", "explicit configuration")
	case strings.Contains(status.ContextSource, "fallback"):
		source = config.Text("estimativa alternativa; provedor não informou", "fallback estimate; provider did not report it")
	}
	return decision + "\n" + fmt.Sprintf(config.Text("Previsto: %d de %d tokens · reserva de resposta: %d\nOrigem: %s (%s)\n\nO limite efetivo prepara a próxima requisição. A capacidade do modelo e a alocação atual no Ollama podem diferir; consulte 3 Alocação. O servidor ainda pode impor um limite menor.", "Estimated: %d of %d tokens · output reserve: %d\nSource: %s (%s)\n\nThe effective limit prepares the next request. Model capacity and the current Ollama allocation can differ; see 3 Allocation. The server may still impose a lower limit."), total, status.ContextWindow, status.ReservedOutputTokens, source, status.ContextSource)
}
