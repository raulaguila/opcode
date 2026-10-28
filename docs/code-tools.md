# Navegação, validação e artefatos

As ferramentas novas entram no catálogo progressivo do agente. Os modos Perguntar
e Planejar podem consultar o mapa, símbolos, validações, artefatos e recursos MCP;
renomear ou substituir símbolos só está disponível em Executar.

- `ls`: lista caminhos permitidos em ordem estável, com `offset`, `limit`
  (padrão/máximo 60), `glob`, `language`, `extension` e `ignore`. Retorna
  `next_offset`, `more`, `total` e `truncated`. Cada página contém até 6.000 bytes
  de caminhos; a varredura para em 20 mil arquivos. Continue com os mesmos
  filtros e o `next_offset` recebido; se `truncated=true`, reduza o escopo.
  Alterações no diretório entre páginas podem mudar a ordem: nesse caso, reinicie
  pelo offset zero. Arquivos ocultos, sensíveis, dependências, armazenamento e
  links ficam fora. Com `rg` instalado respeita `.gitignore`; sem ele, usa a
  descoberta local com exclusões básicas, sem garantir regras completas do Git.
- `get_project_map`: caminhos, tamanhos e extensões do projeto, com `offset` e
  `limit` (100 por padrão, máximo 500). Ignora credenciais, links, armazenamento,
  dependências e saídas comuns de build. O percurso para em 20 mil entradas e
  informa truncamento; não é um índice semântico nem lê conteúdo de cada arquivo.
- `get_file_outline`: declarações Go pelo parser; demais linguagens pelo servidor
  LSP configurado. Retorna nomes, tipos e linhas, com paginação.
- `read_symbol`: usa o nome exato retornado pelo outline, `file_path`, `offset` e
  `limit` (máximo 500 linhas). Métodos Go usam `Tipo.Método`; símbolos LSP aninhados
  usam `Pai.Filho`. Essas leituras parciais não concedem evidência de leitura
  integral para editar: o agente deve continuar lendo o arquivo pelo fluxo normal.
- `lsp_call_hierarchy`: posição em linha/coluna zero-based; colunas usam UTF-16.
  `direction` aceita `incoming` ou `outgoing`. Limita a dez pontos de preparação e
  cem chamadas, com indicação de truncamento.
- `lsp_rename`: usa `new_name` e o LSP para obter edições em vários arquivos.
  `lsp_replace_symbol` substitui `content` no menor símbolo que contém a posição.
  Ambos mostram diffs na aprovação, revalidam o conteúdo e fazem uma transação
  protegida. Operações de criação/remoção/renomeação de arquivos enviadas pelo LSP
  são rejeitadas. Limites: 128 arquivos, 2 MiB de texto por operação, 20 s de
  requisição. Alterações em caminhos sensíveis ou externos exigem suas aprovações.

`discover_checks` propõe comandos a partir de `go.mod`, `package.json`,
`pyproject.toml`, `Cargo.toml` e alvos conhecidos do `Makefile`. Não executa nada e
não altera o JSON. A descoberta não garante que uma dependência, como pytest,
esteja instalada; o usuário escolhe o que cadastrar em `checks`. O
`get_validation_report` mostra os recibos da tarefa, revisão e motivos de
validação ausente/desatualizada. Sugestões não contam como verificações aprovadas.

Saídas grandes das ferramentas são salvas uma vez em artefatos privados, em `data.directory/artifacts`. O agente recebe um identificador opaco e um trecho;
`get_artifact_info`, `read_artifact` e `search_artifact` permitem continuar a leitura
sem executar o comando novamente. Os offsets são bytes; páginas têm no máximo
32 KiB e buscas retornam no máximo cem trechos de ocorrência. Os snapshots são
dados históricos: não são permissões nem evidência de que um arquivo continua
igual. Credenciais conhecidas são redigidas antes de persistir. Os arquivos são
0600 e o diretório 0700; os IDs só abrem na sessão que gerou o resultado.
O armazenamento privado rejeita links simbólicos nos diretórios ancestrais e
fixa handles de diretório antes de ler ou gravar; trocar um caminho durante o
acesso não redireciona a operação para outro local.

O conteúdo preservado é limitado a 1 MiB por artefato. O armazenamento permite no
máximo 32 artefatos/8 MiB por sessão e 1024 arquivos/64 MiB no total. Quando a cota
se esgota, a ferramenta mantém sua resposta normal; não inventa um identificador
nem repete comandos. Remover artefatos não afeta histórico ou alterações de código.
A captura de comandos preserva o que foi retido pelo buffer limitado do processo;
ainda pode existir truncamento quando o próprio processo ultrapassa esse buffer.

`list_mcp_resources` sem servidor lista os nomes configurados; com `server` faz
uma única página, aceitando `cursor` e retornando `nextCursor`. `read_mcp_resource`
recebe `server` e `uri` e aceita somente texto. Cada acesso ao servidor passa por
aprovação, tem prazo de 15 s e cotas de mil recursos por página, cem conteúdos e
1 MiB de texto por leitura. Conteúdo externo continua sendo dados não confiáveis;
não concede capacidade de executar comandos ou mudar o modo. A leitura usa os
transportes MCP e credenciais já configurados.

A prévia de arquivo da interface usa `file_preview`, limitada a 200 linhas/64 KiB,
com a mesma proteção de caminhos, sem atualizar a evidência de leitura do agente.

A inicialização registra apenas os despachantes nativos `list_mcp_tools` e
`call_mcp_tool`: cadastrar um servidor não abre conexão nem inicia processos.
`list_mcp_tools` sem `server` lista cadastros localmente. Com `server` e `cursor`
opcional, pede autorização e retorna uma página de até 256 ferramentas/1 MiB,
com `next_cursor`. Schemas acima de 64 KiB e descrições acima de 16 KiB são
omitidos e contabilizados. O prazo da consulta é de 10 s; a espera pela aprovação
não consome esse prazo. O catálogo externo é referência não confiável e nunca é
injetado como instrução de sistema nem concede permissões.

Para chamar uma ferramenta externa, use `call_mcp_tool` com `server`, `tool` e
`arguments` (objeto JSON). A execução opaca só é habilitada em Executar e exige
aprovação normal antes de iniciar o cliente. O nome externo não altera essa
classificação, mesmo se imitar uma ferramenta nativa. Chamadas têm prazo de 30 s
e resultado limitado a cem blocos/1 MiB. O teste explícito de conexão da interface
enumera apenas uma página de ferramentas e recursos, após aprovação, e fecha a
conexão em até 8 s.

As ferramentas MCP com nome derivado de servidor/ferramenta deixam de ocupar o
catálogo do modelo diretamente; os nomes e schemas originais continuam
consultáveis via `list_mcp_tools`. O histórico existente é preservado. O
despachante consulta os cadastros atuais em cada chamada, portanto mudanças de
MCP não deixam clientes ou credenciais antigos presos em cache. A exclusão ou
desativação impede a próxima chamada; uma chamada já aprovada conserva seu
snapshot até concluir ou ser cancelada.

Prévia e aprovação de alterações com NUL ou UTF-8 inválido usam somente uma
representação binária com tamanho e SHA256. Isso evita que o gerador de diff de
texto encerre o processo; os snapshots de undo/redo continuam preservando todos
os bytes originais, inclusive NUL e sequências inválidas.

## Índice incremental e formatação

`search_code_index` recebe `query` (até 512 bytes/60 termos), `limit`
(padrão 6, máximo 12) e filtros `path`, `glob`, `language`, `extension`, `kind`.
Exemplo: `{"query":"autenticação usuário","path":"internal","language":"go","kind":"function"}`.
Usa SQLite/FTS5 e ranking BM25, prioriza identificadores exatos e evita resultados
sobrepostos. A normalização reconhece camelCase/snake_case e alguns conceitos
em português, sem inferência nem embeddings. Quando `more=true`, refine a busca;
a consulta examina até 120 candidatos classificados e não promete listar todos.

Retorna caminho, símbolo, tipo, linguagem, linhas, hash, assinatura (até 160 bytes)
e prévia (até 240 bytes), com orçamento de aproximadamente 8 KiB para a resposta.
Leia o código atual com `view`/`read_symbol` antes de concluir ou editar. O índice
é uma ferramenta inicial do agente, respeitando os modos e permissões.

Go usa declarações do parser nativo para funções, métodos, tipos, structs,
interfaces e valores. Janelas textuais complementam o índice para código fora
das declarações, Go incompleto e outras linguagens. A identificação de extensões
não comprova disponibilidade de parser ou LSP: resultados `kind: "text"` são
navegação textual. Os outlines das demais linguagens continuam dependendo do LSP.

O índice privado fica em
`data.directory/code-index/<hash-do-workspace>-v2.sqlite`, com arquivo 0600 e
diretório 0700. O cache anterior sem `-v2` é preservado: versões anteriores
podem voltar a usá-lo; a nova versão reconstrói o seu cache sob demanda.
Configuração, conversas e arquivos do usuário não são migrados por essa mudança.
Remover `code-index` com o aplicativo fechado descarta somente os caches.

Cada busca atualiza a lista de arquivos permitidos e remove entradas apagadas ou
ignoradas. Metadados de tamanho/mtime/identidade/ctime, quando fornecidos pelo SO,
e o hash das credenciais conhecidas permitem reaproveitar arquivos por até 60 s.
Mudança nos metadados, expiração ou mudança de credenciais força releitura. Os
campos `scan_reads`, `reused_files`, `updated_files` descrevem a atualização;
`scan_reads` não inclui a verificação adicional dos resultados retornados.
Cada resultado autoriza novamente a leitura e compara o hash atual. Prévia
indexada não concede evidência integral para uma edição. Em sistemas sem
identidade/ctime, uma alteração que preserve tamanho e mtime pode ficar fora da
busca até a expiração; resultados antigos nunca passam sem verificação de hash.

A descoberta exige `rg`, respeita `.gitignore` inclusive fora de repositórios e
omite caminhos sensíveis, links, dependências, armazenamento e `.opcode-worktrees`.
Cada varredura tem limite de 20 mil arquivos, 256 KiB por arquivo e 20 MiB de texto;
exceder um limite aborta a atualização sem substituir o índice anterior. O banco
tem teto de 32768 páginas SQLite. Credenciais conhecidas são removidas antes da
persistência. Não há serviço externo, downloads nem dependência nova.

`format_project` recebe `preset` e `files` explícitos (1–64 arquivos regulares,
até 2 MiB cada). Os presets `go`, `python` e `javascript`/`typescript`/`json`
executam, respectivamente, `gofmt -w`, `ruff format` e `prettier --write`.
O executável precisa estar instalado e acessível no sandbox. A ferramenta não
usa `npx`, não instala pacotes e não roda automaticamente após editar. Cada
comando passa pela aprovação normal e pelo mesmo gerenciador de processos,
saída ao vivo, sandbox e cancelamento de `bash`. Revise o diff e execute
`run_checks` depois; formatar não significa que testes passaram.

Os outlines continuam estruturais: parser Go e document symbols do servidor
LSP configurado nas demais linguagens. Um LSP ausente é informado, sem inventar
declarações a partir de expressões regulares.

## Comparação multilíngue com o Codaro — 28/10/2026

Comparação com o checkout local em `~/Documents/GitHub/codaro`, incluindo execução
dos 36 casos `test_symbols_are_searchable_and_read_from_current_source`.
Todos passaram em diretórios temporários, sem alterar o checkout do Codaro.

| Capacidade | Codaro local | Opcode |
| --- | --- | --- |
| Cadastro de linguagens/formatos | 81 identificadores, 131 extensões | IDs LSP existentes e extensões adicionais nos filtros; sem promessa de parser para cada extensão |
| Parsing local | Tree-sitter disponível nos 80 identificadores além de `text`; 36 casos com fixture exercitados | Parser Go, sem gramáticas adicionais |
| Python, JS/TS/TSX, Java, C/C++, C#, Rust, SQL, Kotlin etc. | Símbolos estruturais locais, busca por símbolo e leitura da declaração | Busca textual filtrada; outline/leitura por símbolo exigem LSP configurado |
| Filtros de busca | Caminho, glob, linguagem, extensão, tipo de símbolo | Mesmos campos; tipo estrutural no índice atual apenas para Go |
| Atualização | Hash e metadados, com reutilização e serviço de observação | Hash e metadados com TTL de 60 s, descoberta renovada a cada busca |
| Busca | Léxica/exata; combinação semântica opcional | Léxica/exata, sem embeddings |
| LSP | Presets para servidores conhecidos e pool reutilizável | Servidores configuráveis; símbolos, definição, referências, hierarquia e renomeação/edição protegidas |
| Inventário consultável | `get_language_support` paginado distingue parser, fixtures e LSP instalado | Sem inventário equivalente por linguagem; configuração e status das integrações LSP |

Os testes do Opcode exercitam filtros em Go, Python, JavaScript, TypeScript, TSX,
Rust, Java, C++, C#, Kotlin, SQL e texto desconhecido. Isso comprova recuperação
textual e classificação dessas fixtures, não suporte estrutural equivalente ao
Codaro. Por exemplo, `.h` é classificado como C pelo filtro; o Codaro também
inspeciona conteúdo para distinguir cabeçalhos C++. Os filtros não iniciam nem
instalam servidores LSP.

Para ampliar a paridade estrutural, o próximo passo é integrar gramáticas locais
com testes de símbolos/escopos por linguagem, começando por Python, JS/TS/TSX e
Rust. Só declarar extensões ou usar expressões regulares não substitui parsing.
Build/testes continuam exigindo ferramentas instaladas e verificações explícitas,
independentemente do reconhecimento de linguagem ou disponibilidade de parser.
