# Matriz de regressão da interface

A matriz `TestRoutedModalRegressionMatrix`, em
`internal/tui/ui_regression_matrix_test.go`, exercita o roteamento real do
`appModel`. Não é apenas uma coleção de renders de componentes isolados.

São 13 superfícies × 4 tamanhos × 3 perfis = **156 cenários parametrizados**:

- tamanhos: 60×20, 80×24, 120×36 e 180×40;
- perfis do renderer: fundo escuro, fundo claro e cores desativadas (`NO_COLOR`);
- cada cenário abre o modal, verifica suspensão do foco do composer, reduz a
  janela para 60×20, restaura o tamanho, interage e confirma o retorno do foco;
- as dimensões são verificadas no frame efetivamente composto pelo aplicativo;
- nenhuma tecla do modal pode alcançar o composer de fundo.

Os perfis são forçados no renderer para tornar o teste determinístico. Não
atestam autodetecção de todo terminal, leitor de tela, ambiente SSH ou variável
externa. A aceitação PTY separada usa também os sinais de ambiente reais.

## Interações exercitadas

| Superfície | Caminho e observação |
|---|---|
| Paleta | Setas alteram seleção; clique no item renderizado retorna o comando certo; resultado fecha o modal. |
| Modo | Seleção por setas e clique; texto não cria busca invisível; confirmação devolve a seleção e fecha. A alteração do modo de domínio é coberta pelo teste de integração de workflow separado. |
| Permissões | Esc devolve recusa da solicitação exata; processar o resultado fecha e retorna o foco. A matriz não aprova ferramentas. |
| Sair | Clique em Continuar cancela; o diálogo de tema não é substituído pelo de saída. |
| Inicialização | Clique em Agora não retorna `Initialize:false`; nenhum agente é solicitado a gerar arquivo. Este cenário identificou a divergência entre origem visual e hitbox, corrigida no aplicativo. |
| Conversas | Clique seleciona a prévia; Enter retorna a sessão selecionada; o resultado fecha o seletor. |
| Argumentos de comando | Tab alcança o próximo campo; entrada Unicode é exibida; Esc cancela sem submissão. |
| Arquivos | Diretório do projeto vazio; Enter e `l` permanecem inócuos; Esc retorna ao composer. |
| Temas | Seleção muda com a seta; marcador textual continua visível sem cores; Esc fecha com restauração. O editor de cores é validado por testes próprios. |
| Ajuda | PgDown é encaminhado ao viewport; clique no título não executa ação; Esc fecha. |
| Conexões | Clique na ação Preferências abre o editor correto; Esc volta à lista e outro Esc fecha. |
| Integrações | N abre cadastro MCP; Tab navega nos campos; Esc preserva/volta e fecha, sem executar teste ou salvar. |
| Modelos | Catálogo de fixture passa pelo evento assíncrono real; busca sem resultado exibe estado vazio; Esc cancela e retorna o foco. |

A página de fundo é uma sonda que conserva sua identidade ao receber resize e
registra mensagens. Ela permite verificar foco e vazamento de teclas sem depender
no modelo, no banco do usuário ou em geração. O conteúdo do composer completo e
seus anexos continuam cobertos por testes próprios da página/chat e pelo PTY.

O catálogo usa um `RoundTripper` de fixture que recusa qualquer host diferente de
`matrix.invalid`. Nenhuma chamada externa é feita. HOME, configuração, dados e
workspace são temporários. Não são executados comandos, servidores LSP/MCP,
login OAuth, inferência, downloads ou clipboard nativo.

## Superfícies complementares e limites

Esta matriz não deve ser anunciada como “todos os estados da aplicação”. Os
seguintes caminhos têm outras regressões dedicadas e não são repetidos nas 156
combinações:

| Família | Cobertura complementar |
|---|---|
| Tarefa, plano e navegação de volta | `internal/tui/page/navigation_test.go`: seleção/scroll, respostas atrasadas, execução explícita e rascunhos preservados. |
| Contexto, índice e diagnóstico | Testes de workspace/contexto: resumos, detalhes, ações explícitas e preservação da política; não certificam serviço remoto. |
| Contrato e critérios por itens | Testes de navegação/contrato: itens com separadores sobrevivem à reabertura e o mouse não muda o campo de destino. |
| Fila e anexos | `workspace_test.go` e testes de draft: edição/remoção explícita e proteção contra perda de rascunho. |
| Perguntas e revisão de respostas | `questions_test.go`: alternativas, revisão, edição e retorno; `layout_regression_test.go` cobre estado compacto. |
| Alterações, checks, diff e reversões | Testes de review/workspace: fontes/estado, prévia e confirmação; nenhum desfazer é aplicado pela matriz de modais. |
| OAuth e detalhes de integração | Testes próprios de eventos, cancelamento e resultados atrasados; não houve autenticação real nesta matriz. |
| Presets e formulários avançados | `menu_regression_test.go` e `interaction_safety_test.go`: limites de clique, transporte, valores preservados, erros roláveis e retorno de testes. |
| Terminal, entrada PTY e instruções durante execução | `TestLiveOllamaPTYUI`, opt-in, com sandbox e aprovações exatas; além de testes de terminal e processos. |
| Citações e conclusão | Testes do runtime/agente continuam independentes da apresentação e não são afrouxados pela matriz. |

Não há captura de imagem fabricada nem nota de superioridade. A matriz detecta
falhas de geometria, roteamento e estado; inspeção visual em terminais reais
continua sendo evidência distinta.

## Executar

```sh
go test -race ./internal/tui -run '^TestRoutedModalRegressionMatrix$' -count=1
```

O teste pode rodar sem Ollama, rede ou credenciais. Não deve usar `t.Parallel`,
pois configuração, tema e perfil de cor são singletons da aplicação. A execução
com `-race` passou após corrigir a origem do diálogo de inicialização no root.
