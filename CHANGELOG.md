# Histórico de versões

## Em desenvolvimento — 2026-10-30

- Interface migrada para Bubble Tea/Bubbles/Lip Gloss/Glamour v2, com eventos
  nativos de teclado, mouse e colagem e compatibilidade dos temas existentes.
- Menus e formulários alinhados ao painel de pergunta: bordas retas, seleção
  discreta e divisórias nos controles, preservando cores de avisos/permissões.
- Seleção preservada em `NO_COLOR` e resets compactos do Markdown tratados
  corretamente, mantendo o peso normal dos parágrafos após títulos e ênfases.
- Dependências de runtime e testes atualizadas; FTS5 registrado por conexão
  dos índices de código/memória e conversão HTML migrada para a API v2.
- Lip Gloss fixado em commit oficial que reduz alocações de escrita. Motivo,
  hash e procedimento para trocar por release em [Dependências](docs/dependencies.md).

## 0.2.0 — 2026-10-28

- Configuração em `~/.opcode/config.json` e `.opcode/config.json`, com template
  inicial privado, compatibilidade com caminhos antigos, schema versionado,
  validação offline, exportação redigida e migração com backup protegido.
- Primeiro uso guiado, catálogo e seleção de modelos, presets, testes opcionais,
  preferências pesquisáveis, atalhos, idioma e recuperação contextual de erros.
- Contratos de tarefa definidos pelo usuário: explicar, planejar, alterar e
  verificar, preservando modos, permissões e recibos da revisão atual.
- Planos locais com argumentos simples e validação estrita, instruções de edição
  claras e trabalho parcial quando não existem checks configurados.
- Contexto adaptável após erros reais de contexto/memória e consulta da alocação
  observada do Ollama sem geração nem download.
- Índice de código incremental, exploração limitada, formatação protegida,
  painel Git/worktrees, revisão adaptável e ações sobre respostas.
- OAuth MCP com PKCE, servidor loopback autenticado e cliente de reconexão.
- Aprovações vinculadas aos argumentos exatos e testes de concorrência,
  persistência, sandbox, configuração, UI e protocolo.
- Builds Linux/macOS amd64/arm64, arquivos locais determinísticos e SHA256.

Validação: testes Go com race detector, vet, build e avaliações reais opt-in.
As seis fixtures com `llama3.1:8b` passaram em duas execuções consecutivas; a
fixture de edição confirma a correção do arquivo e permanece parcial por não
possuir checks. Esses resultados não são uma taxa geral de qualidade do modelo.
Detalhes e evidências em [Validação](docs/validation.md).
