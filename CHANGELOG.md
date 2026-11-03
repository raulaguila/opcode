# Histórico de versões

## Em desenvolvimento — 2026-11-02

- **Duas interfaces, a mesma proposta visual.** `opcode` abre a nova interface
  de terminal; `opcode app` abre o app numa janela própria. As duas
  compartilham agente, conversas e configuração. A interface anterior fica por
  um tempo em `opcode --ui classico`. Guia em [Terminal e app](docs/interfaces.md).
- App: janela nativa com Wails quando compilado com `-tags desktop`
  (`make build-desktop`, `sh ./install --desktop`); sem a tag, abre a janela de
  um navegador. Releases trazem também `opcode-desktop-*` para Linux, macOS e
  Windows.
- App: tema claro, escuro ou do sistema; `@` para citar arquivos; contexto e
  raciocínio ajustáveis nos detalhes do modelo; setas em todas as janelas, com
  menu e seção em dois níveis nas configurações; JetBrains Mono embutida.
- Perguntas do agente podem trazer uma nota curta por opção; a resposta própria
  é a última opção numerada; `Esc` guarda a pergunta para depois.
- Conexões: uma variável de chave não definida não impede mais o início; a
  conexão aparece como **chave ausente** e pode ser editada ou desativada.
- Conversas mostram estado (em execução, plano pendente, falha), modo, modelo e
  progresso do plano; passos de edição mostram `+N −M`; respostas citam a
  primeira fonte; o rodapé mostra a etapa em curso e a velocidade.
- Na primeira execução sem modelo, as duas interfaces novas abrem em **Nova
  conexão**.
- Terminal ganha o que o app já tinha: `@` para citar arquivos; `C` e `R`
  ajustam contexto e raciocínio nos detalhes do modelo, que mostram também
  carregado e velocidade; `Esc` guarda a pergunta para depois; `T` mostra o
  diff inteiro na aprovação, com o campo de mensagem em cinza guardando o
  rascunho; configurações em dois níveis com as setas; `→` nas ações de
  Conversas; card **Parado** com a causa em uma frase; paleta com Recentes e
  comandos novos; largura de leitura e mostrar processo aplicados.
- Agente: o prompt traz o resumo do README, para responder sobre um arquivo
  sabendo o que é o projeto. Um arquivo citado com `@` é lido antes da primeira
  etapa do modelo, com os arquivos Go que importam o seu pacote. Modelos de até
  8B (pelo tamanho no nome, como `qwen3:4b`) recebem as regras em versão curta,
  com cerca de metade do tamanho. As ferramentas de uma resposta ficam estáveis:
  uma nova entra no fim da lista e só janelas pequenas devolvem as alugadas.
  O log registra como cada pedido foi lido e quando o modelo buscou sozinho o
  que nada preparou.
- Busca semântica opcional em Configurações › Trabalho: o nome de um modelo de
  embeddings do Ollama local liga a busca híbrida; vazio desliga. Só vale na
  configuração global.
- Respostas mostram os arquivos lidos no turno (`lido: main.go, cmd/root.go`),
  no lugar da primeira citação. `Ctrl+A` no seletor de modelos, nas duas
  interfaces, escolhe o modelo de tarefas, títulos ou resumos.
- Integrações mostram os servidores de linguagem instalados que servem ao
  projeto (pelo `go.mod`, `pyproject.toml`, `Cargo.toml`…); `Enter` ou
  **Ativar** liga um com o comando do preset. Terminal e app.
- **Alterações** (`/changes`) lista as interações que editaram arquivos, com o
  diff ao lado; `Enter` desfaz uma aplicada ou refaz uma desfeita. `/undo` e
  `/redo` agem na última. Cada arquivo passa pelo card de aprovação, e um
  arquivo mudado depois é recusado. Comandos executados não são desfeitos.
  Terminal e app.
- Sair pelo `Ctrl+C` ou pelo comando **Sair** pede confirmação no terminal.
- Enquanto o modelo pensa, o raciocínio aparece ao vivo (últimas linhas) no terminal e no app; ao responder, fica só a resposta.
- Terminal: uma linha de respiro entre a legenda do campo de mensagem e o rodapé; nas Configurações, cada preferência é separada por um traço e a lista rola quando não cabe.
- `ls` de uma árvore maior que uma página mostra as subpastas com a contagem
  de arquivos e depois os arquivos da pasta, em vez dos 60 primeiros caminhos
  em ordem alfabética; `recursive: true` mantém a lista plana.
- `↑` na primeira linha do campo traz a mensagem enviada anterior, como num
  shell; `↓` na última volta, até o rascunho. Terminal e app.

## Em desenvolvimento — 2026-11-01

- Novo visual da TUI: molduras arredondadas com título, posição e teclas na
  borda; barra superior com caminho, branch e conversa; entrada com a cor do
  modo e as teclas na borda; cartão de erro com as ações de recuperação.
- Paleta agrupada com descrições e atalhos; seletor de modelos com ícone de
  busca, etiqueta Nuvem e detalhes ao lado; conversas com prévia ao lado;
  configurações com seções, cartões de conexão e campos em uma linha.
- Perguntas e aprovações numeradas (1–9 e 1/2/3); a aprovação aparece logo
  acima da entrada; o plano mostra o progresso na borda.

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
