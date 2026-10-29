package dialog

import (
	"github.com/raulaguila/opcode/internal/config"
	"strings"
)

// Translate only application-owned copy. User text, provider errors and model
// output are never run through a replacement pass.
func interfaceCopy(english string) string {
	if english == "TLS insecure" {
		return config.Text("TLS inseguro", english)
	}
	if strings.HasPrefix(english, "› ") {
		return "› " + interfaceCopy(strings.TrimPrefix(english, "› "))
	}
	portuguese := map[string]string{
		"Authentication": "Autenticação", "API key": "Chave da API",
		"Connection active": "Conexão ativa", "Backend": "Provedor", "Transport": "Transporte", "Disabled": "Desativado", "Initial mode": "Modo inicial", "Resume most recent session": "Retomar conversa recente", "Show process by default": "Mostrar processo ao iniciar", "OAuth enabled": "OAuth ativo",
		"previous theme": "tema anterior", "next theme": "próximo tema", "select theme": "escolher tema", "switch options": "trocar opção", "confirm": "confirmar", "yes": "sim", "no": "não", "toggle selection": "trocar seleção", "yes/no": "sim/não", "delete attachment at index i": "remover anexo i", "cancel delete mode": "cancelar remoção", "delete all attchments": "remover todos os anexos",
		"Ctrl+P settings · Ctrl+A role · Ctrl+R catalog · Ctrl+T probe · Esc":             "Ctrl+P ajustes · Ctrl+A função · Ctrl+R catálogo · Ctrl+T teste · Esc",
		"Configure a connection once; choose its model with Ctrl+O.":                      "Configure uma conexão; escolha o modelo com Ctrl+O.",
		"Ctrl+T tests metadata only; Ctrl+S saves. Credentials stay in your environment.": "Ctrl+T consulta metadados; Ctrl+S salva. Chaves ficam no ambiente.",
		"Applies to new turns and sessions; current approvals remain unchanged.":          "Aplica a novas interações e conversas; preserva as aprovações atuais.",
		"Initial mode (ask/plan/execute)":                                                 "Modo inicial (ask/plan/execute)",
		"Maximum steps":                                                                   "Limite de etapas",
		"Maximum seconds":                                                                 "Tempo máximo em segundos",
		"Tool output budget (bytes)":                                                      "Orçamento de saída das ferramentas (bytes)",
		"First token timeout (seconds)":                                                   "Espera do primeiro token (segundos)",
		"Idle timeout (seconds)":                                                          "Espera sem atividade (segundos)",
		"Resume most recent session (true/false)":                                         "Retomar conversa recente (true/false)",
		"Show process by default (true/false)":                                            "Mostrar processo ao iniciar (true/false)",
		"Connection test cancelled; your edits are preserved.":                            "Consulta cancelada; formulário preservado.",
		"Edited; test again to validate these settings.":                                  "Configuração alterada; consulte novamente para validar.",
		"Tab fields · Ctrl+T test with permission · Ctrl+S save · Esc back":               "Tab campos · Ctrl+T testa com permissão · Ctrl+S salva · Esc volta",
		"Connection testing is unavailable.":                                              "Teste de conexão indisponível.",
		"Wait for the test or press Esc to cancel before saving.":                         "Aguarde o teste ou pressione Esc antes de salvar.",
		"↑↓ scroll results · Esc returns to the preserved form":                           "↑↓ rola resultados · Esc volta ao formulário preservado",
		"Test cancelled; edits are preserved.":                                            "Teste cancelado; formulário preservado.",
		"Edited; test again for current settings.":                                        "Configuração alterada; teste novamente.",
		"No integrations. N adds MCP; L adds a language server.":                          "Sem integrações. N adiciona MCP; L adiciona servidor de linguagem.",
		"Testing… approve the connection request if prompted; Esc cancels.":               "Testando… aprove a conexão quando solicitado; Esc cancela.",
		"Configure a provider in ~/.opcode/config.json":                                   "Configure um provedor em ~/.opcode/config.json",
		"Configured catalog · live listing unavailable":                                   "Catálogo configurado · listagem ao vivo indisponível",
		"Could not load models; showing configured catalog. Ctrl+R retries.":              "Falha ao consultar; exibindo catálogo configurado. Ctrl+R tenta novamente.",
		"logs":                   "logs",
		"quit":                   "sair",
		"toggle help":            "mostrar ajuda",
		"switch session":         "trocar conversa",
		"commands":               "comandos",
		"select files to upload": "anexar arquivos",
		"model selection":        "escolher modelo",
		"switch theme":           "trocar tema",
		"toggle process":         "mostrar processo",
		"jump to latest":         "novas mensagens",
		"cancel":                 "cancelar",
		"new session":            "nova conversa",
		"close help":             "fechar ajuda",
		"go back":                "voltar",
		"close":                  "fechar",
		"previous model":         "modelo anterior",
		"next model":             "próximo modelo",
		"previous provider":      "provedor anterior",
		"next provider":          "próximo provedor",
		"select model":           "escolher modelo",
		"refresh models":         "atualizar modelos",
		"select command":         "escolher comando",
		"save":                   "salvar",
		"test connection":        "consultar catálogo",
		"back":                   "voltar",
		"terminal / chat":        "terminal / conversa",

		"Connections and preferences": "Conexões e preferências", "Configure provider": "Configurar provedor", "Preferences": "Preferências", "Profile ID": "ID da conexão", "Name": "Nome", "API base URL": "URL da API", "API key environment variable": "Variável de ambiente da chave", "Backend (local/openai/anthropic/gemini/groq/openrouter/xai)": "Backend (local/openai/anthropic/gemini/groq/openrouter/xai)", "API (empty/chat_completions/responses/ollama)": "API (vazio/chat_completions/responses/ollama)", "Context window (0 uses catalog)": "Janela de contexto (0 usa catálogo)", "Default output tokens (0 uses catalog)": "Tokens de saída (0 usa catálogo)", "Disabled (true/false)": "Desativado (true/false)", "No connections yet. Press N to add Ollama or another provider.": "Nenhuma conexão. Pressione N para adicionar um provedor.", "N add · Enter edit · P preferences · I integrations · Esc close": "N adicionar · Enter editar · P preferências · I integrações · Esc fechar", "Tab/↑↓ fields · Ctrl+S save · Esc back": "Tab/↑↓ campos · Ctrl+S salvar · Esc voltar", "Tab fields · Ctrl+T test · Ctrl+S save · Esc back": "Tab campos · Ctrl+T catálogo · Ctrl+S salvar · Esc voltar", "Testing connection… Esc cancels.": "Consultando catálogo… Esc cancela.", "MCP and language servers": "MCP e servidores de linguagem", "Connection test results": "Resultados do teste", "Transport (stdio/http/sse)": "Transporte (stdio/http/sse)", "Command (stdio)": "Comando (stdio)", "Arguments (JSON array)": "Argumentos (array JSON)", "URL (http/sse)": "URL (http/sse)", "Headers from environment (JSON header:ENV)": "Cabeçalhos do ambiente (JSON header:ENV)", "Child environment (JSON target:ENV)": "Ambiente do processo (JSON destino:ENV)", "Language/name": "Linguagem/nome", "Command": "Comando", "OAuth enabled (true/false)": "OAuth ativo (true/false)", "OAuth client ID (empty: dynamic registration)": "OAuth client ID (vazio: registro dinâmico)", "OAuth client secret environment variable": "OAuth variável do segredo do cliente", "OAuth scopes (space separated)": "OAuth escopos (separados por espaços)", "OAuth redirect port (0: automatic)": "OAuth porta de retorno (0: automática)", "No models found": "Nenhum modelo encontrado", "Configured catalog": "Catálogo configurado", "Loading provider models…": "Consultando modelos do provedor…", "No provider · Ctrl+P settings": "Sem provedor · Ctrl+P configurações", "Ctrl+P settings · Ctrl+A role · Ctrl+R refresh · Esc close": "Ctrl+P ajustes · Ctrl+A função · Ctrl+R catálogo · Esc fechar", "↑↓ select · ←→ provider · Enter choose · Ctrl+F favorite": "↑↓ seleciona · ←→ provedor · Enter escolhe · Ctrl+F favorito",
	}
	if pt, ok := portuguese[english]; ok {
		return config.Text(pt, english)
	}
	return english
}

func localizedSettingHelp(help string) string {
	translations := map[string]string{
		"0: auto (72); 1–240 columns. Code keeps full width.": "0: automático (72); 1–240 colunas. Código mantém a largura disponível.",
		"auto, shown, hidden": "automático, visível ou oculto",
		"pt, en":              "português ou inglês",
		"off, bell, auto, native, osc (only when unfocused)": "desativadas, sino, automático, nativas ou OSC (somente sem foco)",
		"true, false":        "ativado ou desativado",
		"ask, plan, execute": "Perguntar, Planejar ou Executar",
		"1–7200 seconds":     "1–7200 segundos", "1–1800 seconds": "1–1800 segundos", "1–600 seconds": "1–600 segundos",
		"ctrl+letter, alt+letter": "ctrl+letra ou alt+letra",
	}
	if translated, ok := translations[help]; ok {
		return config.Text(translated, help)
	}
	return help
}
