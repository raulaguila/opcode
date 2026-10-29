# Índice, linguagens e recuperação

O diretório não é despejado no system prompt. O agente descobre páginas e lê as
fontes necessárias. Ter um parser não significa que compilação/testes ou LSP
estejam instalados.

## Controles sem modelo

```sh
opcode index status
opcode index update
opcode index pause
opcode index resume
opcode index rebuild
opcode index explain src/service.py
opcode index clear
```

Na TUI: `/index`, `/index update`, `/index pause` etc. A operação tem prazo e Esc
cancela o painel. `clear` descarta dados derivados, preservando fontes, histórico,
configuração e achados. `pause` persiste só no estado privado do índice. Busca
pausada verifica hashes, mas pode omitir arquivos ainda não indexados.

`.opcodeignore` aceita padrões Git no diretório em que existe, inclusive `!` para
reverter padrões próprios. É aditivo ao Git: não reabilita arquivos ignorados
pelo Git nem caminhos bloqueados pela política. Exemplo:

```gitignore
fixtures/generated/
*.snapshot
!small.snapshot
```

Listagem, glob, grep, mapa e índice compartilham exclusões. Com rg, há também as
regras nativas de Git; o fallback local aplica arquivos .gitignore/.opcodeignore
dos ancestrais dentro do workspace. Exclusões fixas de arquivos sensíveis,
symlinks, dependências e armazenamento sempre vencem. `index explain` identifica
a regra da aplicação; regras globais próprias do rg podem excluir outros itens.

## Limites e trabalho em segundo plano

O objeto `index` no JSON aceita:

| Campo | Padrão / teto |
| --- | --- |
| background | true; false desativa updates automáticos |
| maxFiles | 20.000 |
| maxFileBytes | 262.144 |
| maxTextBytes | 20.971.520 |
| memoryMiB | 32; cache SQLite e orçamento do parser, não RSS global |
| updateSeconds | 5; até 30 por atualização automática |

Zero usa padrão; limites de tamanho/arquivos só podem baixar os tetos.
O watcher faz debounce e reconciliação periódica, prioriza arquivos lidos ou
recentes e fecha junto à aplicação. Atualizações de busca são síncronas e
reutilizam metadados por até 60 s; cada hit verifica fonte atual. Falhas não
substituem o snapshot anterior. A versão da política força reconstrução dos
chunks antigos quando os parsers mudam; o banco anterior de outra versão é
preservado. Nenhuma migração move credenciais/histórico.

## Busca e fontes

`search_code_index` aceita os filtros existentes e `intent`:
implementation, tests ou documentation. `strategy:lexical` é o padrão.
`strategy:hybrid` usa o serviço opcional de embeddings. Ambas retornam páginas
curtas de navegação; `view`, `read_lines` ou `read_symbol` estabelecem leitura.
`file_hash` é SHA256 da fonte inteira. `read_lines` deve reenviá-lo em
`expected_hash` e usar os próximos offset/column, também em linhas longas.

Go usa parser padrão. Python, JS, TS, TSX, Rust, Java, C, C++ e C# têm gramáticas
locais selecionadas, nomes de escopos e fixtures de declarações. Demais linguagens
continuam com texto/filtros e LSP quando configurado. Referências por binding,
renomeação e hierarquia exigem LSP; resultados textuais são candidatos.

## LSP sob demanda

Presets reconhecem gopls, pyright-langserver/pylsp,
typescript-language-server, rust-analyzer, clangd, jdtls e csharp-ls. Servidores
precisam estar instalados e configurados; comandos genéricos desconhecidos não
iniciam por inspeção. Para outro servidor, configure `fileTypes` (IDs ou extensões)
e `rootMarkers` (nomes relativos, sem glob). Presets têm marcadores conhecidos;
sem marcador compatível o servidor não inicia. A execução conserva sandbox e
configuração autorizada de integração.

## Embeddings opcionais

Somente o JSON global pode habilitá-los. Exemplo **apenas se esse modelo já
estiver instalado**:

```json
{"index":{"embeddings":{"endpoint":"http://127.0.0.1:11434/api/embed","model":"nomic-embed-text"}}}
```

Não há pull/download. Só IP loopback HTTP, sem proxy/redirect/autenticação.
Cada pedido é limitado, usa truncate=false e texto redigido. O cache privado
considera endpoint/modelo/dia/conteúdo. O híbrido usa um pool limitado e aquece
até 16 vetores novos por busca; repositórios grandes não são inteiramente
vetorizados na primeira chamada. Falhas retornam busca lexical com motivo
sanitizado. Compare recall/MRR nas suas fixtures antes de adotar: a implementação
não presume ganho com qualquer modelo.

A API segue [Ollama /api/embed](https://github.com/ollama/ollama/blob/main/docs/api.md).
