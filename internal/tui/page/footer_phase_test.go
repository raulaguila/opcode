package page

import (
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/llm/agent"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFooterPhaseFollowsCurrentStreamRepairAndOutcome(t *testing.T) {
	ensureSetupTestConfig(t)
	p := workflowPage(t)
	cfg := config.Get()
	previous := cfg.Agents
	t.Cleanup(func() { cfg.Agents = previous })
	cfg.Agents = map[config.AgentName]config.Agent{config.AgentCoder: {Model: models.GPT41}}
	probe := &footerProbe{store: p.app.Workflows, busy: true, metrics: agent.UsageMetrics{StreamModel: models.GPT41, StreamActive: true}}
	p.app.CoderAgent = probe
	p.progress = config.Text("Consultando modelo", "Contacting model")
	require.Contains(t, p.FooterState().Activity, config.Text("Aguardando modelo", "Awaiting model"))
	probe.metrics.StreamOutputBytes = 100
	require.Contains(t, p.FooterState().Activity, config.Text("Gerando resposta", "Generating response"), "the first real delta already establishes the phase without a rate sample")
	pending := message.Message{Parts: []message.ContentPart{message.TextContent{Text: "answer", CitationStatus: message.CitationPending}}}
	p.progress = ActivityForMessage(pending)
	require.Contains(t, p.FooterState().Activity, config.Text("Gerando resposta", "Generating response"), "stale pending metadata cannot hide current deltas")
	probe.metrics.StreamActive = false
	require.Equal(t, config.Text("Validando referências", "Validating references"), p.FooterState().Activity)
	probe.metrics.StreamActive, probe.metrics.StreamOutputBytes = true, 0
	require.Contains(t, p.FooterState().Activity, config.Text("Aguardando modelo", "Awaiting model"), "a new repair request can truthfully wait again")
	probe.metrics.StreamOutputBytes = 1
	require.Contains(t, p.FooterState().Activity, config.Text("Gerando resposta", "Generating response"))
	probe.busy, probe.metrics.StreamActive = false, false
	p.outcome = "completed"
	p.progress = ""
	require.Equal(t, config.Text("Concluído", "Completed"), p.FooterState().Activity)
}
