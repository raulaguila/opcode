# Trabalhar pela interface

Digite `/` no começo do campo para ver os comandos. Use `↑`/`↓` para escolher,
`Tab` para completar, `Enter` para executar e `Esc` para fechar as sugestões.
`Ctrl+K` abre a paleta. Os painéis mantêm o rascunho enquanto você consulta o
processo, contexto e arquivos; `Esc` volta à conversa.

A barra inferior reúne os controles: clique no modo à esquerda para escolher
Perguntar/Planejar/Executar, no modelo à direita para abrir seu catálogo e na
ajuda central para buscar comandos. Durante o trabalho, o centro mostra a
atividade e `Esc` para cancelar; instruções pendentes aparecem com sua contagem
e o clique abre a fila. O menu `/` começa com ações comuns; digite o nome de um
comando para encontrar os demais ou use `/commands` para pesquisar na paleta.
`Ctrl+?` mantém a ajuda completa de atalhos.

Contexto, custos, integração LSP e Git ficam nos painéis correspondentes. O
rodapé não repete “Pronto”, custo zero ou ausência de diagnósticos. Avisos,
erros, resultados parciais e pedidos de aprovação/resposta permanecem visíveis
quando exigem atenção. O campo mantém três linhas iniciais.

## Provedores, modelos e integrações

| Ação | Controle |
| --- | --- |
| Cadastrar/editar conexão e preferências | `/settings` ou `/providers` |
| Escolher modelo | Clique no modelo do rodapé, `Ctrl+O` ou `/models` |
| Trocar conexão no seletor | `←`/`→` ou metades do título |
| Buscar, marcar favorito, atualizar catálogo | Digitar, `Ctrl+F`, `Ctrl+R` |
| Busca aproximada de modelos | Digite parte das letras na ordem, por exemplo `llm8b` para `llama3.1:8b` |
| Mostrar/ocultar detalhes técnicos do modelo | `Ctrl+D` no seletor |
| Escolher função principal ou auxiliar | `Ctrl+A`: coder, task, title, summarizer |
| MCP e servidores de linguagem | `/integrations` ou `I` em Settings |
| Testar/salvar formulário | `Ctrl+T` / `Ctrl+S`; `Tab` alterna campos |

Sem um modelo, a primeira abertura mostra Settings. Salvar uma conexão leva ao
seletor. Credenciais usam referências a variáveis de ambiente, sem exibir seus
valores. Provedores/perfis e preferências são salvos para a próxima abertura;
O formulário básico mostra nome, URL e variável da chave; `Ctrl+A` revela ID,
backend, protocolo e limites. MCP usa os ajustes na chamada seguinte, enquanto servidores LSP ativos exigem
reinício. Testes MCP/LSP pedem aprovação, têm prazo e encerram o cliente.

Os detalhes e exemplos de JSON estão em [provedores e modelos](model-selection.md)
e [integrações MCP/LSP](integrations-settings.md). Salvar ajustes durante uma
execução é recusado; o formulário permanece para você tentar depois.

## Conversa, processo e terminal

`Enter` envia a mensagem. `Shift+Enter` insere uma nova linha em terminais que
informam as teclas modificadoras; o Opcode negocia esse suporte ao iniciar e
restaura o modo do terminal ao sair. `Alt+Enter` e `Ctrl+J` também inserem linhas,
e a combinação legada `\` seguida de `Enter` continua disponível.

Se o terminal enviar `Shift+Enter` como um `Enter` comum, use `Alt+Enter`. No
terminal integrado do VS Code, você também pode adicionar manualmente esta
entrada ao `keybindings.json`, conforme os
[atalhos de envio de sequências](https://code.visualstudio.com/docs/terminal/advanced#_custom-sequence-keyboard-shortcuts):

```json
{
  "key": "shift+enter",
  "command": "workbench.action.terminal.sendSequence",
  "args": { "text": "\u001b\r" },
  "when": "terminalFocus"
}
```

Esse ajuste do VS Code vale para seus terminais em foco. O Opcode não altera
atalhos globais do editor automaticamente.

Cada interação agrupa a pergunta, o processo e a resposta final. Clique no
cabeçalho do processo para expandir/recolher. `Ctrl+B` ou `/process` alterna o
processo da última interação. Ele começa recolhido; uma preferência explícita
`showProcess: true` mantém a abertura durante a execução.
Ao rolar para cima, novas mensagens não puxam a leitura para o fim. Use
`Ctrl+End`, `/latest` ou o indicador de novas mensagens para acompanhar o final.

Clique na resposta, use `Ctrl+Y` ou `/actions` para abrir ações: copiar texto, copiar blocos
de código, selecionar texto e abrir arquivos citados quando encontrados. A
prévia de arquivo usa as permissões normais e não conta como leitura do agente
para permitir uma edição. Se a área de transferência estiver indisponível,
use a opção de selecionar texto.

O terminal mantém os controles existentes: `Ctrl+P` abre/fecha o painel,
`Ctrl+N` cria um PTY, `Alt+←`/`Alt+→` alterna processos e `Ctrl+X` encerra o processo
selecionado. Enquanto o PTY tem foco, suas teclas vão ao processo filho. Iniciar
comandos continua exigindo o modo Executar, permissão e sandbox.
Ao voltar à conversa, a saída fica no terminal, acessível pelo mesmo atalho;
o rodapé indica se há um terminal ativo sem reservar linhas de logs no chat.

## Instruções enquanto o agente trabalha

Você pode enviar outra instrução durante uma execução. Ela entra na próxima
fronteira entre operações; chamadas e resultados de ferramentas permanecem
ordenados. `/queue` mostra os itens ainda pendentes:

- `↑`/`↓` seleciona, `Enter` retira um item para editar e `Delete` remove.
- A edição só substitui um rascunho vazio; envie ou limpe seu rascunho antes.
- Se o agente já consumiu o item, a ação avisa sem duplicá-lo.
- Reenviar passa pelo fluxo normal de envio e confirmação. A fila comporta até
  oito instruções e 1 MiB de conteúdo.

## Contexto e modos

`/context` mostra janela, entrada estimada, schemas, reserva de resposta e margem.
No painel, `C` solicita compactação e `Ctrl+R` atualiza. `/compact` abre diretamente
a compactação. A conversa salva é preservada; a preparação das próximas
requisições abrevia apenas conteúdos antigos permitidos. A contagem é uma
estimativa conservadora, não uma medição pelo tokenizer do modelo. Se o pedido
continuar grande demais, o agente recusa antes da rede.

`/mode ask`, `/mode plan` e `/mode execute` escolhem Perguntar, Planejar e Executar.
Os dois primeiros não recebem ferramentas de escrita ou comandos. `/plan`,
`/task` e `/resume` mostram ou retomam o plano persistente, sem restaurar
aprovações antigas. A troca de modo/modelo é recusada enquanto há execução.

## Alterações, verificações e permissões

`/changes` ou `/checks` mostra interações com alterações e os recibos de validação.
`Enter` abre os diffs de uma interação. `U` prepara o undo da última interação
aplicável e `R` prepara o redo; `/undo` e `/redo` abrem a mesma prévia.

Revise a prévia e use `Ctrl+Enter` (`Ctrl+J` em terminais que não distinguem a
combinação) para **solicitar** as aprovações dos diffs por arquivo. Cada aprovação
segue o diálogo normal de permissões. `Esc` cancela a prévia/espera. Conteúdo
alterado externamente gera conflito em vez de sobrescrever silenciosamente.
Após aplicar, execute novamente as verificações: os recibos anteriores são
invalidados. O recurso reverte edições de arquivos das ferramentas; não reverte
comandos, processos, serviços externos ou alterações manuais.

`/permissions` mostra autorizações lembradas. `R` revoga autorizações e pedidos
pendentes; não desfaz arquivos. Descobrir checks não os executa nem registra
sucesso. Veja [ferramentas e validações](code-tools.md).

## Gerenciar conversas e recuperar falhas

`/sessions` abre a busca por título/ID. No seletor:

| Controle | Ação |
| --- | --- |
| Digitar, `↑`/`↓`, `Enter` | Buscar e abrir uma conversa |
| `Ctrl+R` | Renomear; `Enter` confirma, `Esc` cancela |
| `Ctrl+A` | Arquivar/desarquivar a seleção |
| `Ctrl+H` | Mostrar/ocultar arquivadas |
| `Ctrl+E` | Exportar a seleção em Markdown |

`/export` exporta a conversa atual em `data.directory/exports`. A exportação
contém o processo como histórico, redige credenciais conhecidas e não concede
permissões. Settings permite retomar a última conversa usada, quando ela não estiver
arquivada. Rascunhos
são separados por conversa; `↑`/`↓` percorre o histórico de entrada e preserva o
rascunho original e seus anexos ao voltar.

Uma execução interrompida deixa ações junto ao campo: `/error` abre detalhes,
`/settings` permite corrigir a conexão e `/retry` reenvia o último pedido com
instrução para conferir o estado atual antes de continuar. Não é um reenvio
automático nem uma garantia de evitar todo efeito repetido: permissões,
verificações e inspeção do estado continuam necessárias. O plano e os arquivos
já alterados permanecem disponíveis para revisão.

## Skills, hooks e avaliações

`/skills` lista as skills dos diretórios cadastrados no JSON global. `Enter`
prepara `/skill NOME` no rascunho; acrescente os argumentos e envie para invocar.
Skills não se ativam por decisão do modelo e não concedem permissões. Hooks
`before_tool`/`after_tool` são opt-in no mesmo JSON, executam somente em Executar
e usam os limites, aprovação e sandbox normais. Veja [contexto e extensões](context-and-extensions.md).

O comando abaixo valida offline o contrato das fixtures e gera um relatório:

```sh
go run . evaluate --output /tmp/opcode-fixtures.json
```

Esse modo não executa um agente e não mede a qualidade de um modelo. A avaliação
real exige `--live --provider-config /caminho/provedor.json`; cenários de execução
exigem ainda `--allow-execution`. Ela usa projetos, HOME e bancos temporários,
sem ativar MCP/LSP/skills/hooks do usuário. O relatório registra critérios
observáveis; passar um conjunto não demonstra correção semântica geral. Veja
[avaliações repetíveis](evaluation.md) e [testes reais Ollama](ollama-live-tests.md).

## Preferências e adaptação ao terminal

A conversa tem um cabeçalho compacto com o projeto; o modo fica no rodapé. A
lateral começa fechada. Uma escolha explícita em `tui.sidebar` continua valendo:
`auto` aparece a partir de 140 colunas; `shown` mantém a lateral
quando há pelo menos 110 colunas; `hidden` usa toda a largura. `Alt+B` alterna a
preferência. O campo reserva três linhas para digitação e cresce com linhas e
texto quebrado, até oito linhas quando há espaço, sem substituir o rascunho.
Anexos mantêm sua própria linha. Em terminais baixos, a altura é reduzida para
preservar o espaço da conversa.

Em `/settings`, `P` abre preferências pesquisáveis por categoria, nome ou chave
JSON. `Ctrl+A` mostra limites e atalhos avançados; `Enter` edita; `Ctrl+S` salva.
`Ctrl+R` no editor remove a sobrescrita selecionada e revela o valor da camada
inferior ou o padrão. Configurações já presentes no projeto são alteradas nesse
arquivo; as demais vão para o arquivo global. O arquivo precisa ser um JSON
válido, regular, sem links simbólicos e com até 4 MiB; falhas preservam o arquivo.

`Ctrl+E` na busca mostra valores efetivos e origem: padrão, global, projeto ou
ambiente. Credenciais, cabeçalhos privados e argumentos de processos são
redigidos. Uma preferência controlada pelo ambiente informa o nome da variável;
remova essa sobrescrita antes de alterar o valor na interface.

```json
{
  "tui": {
    "sidebar": "hidden",
    "language": "pt",
    "notifications": "off",
    "keybindings": {
      "commands": "alt+k",
      "terminal": "ctrl+p"
    }
  }
}
```

Os atalhos configuráveis são `commands`, `models`, `sessions`, `terminal`, `files`,
`theme` e `sidebar`; aceitam `ctrl+letra` ou `alt+letra` e não podem se repetir.
Os padrões existentes continuam válidos. O idioma padrão dos controles
localizados é inglês; `pt` seleciona português. Formulários centrais,
preferências, permissões, ajuda e recuperação usam essa seleção. Diagnósticos de
provedores, servidores, ferramentas e algumas mensagens legadas mantêm seu idioma
original; conteúdo do usuário e do modelo nunca é traduzido automaticamente.

Notificações são optativas: `bell` emite o sinal do terminal ao receber uma
aprovação ou conclusão enquanto a aplicação está sem foco; `off` é o padrão.
A entrega depende de o emulador informar mudanças de foco e permitir o sinal.
Nenhum conteúdo da conversa é enviado ao sistema de notificações.

A barra da tarefa distingue investigação, aprovação, comando, verificação e
pergunta ao usuário. O resultado pode ser concluído, planejado, parcial ou
bloqueado. Itens em `/queue` continuam **pendentes** até o agente incorporá-los;
o aviso de incorporação não transforma a fila em uma segunda execução.

Nas aprovações, `T` alterna diff automático, unificado e lado a lado; `F` alterna
tela cheia e `[ ]` navega pelos arquivos. A apresentação automática usa diff
unificado em espaços estreitos. A revisão de uma interação mostra arquivos
selecionáveis e também aceita `T`. Navegar ou mudar a apresentação nunca aprova
uma alteração. A opção de lembrar uma aprovação vale apenas para o escopo e
parâmetros exatos da solicitação naquela sessão.

A recuperação escolhe ações conforme a causa: autenticação oferece configurações;
limite de modelo oferece contexto/modelos; conflito oferece revisão de arquivos;
interrupções oferecem retomada e diagnóstico. No painel de contexto, `R` ou
`/recalibrate` remove o limite aprendido para a conexão/modelo atual, sem executar
inferência nem repetir ferramentas. É necessário aguardar a execução atual.

## Configuração, contrato e Git integrados

- `/setup`: etapas de conexão, modelo e testes opcionais confirmados.
- `/contract`: resultado esperado, arquivos, critérios e checks; Tab muda campo,
  Ctrl+S salva, Esc preserva o rascunho. O painel acompanha terminais pequenos.
- `/local-status`: contexto alocado, CPU/GPU e memória do modelo no Ollama, via
  consulta de metadados sem inferência.
- `/worktrees`: branch e arquivos atuais, diff normal/preparado, recibos e criação
  protegida de worktree. Abrir outra worktree suspende a TUI atual, inicia uma
  instância isolada e retorna à conversa original quando ela termina.

As telas de inicialização, saída, tema, conversas, arquivos e comandos receberam
textos PT/EN. Conteúdo do usuário, mensagens do modelo e erros externos não são
traduzidos por substituição. O [guia diário](daily-workflow.md) detalha os fluxos
e seus limites.


Durante comandos ativos, há uma prévia de saída de até seis linhas, sem transferir
o foco do chat. Ela desaparece ao terminar e fica recolhida em telas com menos de
20 linhas; Ctrl+P mantém o terminal completo disponível. `/index` e `/runtime`
abrem painéis de controle/diagnóstico, sem acrescentar barras permanentes.


Veja também a [evolução comparativa](comparative-evolution.md) para perguntas agrupadas, busca aproximada de modelos, citações verificadas e diagnóstico de linguagens.
