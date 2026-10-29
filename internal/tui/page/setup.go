package page

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/diagnostic"
	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/tui/components/dialog"
	"github.com/raulaguila/opcode/internal/tui/util"
	"github.com/raulaguila/opcode/internal/workflow"
	"strings"
)

func (p *chatPage) openSetup() tea.Cmd {
	if p.app.CoderAgent == nil || p.app.CoderAgent.Model().ID == "" {
		return util.CmdHandler(dialog.OpenProviderDialogMsg{Setup: true})
	}
	m := p.app.CoderAgent.Model()
	p.showWorkspace("setup", config.Text("Configuração · 3/3 · pronto para conversar", "Setup · 3/3 · ready to chat"), fmt.Sprintf(config.Text("Modelo: %s\n\nConfiguração salva. Catálogo e chamada de ferramenta são verificações separadas; consulte Detalhes para testar.", "Model: %s\n\nConfiguration saved. Catalog access and tool calling are separate checks; open Details to test."), panelText(m.APIModel)))
	p.workspace.actions = []workspaceAction{{kind: "setup-done", label: config.Text("Começar a conversar", "Start chatting")}, {kind: "setup-details", label: config.Text("Detalhes e testes opcionais", "Details and optional tests")}}
	return nil
}

func (p *chatPage) openSetupDetails() tea.Cmd {
	path, _ := config.Paths(config.WorkingDirectory())
	p.showWorkspace("setup-details", config.Text("Detalhes da configuração", "Setup details"), config.Text("Configuração: ", "Configuration: ")+panelText(path)+"\n\n"+config.Text("Testes são opcionais e pedem confirmação antes de enviar requisições reais.", "Tests are optional and require confirmation before sending real requests."))
	for _, v := range []struct{ kind, pt, en string }{{"setup-providers", "1. Rever provedor", "1. Review provider"}, {"setup-models", "2. Escolher outro modelo", "2. Choose another model"}, {"setup-probe", "Teste opcional de ferramenta (confirmação)", "Optional tool probe (confirmation)"}, {"setup-read", "Teste opcional no projeto (confirmação)", "Optional project task (confirmation)"}, {"local-status", "Consultar alocação do Ollama (sem inferência)", "Inspect Ollama allocation (no inference)"}, {"setup-done", "Concluir e conversar", "Finish and chat"}} {
		p.workspace.actions = append(p.workspace.actions, workspaceAction{kind: v.kind, label: config.Text(v.pt, v.en)})
	}
	return nil
}
func (p *chatPage) setupChoose(action string) tea.Cmd {
	switch action {
	case "setup-details":
		return p.openSetupDetails()
	case "setup-providers":
		return util.CmdHandler(dialog.OpenProviderDialogMsg{Setup: true})
	case "setup-models":
		return util.CmdHandler(openModelsMsg{})
	case "setup-done":
		p.closeWorkspace()
		return nil
	case "local-status":
		return p.openLocalStatus()
	case "setup-probe", "setup-read":
		kind := action + "-confirm"
		body := config.Text("Uma requisição real será enviada ao modelo selecionado e poderá haver cobrança. Enter confirma; Esc cancela. Nenhum teste é obrigatório para começar.", "A real request will be sent to the selected model and provider charges may apply. Enter confirms; Esc cancels. No test is required to start.")
		if action == "setup-read" {
			body += "\n" + config.Text("O teste muda esta sessão para Perguntar, lê arquivos reais do projeto e pede uma explicação com referências. Não certifica a qualidade geral do modelo.", "The test switches this session to Ask, reads real project files and requests an explanation with references. It does not certify general model quality.")
		}
		p.showWorkspace(kind, config.Text("Confirmar teste opcional", "Confirm optional test"), body)
		p.workspace.actions = []workspaceAction{{kind: kind, label: config.Text("Executar teste", "Run test")}}
		return nil
	case "setup-probe-confirm":
		m := config.ModelWithProviderDefaults(p.app.CoderAgent.Model())
		connection, err := config.ProviderForModel(m)
		if err != nil {
			return util.ReportError(err)
		}
		p.showWorkspace("setup-probe-result", config.Text("Teste de ferramenta", "Tool probe"), "")
		return p.workspaceAsync(func(ctx context.Context) (string, bool, error) {
			if err := diagnostic.ProbeModel(ctx, m, connection); err != nil {
				return "", false, err
			}
			return config.Text("Chamada estruturada confirmada. Nenhuma ferramenta foi executada. /setup volta ao assistente.", "Structured call confirmed. No tool executed. /setup returns to setup."), false, nil
		})
	case "setup-read-confirm":
		p.closeWorkspace()
		if p.session.ID == "" {
			return tea.Batch(util.CmdHandler(WorkflowCommandMsg{Command: "setup-read"}))
		}
		return p.workflowCommand("setup-read", "")
	}
	return nil
}
func (p *chatPage) openLocalStatus() tea.Cmd {
	m := config.ModelWithProviderDefaults(p.app.CoderAgent.Model())
	connection, err := config.ProviderForModel(m)
	if err != nil {
		return util.ReportError(err)
	}
	if m.Provider != models.ProviderLocal || m.API != "ollama" {
		return util.ReportWarn(config.Text("Selecione um modelo com API nativa do Ollama para consultar a alocação.", "Select a model using the native Ollama API to inspect allocation."))
	}
	p.showWorkspace("local-status", config.Text("Ollama · alocação observada", "Ollama · observed allocation"), "")
	return p.workspaceAsync(func(ctx context.Context) (string, bool, error) {
		found, err := models.InspectOllama(ctx, connection.BaseURL, connection.APIKey)
		if err != nil {
			return "", false, err
		}
		lines := []string{fmt.Sprintf(config.Text("Modelo: %s · contexto configurado: %d", "Model: %s · configured context: %d"), panelText(m.APIModel), m.ContextWindow)}
		matched := false
		for _, loaded := range found {
			if loaded.Name != m.APIModel && loaded.Name != m.APIModel+":latest" {
				continue
			}
			matched = true
			lines = append(lines, fmt.Sprintf(config.Text("Contexto alocado: %d · execução: %s · VRAM usada pelo modelo: %.1f GiB", "Allocated context: %d · placement: %s · model VRAM: %.1f GiB"), loaded.Context, loaded.Placement(), float64(loaded.VRAM)/(1<<30)))
			if loaded.Context > 0 && loaded.Context != m.ContextWindow {
				lines = append(lines, config.Text("O contexto alocado difere do configurado; a próxima geração pode recarregar o modelo. O limite aprendido pode reduzir a próxima requisição.", "Allocated context differs from configuration; the next generation may reload the model. A learned limit may reduce the next request."))
			}
		}
		if !matched {
			lines = append(lines, config.Text("O modelo não está carregado. Não foi carregado nem baixado por esta consulta.", "Model is not loaded. This query did not load or download it."))
		}
		lines = append(lines, config.Text("Mais contexto exige mais memória. Confira limites aprendidos em /context; altere somente em /settings. Estes dados não informam a VRAM livre nem garantem que outra janela caberá.", "More context needs more memory. Inspect learned limits in /context; change settings in /settings. These values do not report free VRAM or guarantee another context will fit."))
		return strings.Join(lines, "\n\n"), false, nil
	})
}
func (p *chatPage) setupRead() (tea.Cmd, error) {
	if err := p.app.CoderAgent.SetMode(context.Background(), p.session.ID, workflow.Ask); err != nil {
		return nil, err
	}
	if _, err := p.app.Workflows.SetContract(context.Background(), p.session.ID, &workflow.Contract{Kind: "explain"}); err != nil {
		return nil, err
	}
	p.refreshWorkflow()
	prompt := config.Text("Leia o README do projeto, se existir, e um arquivo de código relevante usando as ferramentas. Explique em português a finalidade do projeto, cite os arquivos que realmente leu e diga o que não pôde confirmar. Não invente fatos.", "Read the project README, if present, and one relevant source file using tools. Explain the project purpose, cite files you actually read and state what you could not verify. Do not invent facts.")
	return p.submitAgentMessage(prompt, nil, prompt), nil
}
