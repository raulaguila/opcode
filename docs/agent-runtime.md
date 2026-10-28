# Modos, planejamento e limites do agente

## Uso

```sh
go run . --mode ask
go run . --mode plan
go run . --mode execute
go run . --resume ID_DA_SESSAO
go run . --resume ID_DA_SESSAO -p "Continue a tarefa e valide os arquivos atuais"
```

Na interface, use a paleta `Ctrl+K` ou as sugestões de comandos com `/`.

Digitar `/` no início de uma mensagem abre as sugestões acima do campo.
Continue digitando para filtrar, use ↑/↓ para navegar, Tab para completar sem
executar e Enter para selecionar. `/mode` oferece os três modos e `/task`
também oferece `/task new`. Esc fecha as sugestões mantendo o texto; outro Esc
pode cancelar a geração, como antes. O menu não abre em caminhos contendo outras
barras nem em mensagens de várias linhas. Também funciona enquanto o agente
trabalha; operações que exigem a sessão ociosa continuam sendo recusadas e
preservam o comando no rascunho.

- `/mode ask|plan|execute`: seleciona Perguntar, Planejar ou Executar.
- `/plan` ou `/task`: mostra/recolhe objetivo, etapas, critérios, verificações e ID da sessão. `Alt+↑/↓` e a roda sobre o painel percorrem o plano.
- `/resume`: pede continuação da tarefa salva, respeitando o modo atual.
- `/task new`: limpa o plano e os registros de validação, preservando a conversa e os arquivos. Revoga as autorizações lembradas e encerra processos anteriores.

Trocas de modo são recusadas enquanto o agente trabalha. Pressione Esc para
interromper antes de trocar. Uma troca revoga permissões lembradas e
autoaprovação da sessão e encerra seus processos, inclusive PTYs. Não concede
autorização para novas operações. Modelos e provedores configurados continuam
os mesmos.

## Capacidades e contexto

Perguntar disponibiliza leitura, busca, navegação e perguntas ao usuário.
Planejar acrescenta o registro do plano; não disponibiliza comandos nem edições.
Executar permite carregar ferramentas mutáveis, mantendo suas permissões e o
isolamento existentes. Integrações MCP sem garantia de leitura ficam restritas
a Executar. Essas restrições são verificadas no executor, inclusive se o modelo
inventar uma chamada para uma ferramenta ausente. Serviços LSP/MCP configurados
continuam tendo o ciclo de inicialização do aplicativo.

O conjunto inicial contém leitura/busca, catálogo e estado da tarefa. O modelo
usa `get_tools_catalog` e `request_tools` para carregar capacidades adicionais.
Pedidos de comandos disponibilizam `bash`; ferramentas de processos são
antecipadas quando há pedido de trabalho em segundo plano ou referência explícita
a essas ferramentas. O carregamento vale por uma execução; não concede permissões.

O prompt de sistema não embute a árvore de arquivos do diretório atual. Listagens
entram por ferramentas, quando necessárias. `get_project_map` oferece `offset`,
`limit`, `next_offset` e `more`; pode ser carregado por `request_tools`.
`search_code_index` mantém um índice local SQLite/FTS5 e retorna resultados
classificados, sem enviar o índice inteiro ao provedor. O índice atual atualiza
os trechos alterados por hash, mas ainda relê os arquivos durante a busca. A
`ls` mantém um limite de 1.000 entradas sem paginação; para continuar uma
exploração, prefira o mapa paginado ou refine o caminho/padrão de busca.

Perguntas que mencionam o projeto/repositório recebem leituras iniciais reais de
até quatro arquivos existentes: README, manifesto, ponto de entrada conhecido e
AGENTS.md. Cada leitura usa `view` com suas proteções, fica no histórico e contém
até 80 linhas/2.400 bytes, respeitando o orçamento restante. Isso oferece fatos
ao modelo; não comprova que ele interpretará o projeto corretamente. Outros
arquivos e entradas incomuns ainda dependem de busca/leitura pelo agente.

As instruções de `contextPaths` também incluem AGENTS.md por padrão. A leitura
automática é limitada a 16 arquivos/16 KiB totais, no máximo 4 KiB por arquivo,
sem arquivos sensíveis ou links externos. Valores conhecidos de credenciais
são removidos. Instruções de projeto não substituem pedidos do usuário nem
autorizam ferramentas. O contexto de instruções continua carregado na criação
do provedor; as leituras iniciais de projeto são renovadas em cada turno aplicável.

## Perguntas ao usuário

`ask_user_question` apresenta até cinco opções e aceita resposta livre. Nada é
enviado por pré-seleção: use setas para escolher ou digite e pressione Enter.
F2 abre os detalhes completos, com rolagem; F2/Esc retorna à resposta. Esc na
pergunta normal cancela a geração. O rascunho principal do chat é preservado.

A resposta retorna como resultado nativo da ferramenta, sem conceder permissões.
Na execução não interativa, a ferramenta informa que precisa da interface;
nenhuma resposta é fabricada. Perguntas pendentes são canceladas ao encerrar
a geração; não ficam aguardando depois de reiniciar.

## Plano e validação

`update_plan` registra objetivo, até 24 etapas, 16 critérios de aceite e nomes
de verificações de `checks`. Apenas uma etapa pode ficar em andamento.
`get_task` consulta esse estado. `finish_task` aceita `planned`, `completed`
ou `blocked`. Para concluir, todas as etapas precisam estar completas e as
verificações exigidas precisam ter passado para a revisão atual.

As verificações usam `run_checks`, com autorização normal de cada comando.
Um recibo registra nome, hash do comando configurado, revisão, impressão dos
arquivos e resultado. Falha, cancelamento, timeout e falta de metadados de exit
code não contam como aprovação. Reexecutar um check invalida seu recibo anterior;
outros checks permanecem disponíveis. Mudar seu comando configurado também
torna o recibo antigo insuficiente.

Uma impressão SHA-256 detecta alterações de conteúdo, criação e remoção dentro
do workspace, inclusive mudanças externas com tamanho/mtime preservados.
Edições pelas ferramentas invalidam a validação no commit, incluindo caminhos
externos explicitamente autorizados. A conclusão consulta novamente os arquivos.
Mudanças exigem verificações atuais; comandos arbitrários de `bash` não são
automaticamente promovidos a testes aprovados. Configure os checks necessários:

```json
{
  "checks": {
    "test": {"command": "go test ./...", "timeout": 120000},
    "vet": {"command": "go vet ./...", "timeout": 120000}
  }
}
```

O hash ignora credenciais, links, armazenamento do aplicativo e diretórios de
dependências/cache/build conhecidos. Limites: 20 mil arquivos, 128 MiB no total e
16 MiB por arquivo. Se a impressão não puder ser produzida, não é possível
declarar validação atual. Alterações externas fora do workspace e em diretórios
excluídos não são detectadas por esse hash. Não há snapshot atômico do filesystem.
Testes aprovados comprovam sua execução; não comprovam cobertura ou correção
semântica de todos os critérios.

O estado fica na tabela SQLite `agent_workflows`, vinculado à sessão e à raiz
do projeto, com limite de 256 KiB por sessão. A migração é aditiva e reversível:
o downgrade remove somente os planos, mantendo sessões e mensagens. Retomar
não reaplica ferramentas anteriores nem restaura autorizações de outra execução.
Processos não sobrevivem ao encerramento do aplicativo.

## Limites configuráveis

Salve uma vez na configuração do usuário ou do projeto:

```json
{
  "runtime": {
    "mode": "execute",
    "maxSteps": 32,
    "maxSeconds": 1800,
    "toolBudget": 96000,
    "firstTokenTimeout": 300,
    "idleTimeout": 90
  }
}
```

Valores omitidos/zero usam os padrões acima. Flags positivas substituem a
configuração nesta inicialização: `--mode`, `--max-steps`, `--max-seconds`,
`--tool-budget`, `--first-token-timeout` e `--idle-timeout`. `--mode` também pode
trocar o modo de uma sessão informada com `--resume`.

`maxSteps` conta requisições ao modelo, incluindo instruções enfileiradas e
tentativas de correção, com intervalo 1–200. `maxSeconds` limita o turno inteiro
a 1–7.200 segundos, incluindo espera por respostas/aprovações. `toolBudget`
limita conteúdo/metadados de resultados a 4.096–1.048.576 bytes por turno;
resultados individuais também são limitados. Não substitui a janela de contexto
do modelo nem estabelece orçamento monetário.

Quatro interações idênticas de ferramenta, argumentos normalizados e resultado
entre as últimas dez interrompem a execução. O limite geral continua valendo
mesmo quando resultados diferentes impedem essa detecção.

O timeout inicial aceita 1–1.800 segundos; o timeout de silêncio entre eventos
aceita 1–600 segundos. Um stream ativo não expira apenas por durar mais que essa
janela. Trabalho local para consumir um evento e execução de ferramentas não
contam como silêncio do modelo. O timeout não reenvia automaticamente a chamada.

Falhas e limites preservam o plano como bloqueado e encerram processos da
sessão. Instruções aceitas ainda pendentes são salvas após resultados de
interrupção para posterior retomada; cancelamento explícito descarta a fila.
Chamadas incompletas recebem resultados de interrupção para manter o protocolo
válido. Isso não desfaz operações já aplicadas.

## Verificação

```sh
go test -race ./...
go vet ./...
```

A suíte cobre restrições dos modos, carregamento progressivo, contexto real,
limites/repetição, perguntas, persistência/migração, invalidação por edição,
recusa de comandos e recuperação do protocolo. O teste da TUI percorre plano,
modo, pergunta e preservação do rascunho sem API externa.

Os testes reais opt-in do Ollama permanecem em
[ollama-live-tests.md](ollama-live-tests.md), executados separadamente por causa
da configuração global do aplicativo.

## Conclusão verificável e trabalho parcial

Pedidos diretos de implementação/edição em Executar registram a impressão dos
arquivos antes da geração. Uma resposta final sem mudança verificável provoca
até duas tentativas de correção, dentro dos mesmos limites da execução. Se o
trabalho continuar incompleto, a execução termina bloqueada em vez de informar
sucesso. Essa identificação de intenção é auxiliar e não concede autorização.
Perguntas, análises e planejamento não exigem uma alteração artificial.

`finish_task` aceita `partial` para trabalho incompleto e `no_change_reason` para
justificar explicitamente uma conclusão sem mudanças (por exemplo, comportamento
já implementado). A justificativa fica salva na tarefa. O evento final carrega
`Outcome` como `completed`, `planned`, `partial` ou `blocked`; uma resposta recebida
não significa automaticamente que a tarefa foi concluída. Uma retomada preserva
a impressão inicial; uma nova solicitação independente começa outra comparação.
Os recibos continuam vinculados à revisão e ao comando efetivamente executado.

A impressão detecta mudanças de arquivos, não a correção semântica da alteração.
Uma justificativa do modelo é um registro explícito, não uma prova independente.

## Exploração separada e limitada

`explore_code` investiga uma pergunta de até 4.000 bytes em um histórico separado.
Disponibiliza somente leitura e navegação local: arquivos, diretórios, busca,
mapa, outline, símbolos e índice textual. Não inclui comandos arbitrários,
escritas, MCP, hooks nem outras chamadas de agentes. O nome legado `agent` usa
os mesmos limites e não cria outro worker independente.

Cada exploração tem no máximo seis etapas de modelo, 120 segundos, oito leituras
por etapa, 32 KiB de resultados lidos e relatório de até 8 KiB. Etapas, resultados,
tempo e custo também pertencem ao orçamento da execução principal. Cancelar o
turno cancela a exploração. O relatório é informação auxiliar: não aprova
operações, não comprova conclusão e deve ser conferido antes de editar arquivos.

## Contrato explícito e planos locais

O usuário pode salvar um contrato pelo painel `/contract` ou pelos flags
`--task-kind`, `--target`, `--accept` e `--check` de um prompt. O contrato fica no
JSON da tarefa e não pode ser configurado por ferramentas do modelo. Sem contrato,
as heurísticas de texto permanecem compatíveis. Ele não amplia o modo atual; um
contrato incompatível é recusado antes de chamar o provedor. Explicar restringe a
leitura; Verificar filtra ferramentas tanto na exposição quanto na execução,
incluindo chamadas forjadas e `request_tools`, e exige recibos válidos.

Para a API nativa do Ollama, `update_plan` anuncia `objective`, `steps_text`,
`criteria_text` e `checks_text` como strings. Etapas usam uma linha `status|title`;
critérios e checks usam uma linha por item. Esse binding evita listas
serializadas como texto por templates locais, sem avaliar literais Python ou
converter argumentos de ferramentas arbitrárias. Outros bindings mantêm arrays
JSON estruturados. Ambos passam pela mesma validação do Store e não autorizam
comandos. O modelo fornece o conteúdo; a aplicação não fabrica o plano.

Ao encerrar uma edição simples sem checks disponíveis, o runtime registra
`partial`, alterações observadas e ausência de validação. Não gera recibos nem
converte esse estado em conclusão. Tarefas com verificações ou planos ativos
continuam sujeitas aos seus critérios e ao reparo limitado.
