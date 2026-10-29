# Ferramentas, execução e permissões

O agente de código e o agente de pesquisa recebem ferramentas através de `SecureTool`. Essa camada instala a política de arquivos e remove valores conhecidos de credenciais das respostas, metadados e erros. O agente de pesquisa recebe apenas leitura, busca, navegação LSP e inspeção Git. Não recebe edição, shell, verificações nem processos.

## Configuração única do usuário

Adicione as opções necessárias à configuração global `~/.opcode/config.json`,
ou ao arquivo anterior que a aplicação já utiliza. Preserve os campos de
provedores e modelos existentes. A primeira inicialização normal cria um
template sem credenciais quando ainda não há configuração. O arquivo
`examples/tools.example.json` contém opções adicionais sem credenciais.

```json
{
  "security": {
    "sandbox": "bubblewrap",
    "allowNetwork": false,
    "allowPrivateNetwork": false,
    "envAllow": [],
    "readOnlyPaths": [],
    "denyPaths": ["*.p12", "*.pfx"]
  },
  "checks": {
    "unit": {"command": "go test ./...", "timeout": 120000},
    "vet": {"command": "go vet ./...", "timeout": 120000}
  }
}
```

`security` é lido antes da configuração do repositório e não pode ser sobrescrito
por `./.opcode/config.json` nem pelos arquivos anteriores. Os comandos `checks`
podem ser definidos por projeto; executar um deles exige a autorização normal
de comando. Os prazos são em milissegundos.

No Linux, o padrão exige Bubblewrap (`bwrap`). Ele fornece namespaces separados, rede desabilitada, diretórios de sistema somente para leitura, workspace gravável e `/tmp` privado. Caminhos convencionais de credenciais dentro do workspace são mascarados, incluindo dependências locais. A inspeção tem limite de 100000 entradas; workspaces maiores exigem escolher um diretório de trabalho mais específico. `.git` também é oculto para comandos genéricos; as ferramentas específicas de Git recebem acesso de leitura aos metadados do repositório. LSP e servidores MCP stdio usam o mesmo executor.

Se o binário ou o suporte a namespaces estiver indisponível, o comando falha. Não existe fallback automático para execução sem isolamento. Em plataformas sem esse mecanismo, ou quando o usuário optar conscientemente por execução sem isolamento, `security.sandbox: "none"` pode ser definido **na configuração global**. Nesse modo, a filtragem de ambiente continua ativa, mas o processo possui o acesso ao sistema de arquivos e à rede do usuário.

- `allowNetwork: true` libera a rede dos processos isolados. O padrão é `false`; downloads de dependências podem precisar dessa opção ou de caches já preparados.
- `envAllow` acrescenta nomes de variáveis ao ambiente mínimo. Variáveis com chaves de API, tokens, senhas, proxies e opções de inicialização não são herdadas por padrão. `HOME`, `BASH_ENV` e `ENV` permanecem neutralizados; configurações de sessão do usuário não são carregadas implicitamente.
- `readOnlyPaths` expõe caminhos absolutos adicionais, somente para leitura, no sandbox. Pode ser necessário para toolchains e caches instalados fora dos diretórios de sistema, ou para os metadados externos de um Git worktree. Evite expor diretórios que contenham credenciais. Caches de Go têm padrões temporários; para usar outro cache, configure seu caminho via `envAllow` e exponha o diretório correspondente.
- `denyPaths` acrescenta padrões de caminho/nome às proteções de credenciais. As proteções padrão incluem `.env*`, configurações do Opcode, arquivos de integração, chaves privadas e diretórios usuais de credenciais. Essa classificação não detecta todo segredo que possa estar escrito em código comum.
- `allowPrivateNetwork` controla destinos privados/loopback de `fetch` e `sourcegraph`. Não libera endereços link-local de metadata. É independente de `allowNetwork`, que trata de subprocessos.

O sandbox protege os limites documentados do processo. Ele não transforma scripts do projeto em código confiável nem fornece limites de CPU/disco. Um comando autorizado pode modificar arquivos comuns do workspace. A opção de autoaprovação da CLI continua sendo uma autorização explícita e ampla.

## Diagnóstico sem iniciar o agente

`opcode doctor --cwd /caminho/do/projeto` verifica configuração, seleção do
modelo, executáveis, LSP configurado e inicialização do sandbox com um comando
inerte limitado a cinco segundos. Funciona mesmo sem agente/modelo cadastrado.
Não inicia LSP/MCP, não mostra valores de ambiente ou chaves e não chama o
modelo. `--json` produz um relatório estruturado; erros retornam código diferente
de zero, e avisos indicam recursos opcionais indisponíveis.

- `--check-provider [--profile ID]`: permite uma consulta de catálogo/metadados,
  limitada a 15 segundos. Não faz inferência.
- `--probe-model --model ID`: permite **uma requisição real de inferência**, até
  45 segundos, com um schema inerte e desafio aleatório. Pode gerar cobrança.
  Exige o ID configurado explicitamente e não executa nenhuma ferramenta. O
  relatório guarda somente sucesso/falha, sem resposta bruta ou credenciais.
  Sucesso confirma esse exemplo de chamada estruturada, não qualidade geral.

A CLI e o seletor de modelos compartilham `diagnostic.ProbeModel`: recebem um
snapshot da conexão e um modelo candidato, inclusive ainda não salvo. A sondagem
usa cópias limitadas a 4096 tokens de contexto e 256 de saída, sem trocar o modelo
ativo. Cancelamento e prazo são preservados; erros exibíveis omitem os corpos
do provedor. Uma geração pode ter reenvios de falhas transitórias pelo transporte,
sempre dentro do prazo total de 45 segundos.

Os detalhes de erros de configuração são omitidos no relatório porque erros de
parse podem conter valores que ainda não foram registrados para redação.

## Arquivos e edição

Leitura dentro do workspace é permitida para arquivos comuns. Caminhos externos e sensíveis exigem autorização explícita, com escopo no caminho resolvido. Escrita por links simbólicos pede autorização para o destino. Operações de arquivo usam `os.Root`, evitando que a resolução de componentes escape da raiz selecionada durante o acesso. Busca em lote omite arquivos sensíveis; use uma leitura explícita autorizada quando precisar deles.

`view` aceita arquivos grandes e retorna uma janela de até 2000 linhas. `offset` é baseado em zero. Linhas extensas são limitadas sem exigir que o scanner mantenha toda a linha na memória. Arquivos usados para edição têm limite de 16 MiB.

O resultado inclui `File: "caminho"` e números de linha, para manter a identidade
da fonte mesmo em templates de modelo que não exibem os argumentos da chamada.
O caminho fica relativo ao projeto quando possível e é escapado como string
JSON. Os números de linha são apresentação, não parte do texto usado em `edit`.
Uma leitura e sua edição dependente não podem ser geradas no mesmo lote: o
modelo precisa receber a fonte antes de propor a alteração. A aplicação retorna
um recibo sem mudança para essa edição prematura; a chamada seguinte conserva
as aprovações normais.

`grep` usa `rg --json`, sem configuração externa do ripgrep, com argumentos delimitados, ordem por caminho e limite de 100 correspondências durante a leitura. `glob` usa a listagem delimitada por NUL do ripgrep. Isso preserva nomes com espaços e `:`. Sem ripgrep, existe um fallback de percurso local, que omite diretórios comuns, arquivos ocultos, credenciais e links; as regras avançadas de `.gitignore` dependem do ripgrep. Ambos os caminhos respeitam cancelamento e limites. Buscas sem ripgrep examinam no máximo 10000 candidatos.

`write`, `edit` e `patch` preparam arquivos temporários no mesmo diretório e preservam os bits de permissão de arquivos existentes. O conteúdo esperado é revalidado após a autorização e antes da instalação. Mudanças de vários arquivos são serializadas dentro do aplicativo, preparadas antes da instalação e restauradas de backups se uma etapa falhar. Destinos conflitantes são rejeitados.

Isso não é uma transação entre filesystem e SQLite, nem uma transação resistente a queda de energia. Escritores externos não participam do mutex do aplicativo; a revalidação reduz conflitos, mas não fornece compare-and-swap do sistema operacional. Falhas de restauração preservam o backup e são informadas. Se o histórico do patch falhar após os arquivos serem alterados, a resposta informa que o filesystem já foi atualizado e marca erro.

## Shell e verificações

`bash` executa um processo independente, com diretório inicial fixo no workspace. `cd`, exports e ativação de ambientes não persistem entre chamadas; agrupe a preparação e o comando na mesma invocação quando necessário.

Os comandos de `bash` e `run_checks` compartilham o gerenciador de processos com
a interface, que mostra stdout/stderr enquanto a execução ainda está ativa.
`bash` aceita `pty: true` para programas que precisam de um terminal e entrada do
usuário; a chamada continua aguardando o término. `process_start` retorna o ID
imediatamente e pode criar o mesmo tipo de PTY em segundo plano.

Os metadados incluem `exit_code`, `timed_out`, `cancelled`, `truncated`, início e fim. Saída diferente de zero e interrupções têm `IsError=true`. A captura mantém início e fim da saída, com memória limitada. No Unix, cancelamento termina o grupo de processos, incluindo descendentes.

`run_checks` recebe `names`, uma lista de até 16 verificações configuradas. Sem nomes, executa todas em ordem. Valida todos os nomes antes de começar e usa as mesmas permissões, ambiente, sandbox e resultados de `bash`. Exemplo de argumentos:

```json
{"names":["unit","vet"]}
```

## Git e LSP

| Ferramenta | Resultado / parâmetros |
| --- | --- |
| `git_status` | Entradas de estado, incluindo renomes; `path` e `limit` opcionais |
| `git_diff` | Diff limitado; `path`, `staged` e `limit` opcionais; caminhos de credenciais excluídos |
| `git_log` | Commit abreviado, timestamp e assunto; `path` e `limit` opcionais |
| `symbols` | Símbolos do documento; requer `file_path` |
| `definition` | Definição na posição; requer `file_path`, `line`, `character` |
| `references` | Referências, incluindo declaração; mesmos parâmetros de posição |

Git usa operações de inspeção, desativa hooks, fsmonitor, diff externo, textconv e prompts de credenciais. Não executa stage, commit, checkout, reset ou clean. Filtros de caminho ficam depois de `--`. Resultados são estruturados e limitados; `git_diff` considera até 100 arquivos por chamada.

`manage_worktrees` é uma ferramenta separada, disponível em Executar. `action:
"list"` lista até 256 worktrees. `create` e `remove` recebem `name` (1–64 letras,
dígitos, `_` ou `-`) e atuam somente em um filho direto de `.opcode-worktrees`.
Criação usa HEAD destacado, sem alterar a branch principal. A aprovação mostra
os argumentos exatos; uma capacidade interna estreita permite ao Git gravar
seus metadados mantendo o sandbox. Shell genérico continua sem esse acesso.

Hooks e fsmonitor ficam desativados; repositórios com filtros de checkout
configurados exigem criação manual. A remoção usa Git sem `--force`: alterações
locais, worktrees bloqueados, submódulos ou remoção do principal/atual impedem a
operação. Caminhos externos, nomes de traversal e links são recusados. Aprovar
não concede permissão para comandos futuros. A execução tem prazo de 30 segundos
após a aprovação; um worktree com metadados comuns fora do sandbox pode não estar
disponível. Para trabalhar no criado, abra `opcode --cwd <caminho-retornado>`;
o agente atual não muda de workspace silenciosamente.
Em outros projetos, inclua `.opcode-worktrees/` nas regras de ignore que desejar;
a ferramenta não modifica silenciosamente o `.gitignore` do usuário.

A navegação LSP usa linhas e posições baseadas em zero; `character` conta unidades UTF-16. O resultado mantém o formato do protocolo, limitado a 100 itens de topo e 2 MiB. O prazo da navegação é 10 segundos. Clientes indisponíveis ou métodos sem suporte geram erro, sem fallback para uma localização inventada. Mensagens LSP recebidas são limitadas a 4 MiB; chamadas respeitam cancelamento, e as gravações do protocolo são serializadas. Diagnósticos não substituem o handler de notificações a cada consulta.

## Processos longos

- `process_start`: recebe `command`, `timeout` opcional, `pty` opcional e tamanho inicial `cols`/`rows`. A criação exige autorização de execução.
- `process_status`: recebe `id` e retorna estado, exit code e stdout/stderr limitados.
- `process_stop`: recebe `id`, cancela e aguarda o término.
- `process_write`: recebe `id` e `data` (até 16384 bytes); inclui uma quebra de linha para submeter uma linha. A entrada gerada pelo agente exige uma aprovação separada, vinculada ao processo.

Cada processo pertence à sessão que o iniciou. Outras sessões não podem consultá-lo nem interrompê-lo. Ele pode continuar após o turno atual, mas tem prazo máximo de 10 minutos (padrão 1 minuto). Há até 4 processos simultâneos e 32 registros por manager; registros concluídos são descartados quando necessário. Cancelar a sessão e encerrar o aplicativo cancela seus processos e recolhe os filhos. O estado é local ao aplicativo, sem retomada após reinício.

### Terminal e entrada do usuário

`Ctrl+P` alterna entre chat e terminal. Sem processos existentes, abre um pedido
de aprovação para iniciar um shell com PTY. O shell mantém `cd`, exports e
programas interativos na mesma sessão. O prazo dos shells criados pela interface
é dez minutos. `Ctrl+N` cria outro, `Alt+Left/Right` escolhe o processo,
`Ctrl+X` encerra o selecionado e `Ctrl+C` é enviado ao processo em foco.
`Ctrl+P` volta ao chat sem encerrar o processo. Comandos sem PTY aparecem como
saída de leitura, navegável com PgUp/PgDn.

O PTY é próprio do processo e usa o mesmo workspace, máscaras de credenciais,
ambiente filtrado e política de rede dos comandos normais. No Linux, sua sessão
de terminal é criada antes do Bubblewrap. Falhas de isolamento continuam sendo
erros; Windows retorna explicitamente que PTY ainda não é suportado.

As teclas digitadas pelo usuário são entrada direta no processo já aprovado.
O agente usa `process_write`, com autorização independente de `process_start`.
Entrada de outra sessão, filas cheias e processos encerrados são rejeitados.
A fila de entrada tem até 32 blocos de 16 KiB; a tela fica limitada a 240x80.
Saída é capturada com os limites existentes e atualizada na interface a cada
100 ms. Uma emulação VT interpreta o conteúdo do PTY, sem repassar OSC/CSI ao
terminal externo. Valores conhecidos de credenciais são ocultados inclusive
quando chegam em trechos separados ou ocupam várias linhas da tela.

### Instruções durante o trabalho

Enter continua enviando mensagens enquanto o agente gera ou executa ferramentas.
`Submit` inicia um turno ou acrescenta uma instrução à fila da sessão, com limite
de oito mensagens e 1 MiB. Somente o worker grava essas mensagens no histórico,
depois dos resultados de ferramentas pendentes, preservando a ordem exigida
pelas APIs. Chamadas antigas ainda não iniciadas recebem um resultado de
cancelamento, e a próxima chamada ao modelo inclui as novas instruções.

Uma operação já em execução termina antes de aplicar a instrução. Escape
cancela o turno, seus processos e a fila. Instruções ainda na fila ficam somente
em memória; são persistidas ao serem aplicadas. Um erro no envio preserva o
rascunho do editor. Resumir a sessão continua sendo uma operação exclusiva.

## Rede e extensões

`fetch` e `sourcegraph` solicitam permissão por origem/destino (`network:scheme://host:port`), sem incluir query strings no escopo. Um redirecionamento para outra origem exige nova autorização; há limite de cinco redirecionamentos e downgrade de HTTPS é rejeitado.

Os IPs resolvidos são validados antes de conectar e usados diretamente na conexão, preservando o hostname para TLS. Loopback e redes privadas são negados por padrão; metadata/link-local, multicast e endereços não especificados continuam negados mesmo quando a opção de rede privada está ativa. Proxies do ambiente não são usados nessa camada. Respostas têm limite explícito de 5 MiB, com erro em vez de truncamento silencioso.

MCP mantém sua configuração de serviços. Comandos stdio herdam o executor filtrado e isolado; apenas variáveis explícitas de `mcpServers.<nome>.env` são acrescentadas a esse ambiente. Conexões remotas MCP usam os transportes da biblioteca e os destinos/headers configurados pelo usuário; não passam pela política de navegação web de `fetch`. Configure somente serviços confiáveis. Resultados de ferramentas MCP continuam sujeitos à autorização de execução e à remoção de credenciais conhecidas na resposta.

## Validação

```sh
go test -race ./...
go vet ./...
go test ./internal/llm/tools -run TestBubblewrapIsolationOnSupportedKernel -v
```

O último teste verifica o sandbox real com dados sintéticos. Ele é marcado como `SKIP` quando Bubblewrap ou namespaces não estão disponíveis; há também um teste separado que prova que uma falha do sandbox não executa o comando fora dele. Os testes de APIs reais continuam optativos e separados dessa suíte.


Para `read_lines`, hashes/snapshots, linguagens locais, índice administrável,
embeddings e inicialização lazy de LSP, veja [Índice e recuperação](retrieval-runtime.md).
`patch preview:true` produz uma proposta com hashes/preview_id; aplicar conserva
aprovação, proteção contra conflitos e recibo da transação.

## Busca web opcional

`web_search` começa desativada. Em Settings → Preferências → Avançado, ative
**Busca web / Web search**, ou configure `"runtime": {"webSearch": true}` no JSON.
O toggle não concede aprovação: cada destino HTTP continua sujeito ao controle
de rede, inclusive redirects. A ferramenta usa o DuckDuckGo HTML, sem chave, e
não consulta as páginas encontradas automaticamente. O serviço pode limitar,
bloquear bots ou mudar seu formato; isso gera um erro explícito, sem resultados
inventados nem fallback silencioso.

O modelo pode carregar o schema com `request_tools` e chamar, por exemplo:

```json
{"query": "Go official documentation", "limit": 5}
```

São no máximo 10 resultados, query de 512 bytes, resposta HTTP de 1 MiB e prazo
de 20 segundos; títulos/snippets e URLs têm limites próprios. Envie apenas
consultas públicas. As proteções de IP privado/metadata, credenciais e redirects
são as de `fetch`; ativar a busca não altera permissões, sandbox ou TLS.

Snippets são dados externos não confiáveis. Links não equivalem a páginas lidas
ou fatos verificados e não satisfazem os recibos de leitura do repositório. A
validação existente de citações locais continua obrigatória. Use `fetch` para
consultar uma fonte necessária, com a aprovação normal. Não há pesquisa web
automática na preparação das perguntas sobre o projeto.
