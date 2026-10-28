# Compatibilidade da mudança para Opcode

O módulo Go agora é `github.com/raulaguila/opcode` e o executável se chama
`opcode`. Instalações novas usam os nomes Opcode. A aplicação mantém caminhos e
variáveis anteriores como aliases de leitura; iniciar a nova versão não copia
credenciais nem move configurações, históricos ou bancos SQLite.

## Configuração

A busca global prefere os arquivos novos, nesta ordem:

1. `~/.opcode.json`;
2. `$XDG_CONFIG_HOME/opcode/.opcode.json`;
3. `~/.config/opcode/.opcode.json`.

Se nenhum existir, usa o equivalente anterior `.opencode.json`, nas pastas
`opencode`. Um caminho de configuração fornecido explicitamente continua tendo
precedência. No projeto, `.opcode.json` vence `.opencode.json`; somente um deles
é carregado. A segurança e as extensões executáveis continuam vindo apenas da
configuração global, mesmo quando o projeto usa um arquivo antigo.

Salvar uma opção existente atualiza seu arquivo original com escrita privada e
atômica. Não cria outra cópia da chave. Referências `apiKeyEnv`, campos JSON
independentes e números grandes desconhecidos são preservados. Uma configuração
antiga inválida causa um erro; não é motivo para selecionar outro provedor
silenciosamente. Para uma instalação nova sem arquivo, a configuração é salva
em `~/.opcode.json`.

## Histórico e SQLite

Sem `data.directory` explícito:

- uma pasta `.opcode` existente tem precedência;
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
