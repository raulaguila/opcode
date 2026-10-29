# Navegação, validação e artefatos

As ferramentas novas entram no catálogo progressivo do agente. Os modos Perguntar
e Planejar podem consultar o mapa, símbolos, validações, artefatos e recursos MCP;
renomear ou substituir símbolos só está disponível em Executar.

- `ls`: páginas ordenadas, até 60 caminhos/6.000 bytes, com filtros e `snapshot`.
  Reenvie o snapshot com `next_offset`; mudanças exigem reiniciar em zero.
- `get_project_map`: até 60 entradas (padrão 30), orçamento de 7.500 bytes, mesmas
  exclusões da listagem. Classifica manifests, entradas, testes/documentação e
  fontes; lê até 12 arquivos pequenos para declarações, pacotes e dependências
  declaradas em Go, package.json, pyproject.toml e Cargo.toml. Não resolve um
  grafo transitivo nem transforma imports em referências semânticas.
- `get_file_outline`: Go padrão; parsers locais para Python, JS, TS, TSX, Rust,
  Java, C, C++ e C#. Outras linguagens usam LSP configurado. Retorna hash, nomes
  qualificados, tipos e linhas; `expected_hash` detecta fonte alterada.
- `read_symbol`: nome exato do outline, offset/limit e `expected_hash` opcional.
- `read_lines`: páginas UTF-8 de até 8 KiB/200 linhas (padrão 4 KiB/100), offset
  e coluna em bytes baseados em zero. Reenvie file_hash como expected_hash e
  continue com next_offset/next_column. Linhas longas continuam sem perder dados.
  O hash cobre o arquivo inteiro, limitado a 16 MiB.
- `get_language_support`: distingue reconhecimento, parser local/fixture e LSP
  configurado/executável disponível. Não instala nem inicia servidores.
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
em português, sem inferência no modo lexical. O modo hybrid é opt-in. Quando `more=true`, refine a busca;
a consulta examina até 120 candidatos classificados e não promete listar todos.

Retorna caminho, símbolo, tipo, linguagem, linhas, hash, assinatura (até 160 bytes)
e prévia (até 240 bytes), com orçamento de aproximadamente 8 KiB para a resposta.
Leia o código atual com `view`/`read_symbol` antes de concluir ou editar. O índice
é uma ferramenta inicial do agente, respeitando os modos e permissões.

Go usa seu parser nativo. Nove gramáticas locais puras Go acrescentam as
linguagens descritas acima. Chunks acompanham declarações/escopos, nomes
qualificados e assinatura do pai quando disponível. Janelas textuais continuam
complementando declarações e atendem fontes incompletas/não suportadas. A identificação de extensões
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
persistência. Não há downloads de modelos. Parsers selecionados são embarcados; embeddings exigem configuração local explícita.

`format_project` recebe `preset` e `files` explícitos (1–64 arquivos regulares,
até 2 MiB cada). Os presets `go`, `python` e `javascript`/`typescript`/`json`
executam, respectivamente, `gofmt -w`, `ruff format` e `prettier --write`.
O executável precisa estar instalado e acessível no sandbox. A ferramenta não
usa `npx`, não instala pacotes e não roda automaticamente após editar. Cada
comando passa pela aprovação normal e pelo mesmo gerenciador de processos,
saída ao vivo, sandbox e cancelamento de `bash`. Revise o diff e execute
`run_checks` depois; formatar não significa que testes passaram.

Outlines locais são sintáticos; referências resolvidas continuam sendo responsabilidade do LSP. Texto/grep nunca é apresentado como resolução de símbolos.

## Comparação multilíngue com o Codaro — 28/10/2026

Comparação com o checkout local em `~/Documents/GitHub/codaro`, incluindo execução
dos 36 casos `test_symbols_are_searchable_and_read_from_current_source`.
Todos passaram em diretórios temporários, sem alterar o checkout do Codaro.

| Capacidade | Codaro local | Opcode |
| --- | --- | --- |
| Cadastro de linguagens/formatos | 81 identificadores, 131 extensões | IDs LSP existentes e extensões adicionais nos filtros; sem promessa de parser para cada extensão |
| Parsing local | Até 80 parsers disponíveis, 36 fixtures exercitadas | Go + nove gramáticas selecionadas; dez fixtures de declarações/escopos exercitadas |
| Outras linguagens | Cadastro amplo e parsing local conforme gramática | Filtros/texto e LSP configurado; sem prometer parser para toda extensão |
| Busca | Léxica/exata e semântica opcional | Léxica/exata, intent implementation/tests/documentation e hybrid opt-in local |
| Índice | Metadados, hashes, watcher | Metadados/hashes, watcher limitado, reconciliação periódica, pausa/admin/explicação |
| Inventário | Parser/fixture/LSP | `get_language_support` separa parser, fixture e servidor disponível |

O snapshot anterior do Opcode (somente parser Go) foi superado por esta etapa.
O Codaro continua com cobertura estrutural mais ampla; não se afirma superioridade
geral por quantidade de extensões. C/C++ `.h` continua classificado como C no
filtro do Opcode. Os presets LSP não instalam executáveis. Veja os limites e os
controles em [Índice e recuperação](retrieval-runtime.md).

Build/testes continuam exigindo ferramentas instaladas e verificações explícitas,
independentemente do reconhecimento de linguagem ou disponibilidade de parser.


`patch` aceita `preview:true` para retornar diff limitado, before/after hashes e
preview_id sem escrever ou aprovar. Reenviar preview_id ao aplicar rejeita mudança
na fonte ou no patch. A aplicação continua pedindo as aprovações normais e usa a
transação existente; o recibo inclui hashes e receipt_id. O identificador não é
um token de autorização nem garantia contra queda da máquina durante multiwrite.
