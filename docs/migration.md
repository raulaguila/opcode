# Compatibilidade da mudança para Opcode

O módulo Go agora é `github.com/raulaguila/opcode` e o executável se chama
`opcode`. Instalações novas usam os nomes Opcode. A aplicação mantém caminhos e
variáveis anteriores como aliases de leitura. A inicialização normal adota o JSON
global antigo no caminho canônico, mantendo o original; históricos e bancos SQLite
permanecem no próprio caminho.

## Configuração

A busca global prefere os arquivos novos, nesta ordem:

1. `~/.opcode/config.json`;
2. `~/.opcode.json`;
3. `$XDG_CONFIG_HOME/opcode/.opcode.json`;
4. `~/.config/opcode/.opcode.json`.

Se nenhum existir, usa o equivalente anterior `.opencode.json`, nas pastas
`opencode`. Um caminho de configuração fornecido explicitamente continua tendo
precedência. No projeto, `.opcode/config.json` vence `.opcode.json`, que vence
`.opencode.json`; somente um deles é carregado. A segurança e as extensões executáveis continuam vindo apenas da
configuração global, mesmo quando o projeto usa um arquivo antigo.

Após a adoção, salvar uma opção global atualiza `~/.opcode/config.json`; o original
antigo fica inativo. Opções do projeto continuam sendo atualizadas no próprio
arquivo, com escrita privada e atômica. Referências `apiKeyEnv`, campos JSON
independentes e números grandes desconhecidos são preservados. Uma configuração
antiga inválida causa um erro; não é motivo para selecionar outro provedor
silenciosamente.

Na inicialização normal, a aplicação cria `~/.opcode/config.json` quando ausente.
Se houver um JSON global antigo, seu conteúdo é preservado no novo arquivo,
incluindo referências de credenciais, chaves literais e campos desconhecidos; o
arquivo antigo permanece intacto para rollback. Caso contrário, usa um template
embutido. A pasta é criada
com modo 0700 e o arquivo com 0600, quando suportados pelo sistema. O template
inclui exemplos de provedores e preferências, sem chaves e sem escolher modelo;
não faz consultas de rede. A publicação é atômica e não substitui um arquivo
criado por outra instância. Não há migração automática de arquivos do projeto.
Diagnósticos offline e execuções com configuração fornecida explicitamente não
criam o arquivo canônico nem adotam configurações antigas. Se um arquivo existente estiver inválido, o erro é apresentado
e seu conteúdo permanece intacto.

Para adotar o novo caminho manualmente, encerre a aplicação e mova o JSON global
para `~/.opcode/config.json`, ou o JSON do projeto para `.opcode/config.json`.
Não sobrescreva um destino existente: compare e combine os campos antes.
O rollback de caminho consiste em mover o JSON atualizado de volta ao caminho
anterior, guardando antes o original inativo quando existir. Não remova a pasta `.opcode`, que também pode conter dados.

## Histórico e SQLite

Sem `data.directory` explícito:

- uma pasta `.opcode` com banco existente tem precedência;
- caso contrário, um banco existente em `.opencode` continua sendo usado, mesmo
  que a pasta nova `.opcode` já contenha apenas `config.json`;
- sem banco existente, uma pasta `.opcode` existente tem precedência;
- caso contrário, uma pasta `.opencode` existente continua sendo usada;
- se nenhuma existe, a aplicação usa `.opcode` no projeto.

Um `data.directory` explícito permanece respeitado; caminhos relativos são
resolvidos a partir do diretório do projeto.

Dentro dessa pasta, `opcode.db` existente tem precedência. Se não existir e houver
`opencode.db`, o banco antigo é aberto **no próprio caminho**. Os arquivos WAL e
SHM continuam com seus nomes originais. Não há cópia ou renomeação de um SQLite
aberto: assim, os registros já confirmados no WAL permanecem visíveis. Conversas,
planos, revisão de arquivos, artefatos e preferências que ficam na pasta de dados
continuam acessíveis. As migrações normais do schema ainda podem acrescentar
estruturas; não são um downgrade do banco.

Se as duas pastas ou os dois bancos já existirem, a versão nova usa o nome novo;
o anterior permanece intacto. Para selecionar o histórico antigo explicitamente,
configure `data.directory` com a pasta correspondente. Não copie apenas o arquivo
`.db` de um banco em uso: WAL e SHM fazem parte de seu estado.

## Variáveis, instruções e temas

`OPCODE_*` tem precedência sobre `OPENCODE_*`, inclusive quando a variável nova
foi definida como vazia. Por exemplo, `OPCODE_DEV_DEBUG` vence
`OPENCODE_DEV_DEBUG`. As opções de testes reais aceitam os dois prefixos, sem
ativar inferência automaticamente. Variáveis dos próprios provedores, como
`OPENAI_API_KEY`, `ANTHROPIC_API_KEY` e `LOCAL_ENDPOINT`, não mudaram de nome.
Referências de credenciais guardadas em JSON não são substituídas pelo valor
resolvido.

As instruções `opcode.md`, `Opcode.md`, `OPCODE.md` e suas variantes `.local.md`
são preferidas. Para cada tipo (base ou local), se nenhum arquivo com nome novo existir, os nomes anteriores
`opencode.md`, `OpenCode.md`, `OPENCODE.md` e variantes locais continuam sendo
considerados. Leitura limitada, proibição de arquivos sensíveis e tratamento de
conteúdo não confiável permanecem. O tema antigo `opencode` é interpretado como
`opcode` em memória, sem alterar automaticamente o arquivo do usuário.

Pastas e configurações anteriores continuam protegidas contra leitura indevida,
pré-carregamento, comandos e publicação acidental. O runner de avaliações também
isola os dois prefixos ao iniciar subprocessos.

## Provas automatizadas

Os testes cobrem precedência de arquivos e variáveis, leitura de configurações
antigas, preservação de referências de credenciais e campos desconhecidos,
segurança global, temas, instruções e um banco antigo mantido aberto com dados
confirmados em WAL enquanto a aplicação o conecta, sem criar `opcode.db`.

```sh
go test -race ./internal/config ./internal/db ./internal/llm/provider \
  ./internal/llm/provider/integration ./internal/llm/prompt
```

## Versão do JSON e manutenção explícita

`schemaVersion: 1` identifica o formato atual; omissão permanece compatível.
Versões futuras e tipos incorretos nos campos conhecidos são recusados sem
regravar o arquivo. `opcode config validate` não exige modelo e não acessa rede;
`config show --redacted` omite credenciais, URLs e campos arbitrários.

`opcode config migrate [--file CAMINHO]` grava no próprio arquivo, preserva
campos desconhecidos e números inteiros grandes e salva antes um backup exato
0600 em `.opcode/config-backups` ao lado do JSON. Se o JSON já está em `.opcode`,
usa sua subpasta `config-backups`. A área privada evita expor backups de chaves
antigas às ferramentas. A migração atual só adiciona a versão; repetir é inócuo.
Para rollback, restaure o backup no caminho original. Não altera configuração
inválida, não segue symlinks e não move configurações legadas.
