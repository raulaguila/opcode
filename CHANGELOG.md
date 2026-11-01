# Histórico de versões

## Em desenvolvimento — 2026-11-01

- Linha do tempo mais limpa: cada processo usa `✓`/`✗`/`■`/`●`, junta chamadas
  repetidas (`edit store.go ×3`) e guarda o raciocínio das chamadas dentro dele.
  O agente para quando a mesma chamada falha três vezes do mesmo jeito.
- `Shift+Tab` alterna o modo; mensagens suas usam barra grossa; a legenda de
  seleção aparece só na primeira resposta.
- Rodapé usa blocos (`██░░░`) no contexto e cede o espaço das métricas a avisos;
  durante a geração, a taxa tem prioridade sobre a fração de tokens.
- Paleta mostra o atalho de cada comando; ajuda usa `Ctrl+L` como o resto da
  interface; plano vazio não ocupa a tela; nova conversa e estados finais
  respeitam o idioma; formulário de provedor destaca rótulos e não acusa
  "alterações não salvas" em uma conexão nova.

- Correções da auditoria: edições pedidas por servidores LSP
  (`workspace/applyEdit`) são recusadas; o servidor local libera vagas de
  sessões ociosas e não trava outras sessões durante um envio; o sandbox
  reutiliza as máscaras de credenciais enquanto nenhum diretório muda; a saída
  parcial do bash é indicada como tal; `write` cria arquivos vazios; o consentimento
  de edição funciona com projetos atrás de symlinks; logs de depuração são privados.
- MCP passa a ter pacote próprio (`internal/llm/mcp`), código morto foi removido e
  o limite do `gocognit` subiu para 30 para evitar divisões mecânicas.

- Recuperação usa os mesmos comandos no desenho e nos cliques; Revisar tarefa
  abre a tarefa, espaços não executam ações e rótulos compactos evitam cortes.
- Aprovação explica o efeito somente leitura em Ask/Plan, é pesquisável por
  `/approval` e deixa de repetir confirmações rotineiras em Executar.
- Paleta informa posição e continuidade; modelo fica legível no rodapé compacto
  ocioso e cada linha do catálogo informa o contexto disponível, sem nova consulta.

- Rodapé separa modo e aprovação em dois controles. A política selecionada,
  inclusive o padrão, permanece visível; cada clique destaca a seleção
  correspondente no mesmo menu. Rótulos compactos preservam o espaço da geração.

- Seletor de modo inclui políticas de aprovação: solicitar (padrão) ou permitir
  criação/edição nativa dentro do projeto, somente nesta sessão. Comandos, rede,
  exclusões, movimentações e caminhos externos/sensíveis continuam protegidos.
  Ask/Plan, validação das citações e isolamento permanecem preservados.

- Raciocínio fornecido pelo modelo aparece durante a geração em bloco próprio,
  recolhendo ao começar o texto público da resposta. Expansão manual e leitura
  preservam o bloco; conteúdo e cópia da resposta final continuam separados.

- Conexões mostram atalhos de salvar, catálogo e opções avançadas com cliques
  alinhados. Falhas de catálogo distinguem autenticação, TLS, rede, tempo e
  formato, com diagnóstico sanitizado em `/error`, separado da recuperação.
- Pergunta vazia oferece orientação no painel e limpa o aviso ao corrigir a
  resposta; rascunhos, seleção e validação de tamanho continuam preservados.
- Rodapé compacto usa estados curtos completos, preservando tempo e tok/s;
  preenchimento integral da barra de contexto ocorre apenas em 100%.
- Plano informa etapas concluídas e conteúdo oculto acima/abaixo. O catálogo
  distingue total, modelos de conversa e resultados de busca.

## Em desenvolvimento — 2026-10-31

- Rodapé apresenta barra e percentual de uso da janela efetiva, mantendo estimativas
  identificadas e estado desconhecido explícito. Durante a geração, etapa, tempo
  e tok/s são compactados para terminais estreitos; perguntas e avisos preservam
  prioridade. Sem novas consultas, dependências ou mudanças nas validações.

- OpenAI/compatíveis enviam texto simples sem blocos multimodais desnecessários,
  exibem recusas do provedor e validam objetos JSON antes de encaminhar ferramentas.
  Chat Completions e Responses distinguem limite de saída e filtragem de conteúdo,
  preservando texto parcial e sem repetir gerações ou alterar os protocolos.

- Chamadas Ollama interrompidas por limite de saída são rejeitadas com diagnóstico
  próprio e acesso às configurações, sem repetição automática ou redução do contexto.
- A busca usa o híbrido existente por padrão quando embeddings locais já estão
  explicitamente configurados; a opção lexical evita essas requisições.

- Pedidos de revisão preparam implementações e testes relacionados em consultas
  limitadas, favorecendo corpos executáveis e preservando identificadores.
  O prompt pede conferir proteções existentes e separar achados de hipóteses.
- Plano usa a superfície, borda e divisória das perguntas, com progresso e etapa
  ativa destacados; a identidade da sessão continua nos detalhes de `/task`.

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
