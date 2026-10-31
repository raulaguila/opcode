# Trabalhar pela interface

Os menus compartilham a apresentação do painel de pergunta: bordas retas,
superfície distinta, títulos de destaque, seleção discreta e uma divisória
antes dos controles quando há espaço. Permissões continuam começando em
Recusar/Cancelar e avisos/diffs mantêm cores próprias. Telas de terminal e
workspace não ganham bordas externas que desloquem suas coordenadas.
Temas claros, escuros e `NO_COLOR` preservam rótulos e marcadores de seleção.
As adaptações do conjunto Charm v2 estão no [guia de dependências](dependencies.md).

Digite `/` no começo do campo para ver os comandos. Use `↑`/`↓` para escolher,
`Tab` para completar, `Enter` para executar e `Esc` para fechar as sugestões.
`Ctrl+K` abre a paleta. Os painéis mantêm o rascunho enquanto você consulta o
processo, contexto e arquivos. `Esc` volta ao painel anterior, mantendo seleção e rolagem; na raiz, volta à conversa.

A barra inferior reúne os controles: clique no modo à esquerda para escolher
Perguntar/Planejar/Executar, no modelo à direita para abrir seu catálogo e na
ajuda central para buscar comandos. Durante o trabalho, o centro mostra a
atividade e `Esc` para cancelar; instruções pendentes aparecem com sua contagem
e o clique abre a fila. O menu `/` começa com ações comuns; digite o nome de um
comando para encontrar os demais ou use `/commands` para pesquisar na paleta.
`Ctrl+?` mantém a ajuda completa de atalhos.

O menu `/` e a paleta compartilham a busca em português/inglês (com ou sem
acentos), os identificadores dos comandos e a prioridade conforme a situação: terminal/fila durante a execução,
recuperação após uma falha e revisão quando há um plano. Uma descrição explica a
opção selecionada; escolher uma ação continua passando pelos controles normais
de modo e permissão. `/cancel` solicita o cancelamento do trabalho atual.

O campo reserva três linhas úteis e cresce até oito conforme o texto ou a
colagem. Anexos usam até duas linhas de prévia; todos continuam anexados.
Telas muito baixas reduzem essa reserva para manter a conversa visível.
Ao abrir um diálogo, o campo perde o foco e preserva o rascunho; ao fechar,
o foco retorna. Perguntas e terminal recebem as teclas apenas quando ativos.
O seletor de modos tem título próprio e marca o modo atual; `/mode` também o abre.

Contexto, custos, integração LSP e Git ficam nos painéis correspondentes. O
rodapé não repete “Pronto”, custo zero ou ausência de diagnósticos. Avisos,
erros, resultados parciais e pedidos de aprovação/resposta permanecem visíveis
quando exigem atenção. O campo mantém três linhas iniciais.

## Provedores, modelos e integrações

| Ação | Controle |
| --- | --- |
| Cadastrar/editar conexão e preferências | `/settings` ou `/providers` |
| Excluir conexão sem modelos em uso | `D`/`Delete` na lista, com confirmação |
| Escolher modelo | Clique no modelo do rodapé, `Ctrl+O` ou `/models` |
| Trocar conexão no seletor | `Alt+←`/`Alt+→`, `Shift+Tab`/`Tab` ou metades do título |
| Buscar, marcar favorito, atualizar catálogo | Digitar, `Ctrl+F`, `Ctrl+R` |
| Busca aproximada de modelos | Digite parte das letras na ordem, por exemplo `llm8b` para `llama3.1:8b` |
| Mostrar/ocultar detalhes técnicos do modelo | `Ctrl+D` no seletor |
| Escolher função principal ou auxiliar | `Ctrl+A`: coder, task, title, summarizer |
| MCP e servidores de linguagem | `/integrations` ou `I` em Settings |
| Testar/salvar formulário | `Ctrl+T` / `Ctrl+S`; `Tab` alterna campos |

Sem um modelo, a primeira abertura mostra Settings. Salvar uma conexão ativa leva ao
catálogo dessa conexão, mesmo quando outro provedor era o ativo. `Ctrl+P` no
seletor edita a conexão visível; `Esc` volta à lista de conexões e outro `Esc`
retorna ao catálogo com busca e função preservadas. Edições não salvas podem ser
retomadas com `R` na lista. Conexões desativadas aparecem identificadas na lista e continuam
desativadas ao salvar; `Ctrl+G` salva e ativa explicitamente. Autenticação permite chave direta mascarada, referência a variável de ambiente
ou nenhuma chave. A chave direta é gravada no JSON local com permissão `0600`;
a referência de ambiente não grava o segredo resolvido. Provedores/perfis e preferências são salvos para a próxima abertura;
O formulário básico mostra nome, URL, autenticação e **Conexão ativa**; `Ctrl+A` revela ID,
backend, protocolo e limites. Nas APIs compatíveis, o formulário básico também
mostra **TLS inseguro**, desligado por padrão, com aviso ao habilitar. MCP usa os ajustes na chamada seguinte, enquanto servidores LSP ativos exigem
reinício. Testes MCP/LSP pedem aprovação, têm prazo e encerram o cliente.
Trocar o provedor limpa a lista imediatamente. Se o catálogo falhar, a lista fica
vazia com uma mensagem de falha; `Ctrl+R` tenta novamente e `Tab` navega para outra conexão.
Salvar a configuração, consultar o catálogo e testar uma chamada de ferramenta
são resultados diferentes. O catálogo não certifica a qualidade do modelo.

Sem filtro, o catálogo coloca primeiro o modelo atual daquela função, seguido
dos favoritos, recentes e demais modelos. Os rótulos aparecem quando há largura;
os indicadores do atual/favorito/recente permanecem. Uma busca usa a relevância
do texto. A seleção destacada só vira o modelo ativo após confirmar. Contexto e origem
aparecem no modo básico. A capacidade anunciada no catálogo é separada do limite
efetivo do modelo principal em uso; a alocação informada pelo servidor recebe seu
próprio rótulo. Outros modelos mostram que o limite efetivo ficará disponível
após ativá-los. Esses dados não alteram a configuração. Também aparece o suporte a ferramentas informado pelo provedor
(ou “não informado”). O Opcode consulta metadados, sem deduzir capacidades pelo
nome e sem executar inferência para preencher esses campos.

Booleanos e opções conhecidas usam `←`/`→` ou espaço, sem exigir digitar valores
JSON. Em Argumentos de MCP/LSP, `Enter` abre uma lista: cada linha representa um
argumento literal, inclusive espaços. `Ctrl+N` adiciona, `Alt+Backspace` remove,
`Ctrl+S` aplica ao formulário e `Esc` descarta a edição da lista. Salvar o
formulário continua sendo uma ação separada; editar argumentos não inicia um
servidor nem concede aprovação.

Os detalhes e exemplos de JSON estão em [provedores e modelos](model-selection.md)
e [integrações MCP/LSP](integrations-settings.md). Salvar ajustes durante uma
execução é recusado; o formulário permanece para você tentar depois.

## Conversa, processo e terminal

`Enter` envia a mensagem. `Shift+Enter` insere uma nova linha em terminais que
informam as teclas modificadoras; o Opcode negocia esse suporte ao iniciar e
restaura o modo do terminal ao sair. `Alt+Enter` e `Ctrl+J` também inserem linhas,
e a combinação legada `\` seguida de `Enter` continua disponível.
O rodapé mostra `Alt+Enter` enquanto o terminal não confirmar suporte a teclas
modificadoras; depois da confirmação, mostra `Shift+Enter`.

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

O editor externo (`Ctrl+E`) devolve o texto ao rascunho; fechar o editor não
envia a mensagem. `EDITOR="code --wait"` aceita argumentos literais e caminhos
entre aspas, sem shell ou expansão de variáveis. Revise e pressione Enter para
enviar. `Ctrl+V` também funciona nos campos da paleta e das preferências.

Esse ajuste do VS Code vale para seus terminais em foco. O Opcode não altera
atalhos globais do editor automaticamente.

A conversa intercala mensagens públicas com grupos consecutivos de ferramentas
na ordem em que foram registradas. Uma mensagem do modelo encerra o grupo
anterior; novas chamadas abrem outro grupo. O diagnóstico e a confirmação
posterior, como “deixamos as mudanças para depois”, permanecem como mensagens
separadas, inclusive ao reabrir a conversa. Recolher ferramentas preserva esses
textos. Clique no cabeçalho de cada grupo para expandir/recolher; `Ctrl+B` ou
`/process` alterna o grupo mais recente. Os grupos começam recolhidos; uma
preferência explícita `showProcess: true` mantém a abertura durante a execução.
O resumo distingue leituras e buscas de contexto, fontes reutilizadas, chamadas
de terminal, outras ferramentas e reparos. Por exemplo, “3 leituras de contexto”
descreve a preparação inicial; não significa três comandos ou três chamadas ao
modelo. Detalhes em [perguntas e evidências](question-and-evidence-recovery.md).
Reparos internos de protocolo, conclusão e citações ficam identificados como
validação da aplicação dentro do processo. Não aparecem como perguntas suas,
não entram no histórico de rascunhos e não substituem as validações. O texto da
resposta permanece acessível durante “Validando citações…”. Falhas recebem
“Citações não validadas”. Uma correção direta de referências usa o mesmo bloco:
o original continua visível com “Revisando referências…” até a nova versão passar
na validação. Depois, a versão corrigida vira principal e “Ver versão anterior”
permite abrir a anterior por clique ou `Alt+V`. Uma falha conserva o original
com aviso e “Ver tentativa de correção” para consultar a tentativa separadamente.
Quando o original está carregado, copiar a resposta principal segue essa seleção
e não copia o candidato pendente. Visibilidade não significa aprovação da resposta.
Esses estados usam metadados; textos antigos sem metadados não recebem um estado
de validação inferido pelo conteúdo.

O vínculo da revisão é explícito e permanece ao reabrir a sessão. Não altera
o conteúdo original nem gera chamadas ao modelo ao expandir versões. Mensagens
de progresso continuam na linha do tempo; releituras nativas de fontes podem
continuar a mesma revisão. Uma nova instrução, pergunta, mudança de workflow ou
chamada fora desse conjunto estreito conserva as respostas independentes. Em
registros antigos já marcados como substituídos, a versão anterior pode ficar
recolhida separadamente, sem inventar vínculos com outra mensagem. Se a correção
ou a origem não estiver carregada no histórico, o texto disponível permanece
acessível. Atalhos globais personalizados mantêm precedência sobre `Alt+V`.

Perguntas ao usuário aparecem acima do input em painel com borda, fundo distinto
e o estado “Aguardando sua resposta”. Setas e marcadores indicam as opções; nada
é enviado por pré-seleção. A última opção é o campo vazio “Outra resposta”.
Use `↑`/`↓` para selecioná-lo ou comece a digitar; o marcador e o foco mudam
para esse campo. Enter envia somente a opção ativa: a sugestão selecionada ou
o texto escrito. Voltar para uma sugestão conserva o rascunho da resposta livre,
mas ele não substitui a sugestão no envio. O campo vazio não pode ser enviado.
Em seleção múltipla, o texto livre é uma alternativa às opções marcadas. `F2`
abre o conteúdo completo com rolagem; `F2`/`Esc` volta ao painel. `Esc` no painel
normal cancela a geração. Perguntas agrupadas mantêm a revisão antes do envio.
Depois da resposta, a conversa mantém a pergunta original e “Você respondeu: …”
juntas, com o rótulo “Pergunta do agente” e pipe na cor de destaque do tema,
distinta das mensagens comuns. Os rótulos preservam a distinção sem cores.
Perguntas agrupadas mantêm cada resposta vinculada à sua pergunta. O conjunto
continua visível com ferramentas recolhidas e ao reabrir a sessão; histórico
incompleto mostra a resposta real sem inventar uma pergunta. Esse painel não
concede permissões para executar ferramentas.
Ao rolar para cima, novas mensagens não puxam a leitura para o fim. Use
`Alt+End`, `/latest` ou o indicador de novas mensagens para acompanhar o final.
`Alt+PgUp`/`Alt+PgDn` rolam meia página; `Ctrl+U`, `Ctrl+D` e `Ctrl+End` ficam
com a edição do rascunho, sem mover a conversa.

Clique simples na resposta não abre menus. Arraste o botão esquerdo para destacar
um trecho, inclusive entre linhas. Selecionar não copia automaticamente.
`Alt+S` inicia a seleção pelo teclado na área visível: setas movem o cursor,
`Shift+setas` amplia o trecho e `Home`/`End` e `PgUp`/`PgDn` navegam pelas linhas.
`/select` oferece a mesma entrada pela paleta. `Alt+F` ou `/search texto` busca
na conversa renderizada; `Enter`/`↓` avança, `↑` volta e `Esc` devolve o foco ao
rascunho intacto. A busca é literal, ignora maiúsculas e não envia texto ao modelo.
Ela consulta até 10 mil linhas/2 MiB e mostra até 200 ocorrências; um aviso indica
quando há esse limite. Processos recolhidos ficam fora da busca. `Alt+S` em uma
ocorrência inicia a seleção naquele ponto. Os atalhos não interceptam um PTY,
pergunta ou modal ativo. Se `Alt+F`/`Alt+S` já estiver atribuído a um atalho global
personalizado, ele mantém a prioridade; use `/search` ou `/select` nesse caso.
`Ctrl+C` copia somente o trecho destacado e mantém o destaque; `Esc` limpa a
seleção antes de cancelar uma tarefa. Sem seleção, os atalhos normais continuam
valendo. No PTY, `Ctrl+C` continua interrompendo o processo filho. Durante uma
seleção, a área de leitura fica estável enquanto os novos dados chegam; ao limpar,
o conteúdo é atualizado. Redimensionar ou trocar de conversa limpa a seleção,
pois as coordenadas das linhas mudam. `Shift+arrastar` permite usar a seleção
nativa nos terminais que oferecem esse gesto; nesse caso, use a cópia do próprio
terminal, como `Ctrl+Shift+C`.

Use `Ctrl+Y` ou `/actions` para abrir ações: copiar texto, copiar blocos
de código, selecionar texto e abrir arquivos citados quando encontrados. A
prévia de arquivo usa as permissões normais e não conta como leitura do agente
para permitir uma edição. Quando o clipboard gráfico local não está disponível
ou falha, a cópia solicita o clipboard do terminal via OSC 52 e informa que o
recebimento não é confirmado. Em SSH, usa somente essa rota, sem executar helpers
no servidor. Se o terminal também bloquear a cópia, use a seleção nativa com
`Shift+arrastar` ou a opção de selecionar texto.
Uma indicação discreta abaixo da última resposta concluída torna essas ações
visíveis. Blocos de prosa usam a largura disponível da conversa por padrão;
`tui.proseWidth` permite limitar a leitura de 1 a 240 colunas (por exemplo, 80
ou 100), com `0` restaurando a largura disponível. Entre blocos de Markdown há
uma linha vazia, sem acumular o espaçamento do renderizador; quebras e linhas
vazias dentro dos blocos mantêm sua formatação. Código e tabelas
conservam a largura disponível, mesmo em respostas mistas. Listas e citações
em bloco que contêm código/tabelas mantêm sua estrutura e largura. Links de
referência são resolvidos no documento completo. A ação Copiar resposta usa o
texto integral; `Ctrl+C` com seleção usa somente o trecho destacado na tela.

Títulos renderizados usam negrito sem os marcadores `#`. No tema Opcode,
parágrafos e ênfases têm cores neutras; código em linha e links usam ciano
discreto, com variantes clara e escura. As cores de sintaxe dentro de blocos de
código continuam distintas. Listas alinham as linhas seguintes ao texto depois
do marcador, preservam numeração, tarefas e aninhamento, e separam itens longos
sem espaçar desnecessariamente itens curtos. Listas dentro de citações em bloco
mantêm a renderização do contêiner do Glamour.

O painel de pergunta usa borda reta, seleção discreta e uma linha divisória
acima dos atalhos. A pergunta fica separada das opções quando há espaço;
`F2` mantém acesso a explicações e opções extensas. Em terminais pequenos,
espaços e divisórias cedem lugar à pergunta, à escolha ativa e à resposta.
Não há opção pré-selecionada: a confirmação continua dependendo da escolha
explícita ou do texto digitado no último campo.

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

`/context` começa informando se há espaço estimado para continuar, o total previsto,
a reserva de resposta e a origem do limite efetivo. Capacidade anunciada e alocação
atual no Ollama são informações distintas. `D` abre/fecha os dados técnicos
(schemas, margem, componentes e calibração); Índice e Diagnóstico também
começam com resumo e deixam o JSON em detalhes.
No painel, `C` solicita compactação e `Ctrl+R` atualiza. `/compact` abre diretamente
a compactação. A conversa salva é preservada; a preparação das próximas
requisições abrevia apenas conteúdos antigos permitidos. A contagem é uma
estimativa conservadora, não uma medição pelo tokenizer do modelo. Se o pedido
continuar grande demais, o agente recusa antes da rede.

As teclas `1`–`4` alternam Orçamento, Índice, Alocação e Diagnóstico nos painéis
de contexto. A aba de orçamento inclui até 12 intervalos lidos recentemente,
obtidos dos recibos existentes. São histórico de leitura, não garantia de que
esses trechos estarão na próxima requisição. Consultar o painel não lê novos
arquivos, não compacta sozinho e não altera a validação das citações.

`/mode ask`, `/mode plan` e `/mode execute` escolhem Perguntar, Planejar e Executar.
Os dois primeiros não recebem ferramentas de escrita ou comandos. `/plan` e
`/resume` mostram ou retomam o plano persistente, sem restaurar aprovações antigas.
`/task` reúne plano, atividade, fila e atalhos para contrato, alterações, checks,
permissões, contexto e terminal. O painel atualiza o progresso sem mover a seleção;
`Ctrl+R` também permite atualizar. **Ajustar plano** muda para Planejar e prepara
um rascunho somente se o campo estiver vazio. **Executar com revisão** pede
confirmação explícita para mudar para Executar e continuar; comandos e edições
ainda passam pelas permissões normais. Rascunhos e anexos não enviados são
preservados. `/task new` continua criando uma tarefa explicitamente. A troca de modo/modelo é recusada
enquanto há execução.

## Alterações, verificações e permissões

Cada nova aprovação começa em **Recusar**. `Esc` ou `D` recusa; `Tab`/setas
alternam opções e Enter confirma a opção marcada. Clique no botão também
responde à solicitação exibida. “Repetir escopo exato” não muda parâmetros nem
concede autorização geral. O marcador `›` continua visível sem cores.

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

Pedidos genéricos de aprovação mostram alvo, escopo e parâmetros, com redação
de credenciais conhecidas e limite de tamanho na prévia. A apresentação não
modifica os parâmetros autorizados. Aprovar uma vez ou lembrar na sessão
continua valendo somente para a solicitação/escopo correspondente.

## Gerenciar conversas e recuperar falhas

As conversas são ordenadas por recência. A data aparece quando há espaço; a
prévia carrega somente para a conversa selecionada e mostra a última mensagem
humana, omitindo instruções internas de reparo.

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
arquivada. `opcode --resume` faz essa escolha para a abertura atual, dentro do
projeto; sem conversa disponível, informa isso e inicia uma nova. Para uma
conversa específica, use `opcode --resume ID` ou `opcode --resume=ID`.
Uma retomada interativa não reexecuta ferramentas nem restaura aprovações.
`/resume` na conversa pede a continuação da tarefa salva.

`/quit` inicia o fluxo normal de encerramento, salvando o rascunho antes de
sair e respeitando operações protegidas em andamento. No cliente `attach`, o
mesmo comando apenas desconecta, mantendo a execução no servidor. Rascunhos
são separados por conversa; `↑`/`↓` percorre o histórico de entrada e preserva o
rascunho original e seus anexos ao voltar.

Uma execução interrompida deixa ações junto ao campo: `/error` abre detalhes,
`/settings` permite corrigir a conexão e `/retry` reenvia o último pedido com
instrução para conferir o estado atual antes de continuar. Não é um reenvio
automático nem uma garantia de evitar todo efeito repetido: permissões,
verificações e inspeção do estado continuam necessárias. O plano e os arquivos
já alterados permanecem disponíveis para revisão.

O aviso junto ao campo usa uma causa curta e ações: falha de citações, orçamento
de contexto, memória insuficiente no servidor, conexão ou autenticação são situações
diferentes. Detalhes técnicos ficam em `/error`. Ao resolver a falha, o aviso
deixa o campo livre; o diagnóstico continua disponível para consulta.
Uma falha de citação que menciona preparação de contexto não indica, por si só,
que a janela do modelo acabou; a barra inferior continua mostrando o orçamento.

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
    "proseWidth": 0,
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

Notificações são optativas e só acontecem quando a aplicação está sem foco:

- `off` é o padrão; `bell` emite o sinal do terminal.
- `native` tenta a notificação do sistema em Linux/macOS, com prazo de três
  segundos e fallback para `bell` quando indisponível.
- `auto` usa `native` localmente e `bell` quando detecta SSH.
- `osc` envia OSC 777 explicitamente; depende do suporte do emulador.

A entrega depende de o emulador informar mudanças de foco e permitir a
notificação. Só uma frase fixa da aplicação é enviada, sem conteúdo da conversa,
caminhos ou credenciais. Em Temas, `↑`/`↓` mostra uma prévia, `Enter` salva e
`Esc` restaura o tema anterior sem gravar a prévia.

A barra da tarefa distingue consulta ao modelo, escrita da resposta, conferência
de fontes, aprovação, comando, verificação e
pergunta ao usuário. O resultado pode ser concluído, planejado, parcial ou
bloqueado. Itens em `/queue` continuam **pendentes** até o agente incorporá-los;
o aviso de incorporação não transforma a fila em uma segunda execução. O tempo
decorrido acompanha a execução; adicionar uma instrução não reinicia o relógio
nem troca o status da operação em andamento.

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

## Menus consistentes e terminais compactos

O seletor de modo mostra as três opções diretamente. Enter aplica a escolha;
texto e colagem não criam uma busca invisível. A paleta identifica a categoria
da ação e o comando correspondente. Listas usam `›` para foco, inclusive sem cores.

O catálogo evita categorias repetidas nos demais modelos e mostra contexto em
unidades legíveis, mantendo o valor exato e sua origem. Conexões oferecem ações
clicáveis Salvar, Catálogo e Mais, além dos atalhos. Preferências e Integrações
ficam acessíveis na lista de conexões. Preferências mostram a categoria selecionada
como seção e usam nomes humanos no editor.

Provedores e integrações identificam alterações não salvas. Esc retorna à lista;
`R` retoma a edição preservada enquanto o aplicativo continua aberto. Salvar é
explícito. Mensagens longas têm resumo e `Ctrl+D` abre detalhes roláveis, sem
interromper o recebimento de resultados de testes.

Em MCP, os campos acompanham o transporte: processo local mostra comando,
argumentos e ambiente; remoto mostra URL e cabeçalhos. Os campos OAuth aparecem
quando habilitados. Enter em cabeçalhos/ambiente abre uma lista `nome=VARIAVEL`;
os valores continuam sendo referências a variáveis de ambiente. Nomes repetidos
são recusados. Nenhum teste é disparado ao mudar um campo.

Arquivos começam no diretório do projeto. A prévia aparece quando há largura;
em telas pequenas, a lista ocupa o espaço disponível. Pasta vazia é um estado
válido: Enter não causa erro nem envia mensagem. O cursor é preservado no resize.
Argumentos de comandos personalizados mantêm o campo focado e os valores ao
redimensionar. Conversas mantêm a seleção visível; clique seleciona a prévia e
Enter abre. Temas recebem resize e preservam a prévia reversível. Esc ou Continuar
cancela o diálogo Sair.

Painéis mostram a localização anterior e até três ações por vez. `M` expande ou
recolhe a lista; as setas continuam alcançando as demais ações. Cliques usam as
mesmas posições da renderização. Tarefa, contexto, índice, diagnóstico, alterações,
verificações, fila, permissões, Git, worktrees, fontes e skills compartilham esse
comportamento. `/skills termo` filtra nomes e descrições; escolher prepara o rascunho.

No resultado esperado, Enter abre a edição de arquivos, critérios ou verificações
por itens. `Ctrl+N` adiciona, `Alt+Backspace` remove e `Ctrl+S` aplica ao formulário;
outro `Ctrl+S` salva o contrato completo. Itens existentes com vírgulas ou ponto e
vírgula são preservados. As validações do contrato continuam sendo aplicadas.
Perguntas mantêm a opção focada visível em telas baixas. O terminal identifica a
entrada ativa e, com vários processos, mostra seus identificadores e a seleção.
A ajuda agrupa conversa/edição, trabalho/terminal e configurações.

A validação inclui testes de componentes, roteamento pelo aplicativo e PTY com
Ollama em temas claro, escuro e NO_COLOR. A matriz não certifica todos os terminais
ou tecnologias assistivas. As evidências estão em
[evidência dos menus](evidence/2026-10-29-menu-ui-validation.json).


## Contexto e velocidade no rodapé

À esquerda do modelo, a barra mostra, por exemplo,
`~8k/32k ctx · 24.5 tok/s`. Clique na informação de contexto para abrir `/context`.
O denominador é a janela efetiva do pedido quando disponível; antes da primeira
mensagem, usa a janela do provedor vinculado ao agente. Sem agente ativo, usa o
limite configurado/conhecido como referência. Não é uma indicação de memória livre.
`k` representa 1024 tokens, como no seletor de modelos.

O numerador representa a entrada do último pedido: sem `~`, veio do uso informado
pelo provedor; com `~`, é a estimativa do orçamento. Não soma uma conversa inteira
sem considerar a recuperação e a compactação. `—` indica dado ainda indisponível.
A renderização usa dados em memória e não consulta o provedor a cada quadro.

Durante a geração, `~24.5 tok/s` é uma estimativa da chamada em andamento,
atualizada pelo ciclo de renderização existente (100 ms). A estimativa divide os
bytes UTF-8 recebidos por quatro e pelo tempo desde o primeiro dado de saída.
Não conta pacotes como tokens e não é um tokenizer; pode diferir bastante entre
modelos e idiomas. Texto, raciocínio e deltas de argumentos disponibilizados pelo
adaptador contam uma vez, sem reter o conteúdo nas métricas. Os primeiros 250 ms
mostram `Gerando…` para evitar picos. Antes de qualquer saída, aparece
`Aguardando modelo…`; um servidor que acumula a resposta ou uma chamada de
ferramenta até o final pode permanecer nesse estado, sem taxa inventada.

Ao concluir, a estimativa dá lugar à média da última chamada concluída do modelo
atual na sessão. No Ollama, usa `eval_count` dividido por `eval_duration`. Quando
falta duração do provedor, `~` identifica a média observada pelo cliente, incluindo
espera pelo primeiro token e transporte, sem o tempo posterior de ferramentas ou
aprovações. Tokens de saída podem incluir argumentos de chamadas de ferramentas;
não representam apenas palavras visíveis. Ferramentas e aprovações mostram `—`,
sem reutilizar uma chamada anterior enquanto o agente trabalha. As métricas de
streaming são removidas ao concluir, cancelar ou falhar e reiniciadas a cada
chamada, inclusive nas correções de resposta. Em terminais estreitos, primeiro a
velocidade e depois o contexto são ocultados para preservar os controles. O input
mantém sua altura.
