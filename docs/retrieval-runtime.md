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

A [matriz gerada de linguagens](language-support.md) é a fonte da lista atual,
com distinção entre sintaxe, fixtures e suporte semântico por LSP.

Go usa parser padrão; os demais perfis usam gramáticas locais selecionadas,
listadas na matriz gerada. Cada perfil tem fixture de declaração e chunk de
índice; isso não certifica toda a sintaxe de uma linguagem.
Os parsers já pertencem à dependência Go existente, sem instalação de runtimes.
As fixtures adicionais cobrem declarações, intervalos exatos de linhas e chunks
de índice. Formas genéricas como chamadas Elixir, listas Clojure e funções anônimas R
não são tratadas como declarações. Isso não equivale a cobertura de todos os
dialetos ou resolução semântica; por exemplo, funções R são reconhecidas em
atribuições diretas a identificadores, e os bindings de OCaml/F# são navegação
sintática de valores. Nenhuma gramática é baixada durante a execução.

Vue/Svelte extraem declarações dos scripts JS/TS incorporados, preservando linhas
do arquivo original. Templates ficam pesquisáveis como texto; não se presume
resolução de bindings entre template e script. Dart inclui o corpo após a
assinatura. HCL expõe blocos/atributos, YAML/JSON chaves, TOML seções/chaves, HTML
elementos com `id` e CSS seletores. Esses nomes são navegação sintática, não
referências semânticas. Sintaxe inválida ou não suportada continua indexada em
janelas de texto. Arquivos acima de 256 KiB, cancelamento e limites do parser
continuam aplicados; arquivos com mais de oito scripts usam o fallback textual.

Demais linguagens continuam com texto/filtros e LSP quando configurado.
Referências por binding, renomeação e hierarquia exigem LSP; resultados textuais
são candidatos. A política de índice `parser-v5` reconstrói chunks anteriores
mesmo quando os arquivos não mudaram.

## LSP sob demanda

Presets reconhecem gopls, pyright-langserver/pylsp,
typescript-language-server, rust-analyzer, clangd, jdtls e csharp-ls. Também incluem
ruby-lsp/solargraph, intelephense/phpactor, kotlin-language-server, sourcekit-lsp,
sqls, bash-language-server, lua-language-server, dart, vue-language-server,
svelteserver, terraform-ls, yaml-language-server, taplo e servidores VS Code
de JSON/HTML/CSS. Servidores
precisam estar instalados e configurados; comandos genéricos desconhecidos não
iniciam por inspeção. Para outro servidor, configure `fileTypes` (IDs ou extensões)
e `rootMarkers` (nomes relativos, sem glob). Presets têm marcadores conhecidos;
sem marcador compatível o servidor não inicia. A execução conserva sandbox e
configuração autorizada de integração.

`get_language_support` separa parser testado, servidor configurado, executável
encontrado e checks configurados. Aceita `language`, `offset`, `limit` (padrão 10,
teto 20) e `detect_project` (padrão false). A descoberta opcional conta até 2000
arquivos com as exclusões compartilhadas, sem ler conteúdo de fonte; contagens
parciais são sinalizadas. As sugestões incluem comando, argumentos e marcadores
para configuração explícita, sem iniciar/instalar servidores nem executar checks.
Checks listados são do projeto, sem atribuição automática a uma linguagem.

Na CLI, `opcode doctor --languages --json` apresenta somente linguagens detectadas
no projeto, com esses estados e sugestões. O diagnóstico não faz inferência nem
download; `doctor` sem a flag mantém a inspeção breve existente. Um executável no
PATH ainda precisa de configuração, marcadores de raiz compatíveis e autorização
normal de integração para ser iniciado durante o trabalho.

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

## Diagnósticos de preparação e latência

`/runtime` inclui `usage.performance`: chamadas de preparação, chamadas de `view`
sem reutilização (inclusive falhas), fontes reutilizadas após revalidação, bytes projetados e títulos locais
sem inferência. A reutilização mantém os controles de permissão e a verificação
de integridade; não autoriza aproveitar uma citação obsoleta. A revalidação ainda
pode ler/calcular o hash do arquivo; esses contadores não medem operações de disco.

O tempo até o primeiro conteúdo usa uma janela das últimas 64 tentativas por
sessão, com última amostra e percentis p50/p95 por posto mais próximo. Inclui
espera de transporte/provedor e não mede apenas inferência. Reparos e chamadas
continuam nas métricas cumulativas existentes. Nenhum texto das fontes é
armazenado nos diagnósticos.

Quando conversa e título usam o mesmo modelo local, o título é obtido da primeira
mensagem (até 72 caracteres), sem disputar outra inferência com a resposta. Um
modelo de título selecionado separadamente mantém a geração pelo provedor.

## Velocidade de geração

A estimativa ao vivo exige dois fragmentos não vazios e pelo menos 250 ms entre
as amostras. O primeiro bloco é excluído: ele pode conter saída acumulada pelo
servidor. O intervalo usa os instantes dos fragmentos, não as atualizações da
tela. Um único bloco mantém “Gerando” sem inventar tok/s.

Modelos OpenAI reconhecidos pelo tokenizador já instalado usam contagem local
compatível, ainda marcada como estimativa. A contagem guarda no máximo 16 KiB
transitórios, não persistidos, e roda no máximo quatro vezes por segundo. Ao
atingir o limite ou falhar, usa bytes/4. Nomes parecidos em provedores locais não
ativam esse tokenizador. A origem aparece em `usage.stream_count_source`.

A medição final informada pelo provedor continua tendo prioridade. Servidores
que entregam tudo ao final não oferecem amostras para uma velocidade ao vivo.
