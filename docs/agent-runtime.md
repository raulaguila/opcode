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
