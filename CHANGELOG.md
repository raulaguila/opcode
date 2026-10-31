# Histórico de versões

## Em desenvolvimento — 2026-10-31

- Padrões das avaliações aceitam prosa, setas LaTeX e parágrafos de cabeçalhos,
  preservando rejeição de inversões, negações e contradições explícitas. O motor
  de avaliação, o agente e a validação de citações permanecem inalterados.

- Preparação de fluxos tenta até três pistas distintas quando a primeira não
  entrega uma nova fonte, mantendo o limite de leituras e orçamento existentes.
- Prévia de fontes atualiza o mesmo intervalo com Ctrl+R; formatos de referências
  acompanham o validador e o estado registrado pertence ao ID da resposta.
- Avaliações de explicação detectam pares entrada/retorno invertidos, mantendo
  os critérios de citações e sem chamadas adicionais ao modelo na conversa.
- Processos concluídos com sucesso ficam em segundo plano; opções longas de
  perguntas mostram início/final e identificam o atalho F2 para detalhes.

- Investigação de fluxos usa pistas da tarefa e orienta leitura de implementações,
  relações e resultados, preservando limites, recibos e validação de citações.
- Busca favorece identificadores dentro de perguntas naturais; avaliações
  adicionais cobrem fluxos distribuídos, homônimos e documentação conflitante.
- Listas simples permanecem compactas mesmo após quebra visual ou resize.
- Processos e fontes apresentam resultados mais claros; ações da resposta expõem
  a largura de leitura existente e o raciocínio tem rótulo explícito.

- Visões gerais usam chamadas do ponto de entrada como pistas e ranking próprio
  do índice para priorizar símbolos/caminhos, reduzindo resultados que só repetem
  uma consulta no corpo do código. Leituras adicionais continuam limitadas.
- Respostas gerais priorizam finalidade, funcionalidades e limites da inspeção,
  com referências junto às afirmações. O Ollama evita rodapés de fontes repetidas.
- Correções diretas reúnem versão anterior, motivo e detalhes internos em um
  controle de revisão, preservando avisos de falha e mensagens públicas.

- Criação, edição e patch verificam os conteúdos instalados e registram recibos
  dos arquivos afetados, separados dos recibos de testes. A conclusão de uma
  escrita simples não depende do limite global do workspace.
- Alterações externas nos arquivos gravados impedem a confirmação; comandos,
  hooks, integrações opacas e testes mantêm a verificação ampla. Testes
  indisponíveis resultam em estado parcial, sem apagar a confirmação da escrita.

## Em desenvolvimento — 2026-10-30

- Corrigido o vazamento do fundo padrão do terminal após negrito/código inline;
  prosa e blocos de código preservam seus fundos distintos.
- Conversão de cores usa um único buffer e preserva grupos RGB completos;
  parser Markdown compartilha tabelas imutáveis, mantendo árvores por chamada.
- Raciocínio da resposta fica em Detalhes antes da mensagem correspondente,
  sem criar um processo final duplicado. Blocos de código têm superfície própria.
- Perguntas amplas recebem uma busca limitada na implementação além dos arquivos
  básicos; prompt distingue documentação e comportamento confirmado no código.
- Legendas de navegação usam estilo secundário com contraste; modelo em uso é
  identificado somente na sua linha, separado da posição na lista.
- Prosa usa a largura interna exata, mantém percentuais decimais na mesma
  palavra e apresenta ênfase incompleta de streaming provisoriamente.
- Rodapé distingue espera, geração, validação e conclusão; histórico interno
  informa finalização. Perguntas respondidas ficam compactas e legíveis.
- Seletor distingue embeddings informados, hospedagem e limites de contexto;
  paleta e conexões têm ações consistentes e cliques alinhados ao desenho.
- Fontes citadas são agrupadas por arquivo, preservando intervalos e cópia
  integral; a prévia continua protegida pelas permissões normais.
- UTF-8 fragmentado é preservado no painel de terminal e nos simuladores PTY.
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
