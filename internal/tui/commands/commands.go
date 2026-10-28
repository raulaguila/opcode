package commands

// Builtins is shared by the slash menu and command palette.
type Command struct {
	ID, Title, Description string
	Group                  bool
}

var Builtins = []Command{
	{ID: "new", Title: "Nova conversa", Description: "Começar outra conversa"},
	{ID: "files", Title: "Arquivos", Description: "Escolher arquivos para anexar"},
	{ID: "terminal", Title: "Terminal", Description: "Abrir ou voltar ao terminal"},
	{ID: "commands", Title: "Todos os comandos", Description: "Buscar ações e ferramentas"},
	{ID: "mode ", Title: "Modo de trabalho", Description: "Escolher modo de trabalho…", Group: true},
	{ID: "plan", Title: "Mostrar/recolher plano", Description: "Mostrar ou recolher o plano"},
	{ID: "task", Title: "Tarefa atual", Description: "Ver tarefa e critérios de aceite"},
	{ID: "resume", Title: "Retomar tarefa", Description: "Continuar a tarefa salva"},
	{ID: "setup", Title: "Configuração guiada", Description: "Provedor, modelo e testes opcionais"},
	{ID: "local-status", Title: "Alocação do Ollama", Description: "Contexto carregado, CPU/GPU e memória do modelo"},
	{ID: "contract", Title: "Resultado esperado", Description: "Explicar, planejar, alterar ou verificar; arquivos e critérios"},
	{ID: "settings", Title: "Configurações", Description: "Provedores e preferências"},
	{ID: "models", Title: "Escolher modelo", Description: "Catálogo, favoritos e detalhes"},
	{ID: "integrations", Title: "Integrações", Description: "Cadastrar/testar MCP ou LSP"},
	{ID: "sessions", Title: "Conversas", Description: "Buscar, renomear, arquivar e exportar"},
	{ID: "queue", Title: "Instruções pendentes", Description: "Editar ou remover mensagens da fila"},
	{ID: "context", Title: "Contexto do modelo", Description: "Orçamento previsto e reservas"},
	{ID: "recalibrate", Title: "Recalibrar contexto", Description: "Remover limite aprendido para o modelo atual"},
	{ID: "compact", Title: "Compactar contexto", Description: "Preparar o histórico para a próxima chamada"},
	{ID: "worktrees", Title: "Projeto e worktrees", Description: "Branch, arquivos, diferenças e projetos isolados"},
	{ID: "changes", Title: "Alterações e verificações", Description: "Revisar os arquivos alterados por interação"},
	{ID: "checks", Title: "Verificações da tarefa", Description: "Consultar recibos da revisão atual"},
	{ID: "undo", Title: "Desfazer interação", Description: "Revisar a reversão antes de aplicar"},
	{ID: "redo", Title: "Refazer interação", Description: "Revisar a reaplicação antes de aprovar"},
	{ID: "permissions", Title: "Permissões da sessão", Description: "Inspecionar e revogar autorizações"},
	{ID: "skills", Title: "Skills disponíveis", Description: "Ver habilidades cadastradas explicitamente"},
	{ID: "skill ", Title: "Usar uma skill", Description: "Informe o nome e os argumentos", Group: true},
	{ID: "actions", Title: "Ações da resposta", Description: "Copiar resposta/código ou abrir citação"},
	{ID: "export", Title: "Exportar conversa", Description: "Salvar conversa e processo em Markdown"},
	{ID: "retry", Title: "Retomar última pergunta", Description: "Conferir o progresso antes de continuar"},
	{ID: "error", Title: "Detalhes do erro", Description: "Ver diagnóstico da execução"},
	{ID: "process", Title: "Mostrar/recolher processo", Description: "Revisar as etapas da última interação"},
	{ID: "latest", Title: "Novas mensagens", Description: "Voltar ao acompanhamento da resposta"},
}
