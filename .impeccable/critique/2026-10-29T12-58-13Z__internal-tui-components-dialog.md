---
target: Menus e painéis da TUI Opcode após f012050
total_score: 23
p0_count: 0
p1_count: 3
timestamp: 2026-10-29T12-58-13Z
slug: internal-tui-components-dialog
---
Método: duas avaliações independentes — A: `/root/menu_design_a`; B: `/root/menu_evidence_b` — sintetizadas após A terminar. Alvo: menus e painéis da TUI em `internal/tui`, revisão `f01205093f65a6d80c33145c55697adf7604ca6b`. Inspeção das capturas enviadas e do código; o fluxo PTY de aceitação executado nesta rodada está documentado em `docs/evidence/2026-10-29-ui-ux-followup-validation.json`.

# Opcode: revisão dos menus após f012050

O próximo ganho está em tornar as ações reconhecíveis e as telas previsíveis. A aparência sóbria é adequada ao terminal. O atrito vem de componentes com comportamentos diferentes: alguns oferecem busca, prévia e marcadores textuais; outros dependem de atalhos comprimidos, cores ou medidas fixas.

## O que já foi entregue

As doze prioridades anteriores foram implementadas e publicadas. Aprovações começam em Recusar; Esc recusa e o clique usa a geometria real. O editor externo devolve rascunho. Conexões desativadas são explícitas. Busca PT/EN, contraste, transição de plano, resumos de contexto, navegação de volta, metadados do modelo, prévias de conversa e Markdown por bloco estão implementados. Permissões, validação de citações e critérios de avaliação foram preservados.

Passaram `go test -race -p 2 ./...`, `go vet ./...`, vet da TUI com integração, build sem CGO e fluxo PTY real com Ollama `llama3.1:8b`. O fluxo incluiu recusa por Esc/Enter, aprovação por clique, leitura com citação, saída de comando ao vivo, entrada pelo PTY e instrução enfileirada. Isso não significa que todo submenu foi validado em todas as dimensões.

A repetição de Ctrl+S/Ctrl+T vista na primeira captura já foi removida. Favoritos, recentes, busca, prévia de sessão, volta e resumo de contexto existem; as propostas seguintes refinam sua apresentação.

## Prioridades para a próxima implementação

1. **P1 — Eliminar os dois caminhos que derrubam a aplicação.** No menu de presets, clique no rodapé ou rolagem além dos seis itens deixa um índice inválido. No seletor de arquivos, Enter ou `l` em diretório vazio acessa o primeiro item inexistente. Ambos foram reproduzidos por sondas locais, sem inferência. Validar índice, limitar hitboxes à lista e apresentar estado vazio. Fontes: `experience.go:101,164`, `filepicker.go:167,192`. Direção: `$impeccable harden`.

2. **P1/P2 — Completar adaptação e preservação de foco.** O seletor de arquivos renderizou 134×28 mesmo recebendo 60×20 ou 80×24. O formulário de argumentos rendeu 66×22 em 60×20 e perdeu entrada no segundo campo após resize. A seleção de sessão também pode sair da área visível. Usar dimensões reais, prévia recolhível, rolagem até o item focado e preservar foco/seleção no resize. Incluir temas no envio de WindowSizeMsg pelo root e manter escolhas de perguntas visíveis. Fontes: `filepicker.go:123,263,352`, `arguments.go:102,183,230`, `session.go:62,163`, `internal/tui/tui.go:292,330`, `internal/tui/page/questions.go:91`. Direção: `$impeccable adapt`.

3. **P2 — Unificar teclado, mouse, foco e retorno.** O modo abre pelo mouse, mas o root descarta o clique dentro desse seletor e de outros modais. Esc não cancela a confirmação de saída. O tema selecionado fica indistinguível em NO_COLOR. Encaminhar eventos ao modal ativo, sem rolar o chat atrás; Esc volta/cancela, `›` indica foco e `✓` indica o item em uso. Reservar uma ação principal, Voltar e Mais ações, sem rodapés longos truncados. Fontes: `internal/tui/tui.go:244,630`, `quit.go:65`, `theme.go:164`, `providers.go:448,469`, `integrations.go:430`. Direção: `$impeccable harden` e `$impeccable clarify`.

4. **P2 — Simplificar os menus das capturas.** Provedores, Preferências e Integrações devem ser entradas visíveis. Usar nome humano e estado antes de ID/backend/legacy. Formulários devem sinalizar alterações pendentes, preservar edição ao voltar e separar Salvar, Consultar catálogo e opções avançadas. No catálogo, retirar Outros repetido e Search: Search models, tornar favorito/provedor descobríveis e manter metadados essenciais. No modo, três opções sem busca; Enter aplica o modo. Fontes: `providers.go:308,432,469`, `model_view.go:145,208,235,276`, `commands.go:160,190`, `experience.go:153,237`. Direção: `$impeccable distill` e `$impeccable clarify`.

O restante da lista abaixo completa essas quatro frentes. Uma revisão `$impeccable polish` deve encerrar a implementação, com testes de todos os modais pelo root e uma matriz PTY de tamanhos/temas, preservando o fluxo real com Ollama e seus critérios.

## Detalhes para as telas enviadas

- **Provedor:** nome, endereço, referência à variável da chave e conexão ativa. Explicar quando a chave é opcional; jamais exibir seu valor. Erro junto do campo e texto integral acessível. ID, protocolo, limite manual e origem antiga ficam em Mais opções. Ativar continua explícito.
- **Conexões:** mostrar, por exemplo, Ollama · Local · Ativa; a origem de configuração antiga pertence aos detalhes. Configurações não deve esconder Preferências e Integrações atrás de letras no rodapé.
- **Modelos:** destacar Em uso e favorito sem repetir categoria em toda linha. Contexto em unidade legível, preservando máximo anunciado versus alocação observada. Local/carregado/remoto só com metadados que sustentem a informação. Suporte informado a ferramentas não é teste de qualidade. Não executar inferência automaticamente ao abrir o catálogo.
- **Modo:** Perguntar, Planejar e Executar com a consequência de cada opção. A mudança deve ser apresentada como seleção para o trabalho, sem sugerir execução imediata. Permissões continuam independentes da escolha.

## Todos os grupos de menus

| Família | Melhoria restante |
|---|---|
| Conexões / provedor | Identidade humana, estado Ativa/Desativada, ajuda por campo, alterações pendentes e ações visíveis; detalhes técnicos da configuração antiga sob demanda. |
| Modelos / catálogo / teste | Retirar Outros repetido e Search: Search models; favorito e provedor descobríveis, unidades legíveis, estado de metadados separado do resultado de um teste. |
| Modo de trabalho | Seletor compacto com três opções e Aplicar modo; sem busca genérica e sem comando ambíguo Enter executar. |
| Paleta e sugestões `/` | Agrupar por Conversa, Trabalho, Projeto e Configurações; mostrar o comando correspondente e diferenciar Abrir, Aplicar e Executar. Preservar a busca unificada existente. |
| Preferências / valores efetivos | Seções por categoria, títulos humanos, origem da opção secundária, acesso claro a Restaurar padrão e erros completos. |
| MCP / LSP / OAuth | Campos condicionais, estado e ações de autenticação pertinentes, retorno ao formulário com edição preservada; URL de autenticação com ação explícita de copiar/abrir. |
| Conversas / renomear / arquivar / exportar | Preservar recência e prévia; substituir arquivadas: true/false por Ativas/Arquivadas; Mais ações para renomear/exportar; seleção com prévia antes de abrir por mouse. |
| Arquivos / anexos / `@` | Lista e prévia adaptáveis; posição/pasta claras; nomes Unicode íntegros; distinguir anexar de apenas visualizar. |
| Argumentos de comandos / argumentos de integrações | O editor de argumentos de integrações já usa uma linha por argumento. Aplicar adaptação e localização ao formulário separado de comandos personalizados, com confirmação de envio clara. |
| Tarefa / plano / resultado esperado | Mostrar objetivo e próximo passo antes dos atalhos auxiliares; ação principal depende do estado. Editar critérios como itens, em vez de exigir vírgulas e ponto e vírgula. |
| Perguntas / revisão de respostas | Manter pergunta, opção focada e resposta visíveis; rolar opções intermediárias e permitir revisar respostas completas sem perder seleção. |
| Contexto / índice / alocação / diagnóstico | Preservar os resumos e detalhes recolhidos; abas com estado ativo claro, unidades consistentes e rótulos que expliquem o efeito de compactar, recalibrar e reconstruir. |
| Alterações / checks / diff / desfazer-refazer | Lista de arquivos e recibos com posição clara; verificação aprovada, falha ou ainda não executada explícita. Manter prévia e aprovação antes de aplicar reversões. |
| Permissões / aprovações | Manter Recusar como padrão, escopo e parâmetros exatos; explicar duração de Lembrar e só mostrar atalhos de diff quando aplicáveis. Revogação claramente separada de desfazer arquivos. |
| Terminal / processos / instruções pendentes | Lista visível dos processos, indicação inequívoca de foco do terminal e estado; acesso a interromper, voltar e editar fila sem memorizar toda a linha de atalhos. |
| Projeto / Git / worktrees | Identificar projeto e branch atuais; mostrar destino e efeito de abrir outra instância, preservando as confirmações e permissões existentes. |
| Ações da resposta / código / citações | Separar Copiar de Abrir fonte; identificar arquivo e linha, preservar validação das citações e localizar rótulos ainda fixos em português. |
| Skills | Buscar por nome e finalidade, mostrar parâmetros antes de usar e distinguir descrição de execução; nenhum uso automático decorrente da seleção. |
| Configuração guiada / inicialização | Manter caminho curto para conversar e testes opcionais; ação específica Criar Opcode.md / Agora não em vez de Sim / Não. |
| Tema / ajuda / sair | Distinguir tema em prévia do salvo, foco textual sem cores, ajuda por intenção e botões Sair / Continuar. Preservar a prévia reversível existente. |

## Convenção visual proposta

Cada tela deve responder: onde estou, o que está selecionado, o que muda ao confirmar e como volto. Usar borda e espaçamento consistentes, diminuir a sombra e reduzir a competição visual da conversa ao fundo. Listas longas precisam de busca; seletores de três opções não. Informações técnicas ficam em Detalhes, sem esconder a ação principal ou o estado de salvamento.

A barra inferior da conversa continua enxuta. Quando um menu está aberto, a ajuda é daquele menu; os controles da conversa não devem competir por atenção nem parecer ativos atrás dele.

## Heurísticas

Notas provisórias de 0 a 4, baseadas em código e capturas. Não são benchmark, certificação de acessibilidade ou comparação de qualidade com Codaro/Crush. Este alvo é mais restrito que a revisão anterior da TUI.

| Heurística | Nota | Evidência principal |
|---|---:|---|
| Estado do sistema | 3 | Modelo atual, origem, carregamento e recência; edição pendente pouco clara. |
| Linguagem familiar | 2 | legacy, caminhos de configuração e booleans ainda expostos. |
| Controle e liberdade | 2 | Volta, recusa e prévia reversível; formulários podem perder edição ao voltar. |
| Consistência | 2 | Enter, foco, ajuda e ações mudam entre famílias. |
| Prevenção de erros | 2 | Autorizações seguras, mas presets/diretório vazio ainda causam panic. |
| Reconhecimento | 2 | Busca boa; ações secundárias escondidas ou cortadas. |
| Eficiência | 3 | Teclado, mouse, favoritos e recentes existentes. |
| Minimalismo | 3 | TUI sóbria; categorias repetidas e rodapés técnicos acrescentam ruído. |
| Recuperação | 2 | Erros podem ser truncados e ficar distantes do campo. |
| Ajuda | 2 | Ajuda rolável; falta agrupamento por intenção. |
| Total | 23/40 | Diagnóstico de menus; priorizar adaptação e previsibilidade. |

## Perfis de uso e limites

Para o iniciante, legacy e Enter executar no seletor de modo são ambíguos. Para quem trabalha por teclado, convenções diferentes e atalhos escondidos exigem reaprendizado. Em terminal compacto ou sem cores, algumas opções secundárias e a seleção podem desaparecer. A maior carga está nos formulários avançados, não na quantidade de modelos por si só; a busca e a ordenação já ajudam.

As forças a preservar são a distinção entre foco e modelo atual nos menus centrais, os resumos com detalhes sob demanda e os controles de segurança. Os formulários e o retorno após salvar/testar precisam alcançar a mesma previsibilidade.

A análise de todos os grupos é estática, apoiada pelas capturas. A aceitação PTY recente valida o fluxo principal e variantes de tema/tamanho, não todos os menus. Não houve novo benchmark comparativo com Codaro ou Crush.

## Evidência técnica da segunda avaliação

Detector Impeccable executado: saída `[]`, exit 0, **zero arquivos Go suportados**. Isso não aprova a interface nem sua acessibilidade. Nenhuma página web ou overlay foi criada: o alvo é uma TUI. Evidência alternativa: capturas, leitura de fonte, frames VT e sondas determinísticas de componentes.

As sondas foram executadas com Go overlay e fonte fora do checkout, sem novas chamadas ao provedor. O PASS no log indica captura bem-sucedida do defeito, não correção do comportamento. Resultados:

| Severidade | Reprodução / evidência | Resultado observado |
|---|---|---|
| P1 | Clique em `(5,10)` após o último preset | Panic `index out of range [6] with length 6`. |
| P1 | Enter ou `l` com lista de arquivos vazia | Panic `index out of range [0] with length 0`. |
| P1 | Arquivos em 60×20, 80×24, 120×36 e 180×40 | Renderização invariável em 134×28. |
| P2 | Segundo argumento focado → resize → digitar | Índice 1, foco no campo 0; texto não entra. |
| P2 | Cinco argumentos em 60×20 | Renderização de 66×22. |
| P2 | Sessão 10 selecionada → reduzir para 60×20 | Seleção 9, offset 0, oito linhas; item selecionado invisível. |
| P2 | Esc no diálogo Sair | Nenhum comando de fechar retornado. |
| P2 | Seleções diferentes de tema com perfil ASCII | Views idênticas. |
| P2 | Leitura do roteamento de mouse do root | Eventos descartados em modo/paleta, arquivos, temas, argumentos, inicialização e saída. |
| P2 | Leitura do roteamento de resize do root | Tema não recebe WindowSizeMsg; limites do componente ficam sem atualização. |

Os oito primeiros têm sondas de componente; os dois últimos têm evidência direta no roteamento. Não foram reproduzidos em uma nova sessão PTY completa. A redução das perguntas que pode esconder opções intermediárias foi identificada por leitura e permanece hipótese de interação para a próxima regressão.

Perguntas dispensadas: a preferência por uma interface mínima, uso da barra inferior, manutenção de permissões e validação de citações já está definida pelo usuário. Esta rodada solicita análise; os novos achados formam o próximo conjunto de correções, ainda não implementado.
