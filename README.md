# Opcode

Agente de desenvolvimento em Go para trabalhar em repositórios pelo terminal.
A interface permite perguntar, planejar e executar com ferramentas protegidas,
acompanhar comandos ao vivo e enviar novas instruções durante o trabalho.
O suporte a Ollama inclui o protocolo nativo e APIs compatíveis com OpenAI.
Servidores compatíveis personalizados usam uma conexão própria, com TLS inseguro
opcional por conexão. Veja [a configuração](docs/model-selection.md#servidor-openai-compatible-e-tls).

O código deste fork é desenvolvido em
[github.com/raulaguila/opcode](https://github.com/raulaguila/opcode), na branch
`develop`. A qualidade das respostas depende do modelo, do contexto e das
verificações executadas; os recursos disponíveis não garantem correção geral.

## Instalar e executar

Use Go 1.26 ou posterior para compilar o checkout:

```sh
git clone --branch develop https://github.com/raulaguila/opcode.git
cd opcode
go build -o opcode .
./opcode
```

Para instalar a partir da branch de desenvolvimento publicada:

```sh
go install github.com/raulaguila/opcode@develop
opcode
```

O binário é instalado em `GOBIN`, quando definido, ou em `GOPATH/bin`; inclua esse
diretório no `PATH`. O helper local `sh ./install` usa o mesmo `go install`, sem
editar arquivos do shell. Builds de release e checksums são publicados pelo workflow de versões após
a aprovação dos testes e do vet. Veja [releases](https://github.com/raulaguila/opcode/releases). Não há distribuição Homebrew
ou AUR configurada para este fork.

## Primeiro acesso e modelos

Na inicialização normal, se `~/.opcode/config.json` não existir, o Opcode cria esse
arquivo. Se houver uma configuração global antiga, preserva seu JSON no novo
caminho e mantém o original para rollback. Caso contrário, usa um template com Ollama,
LM Studio, OpenAI e Anthropic, preferências e opções básicas. Nenhum modelo é
selecionado pelo template; exemplos remotos ficam desativados e chaves são
referenciadas por variáveis de ambiente. Criar o arquivo não consulta provedores,
não executa inferência e não baixa modelos. Arquivos existentes não são
sobrescritos. `doctor` continua sendo uma leitura sem criar o template.

Toda inicialização normal também garante um exemplo separado em
`~/opcode/config.example.json`, com provedores e referências a variáveis de
ambiente, sem copiar suas credenciais. Se você apagar esse exemplo, ele será
recriado na próxima abertura; arquivos existentes são preservados.

Para recomeçar a configuração global e abrir a aplicação:

```sh
opcode --reset
```

O reset salva antes uma cópia exata em `~/.opcode/config-backups/`, informa o
caminho no terminal e recria `~/.opcode/config.json` com os padrões. Também
recupera um JSON inválido. Encerre outras instâncias antes de usar. Configurações
do projeto e variáveis de ambiente continuam valendo; conversas e dados não são
apagados. Sem modelo configurado nessas outras fontes, a configuração guiada
será aberta.

Sem modelo configurado, a abertura interativa inicia a configuração guiada. `N` cadastra
uma conexão com um preset; `Ctrl+T` testa o catálogo e `Ctrl+S` salva. Escolha então o modelo
pelo seletor. A autenticação aceita chave direta em campo mascarado, variável de
ambiente ou nenhuma chave; [veja como configurar](docs/model-selection.md#informar-a-api-key-diretamente).
A última etapa oferece começar a conversar; detalhes da configuração
e testes opcionais com confirmação ficam em uma ação secundária. `/setup` reabre
o fluxo. A barra inferior reúne modo, comandos, contexto, velocidade da última resposta e modelo; processo e lateral
começam recolhidos. `~/.opcode/config.json` é reutilizado entre projetos;
`./.opcode/config.json` pode acrescentar ou sobrescrever configurações do projeto.
Os formatos anteriores `.opcode.json` e `.opencode.json` continuam reconhecidos
e os arquivos do projeto permanecem no próprio caminho. Veja [precedência e compatibilidade](docs/migration.md).

Para configurar Ollama uma vez, por exemplo:

```json
{
  "providerProfiles": {
    "local": {
      "name": "Ollama local",
      "backend": "local",
      "baseURL": "http://127.0.0.1:11434/v1",
      "api": "ollama",
      "defaultMaxTokens": 2048
    }
  }
}
```

O contexto é consultado no provedor, sem guardar seu tamanho no JSON.
Um `contextWindow` positivo é uma opção para definir um limite manual.
O seletor identifica contexto alocado, máximo anunciado ou estimativa alternativa,
e mostra suporte a ferramentas informado pelo provedor (ou desconhecido).
A configuração básica inclui o estado ativo da conexão; salvar um perfil
desativado não o ativa silenciosamente.
Consultar o catálogo não baixa
pesos nem gera respostas. Clique no modelo do rodapé, pressione `Ctrl+O` ou use
`/models` para buscar, trocar de perfil e escolher. `Ctrl+F` marca favoritos,
`Ctrl+A` alterna a função principal/auxiliar e `Ctrl+R` atualiza o catálogo.
`✓` identifica o modelo ativo; `›` marca o destaque. `PgUp`/`PgDn` navegam na
lista e `Alt+←`/`Alt+→` trocam o provedor; as setas simples editam a busca.
A escolha é salva; o contexto descoberto fica em memória, sem serializar chaves
resolvidas do ambiente.

Provedores remotos podem usar `apiKeyEnv` com o nome de uma variável. As variáveis
usuais (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, etc.) e o formato
`providers` continuam disponíveis. Uma assinatura de aplicativo de chat não
fornece credenciais de API a este programa. No modo `--prompt`, a configuração
válida deve existir antes da execução.

Veja [configuração de provedores/modelos](docs/model-selection.md),
[o schema JSON](opcode-schema.json) e
[integrações MCP/LSP](docs/integrations-settings.md).

Veja o [guia de uso diário](docs/daily-workflow.md) para configurar, definir o
resultado esperado de tarefas e trabalhar com Git/worktrees.

## Recursos

| Frente | Disponível neste fork |
| --- | --- |
| Tarefas | Contratos explícitos em `/contract` ou `--task-kind`, com alvos, critérios e checks da revisão atual. |
| Configuração | Perfis com ID, nome e backend separados; múltiplos endpoints, formulários, teste de conexão, catálogo dinâmico, favoritos, recentes e modelos auxiliares independentes. |
| Chat | Processo recolhível por interação, resposta final separada, ações de texto/código/arquivos, aviso de novas mensagens, histórico e rascunhos por conversa. |
| Execução | Terminal PTY com entrada do usuário, saída ao vivo e fila de instruções editável durante o trabalho. |
| Contexto | Estimativa preventiva do payload e schemas, reserva de resposta e compactação local preservando a conversa salva. |
| Revisão | Diffs por interação, recibos de checks e undo/redo de edições de arquivos com detecção de conflitos e aprovação. |
| Conversas | Busca, renomeação, arquivamento, exportação, retomada opcional e ações de recuperação de erro. |
| Ferramentas | Listagem paginada, índice local com filtros e prévias curtas, mapa, outline/símbolos, artefatos paginados, checks, navegação/edição LSP e MCP sob demanda. |
| Linguagens | 26 perfis de parsers locais; diagnóstico `doctor --languages --json` distingue parser, LSP e checks sem instalar ferramentas. |
| Referências | Citações verificadas contra caminho, linhas realmente lidas e hash atual; reparo limitado preserva o pedido original. |
| Decisões | Perguntas individuais ou agrupadas com seleção única/múltipla, texto livre e revisão antes do envio. |
| Extensões | Skills invocadas explicitamente pelo usuário e hooks globais opt-in, sujeitos às permissões e ao sandbox. |
| Avaliações | Fixtures e avaliações reais opt-in, drivers Codaro/Crush, comparação de condições e medição de ranking lexical/híbrido sem inferência. |
| Confiabilidade | Conclusão com evidência de alteração ou justificativa explícita, estado parcial/bloqueado e recuperação de contexto/OOM com calibração local temporária. |
| Produtividade | Índice incremental FTS5/BM25, exploração somente leitura com orçamento, formatação e worktrees protegidas. |
| Preferências | Lateral adaptável, input crescente, busca de configurações com origem efetiva, atalhos, idioma e notificações opcionais. |
| Reconexão | Servidor local autenticado e cliente `attach`, com snapshots de sessão, aprovações, perguntas e PTY sem repetir comandos. |

## Configuração pela CLI

```sh
opcode config path
opcode config validate
opcode config show --redacted
opcode config migrate  # backup privado antes de adicionar schemaVersion
```

Os JSONs antigos continuam aceitos. A migração não move arquivos nem resolve
chaves do ambiente para gravá-las no disco. Veja [compatibilidade](docs/migration.md).
`/local-status` e `opcode doctor --ollama-status` consultam contexto e CPU/GPU
observados no Ollama, sem carregar ou baixar modelos.

## Diagnóstico, configuração e reconexão

`opcode doctor` verifica a instalação sem exigir modelo nem acessar o provedor.
`--check-provider` consulta somente o catálogo; `--probe-model` executa um teste
real e inerte de chamada de ferramenta, que pode consumir tokens. Um teste de
catálogo aprovado não certifica a capacidade de executar tarefas.

As preferências pesquisáveis mostram o valor efetivo e sua origem. Por exemplo,
no JSON global, `"tui": {"language":"pt","sidebar":"auto","notifications":"off"}`
seleciona controles em português e lateral adaptável. Atalhos podem ser alterados
em `tui.keybindings`; valores controlados por ambiente precisam ser alterados na
variável correspondente. Alguns textos legados e mensagens técnicas mantêm o
idioma original.

Para manter a aplicação e os PTYs abertos enquanto um cliente desconecta, use
`opcode serve` e `opcode attach`. A autenticação vem de `OPCODE_SERVER_TOKEN`;
o servidor aceita somente loopback e nunca autoaprova ferramentas. Leia o
[guia do servidor e OAuth](docs/server-and-auth.md) antes de usar a API.
OAuth de integrações MCP é iniciado explicitamente por `opcode mcp login NOME`
ou pelo cadastro da integração. Não concede permissão de executar ferramentas.

Arquivos e checksums locais são gerados por `make release-local`, conforme
[builds reproduzíveis](docs/local-release.md). Isso não publica uma release.

Comece pelo [guia da interface](docs/workspace-ui.md). Os principais controles:

| Controle | Ação |
| --- | --- |
| `/` e `Ctrl+K` | Sugestões de comandos e paleta |
| `/mode ask`, `/mode plan`, `/mode execute` | Perguntar, Planejar e Executar |
| `Ctrl+B`, `/process` | Expandir/recolher o processo da última interação |
| `Alt+End`, `/latest` | Ir às mensagens novas |
| `Alt+PgUp` / `Alt+PgDn` | Rolar meia página de conversa |
| `Ctrl+U`, `Ctrl+End` | Apagar até o início / ir ao final do rascunho |
| `Alt+Enter`, `Shift+Enter` | Nova linha; o rodapé indica a combinação suportada |
| `Ctrl+Y`, `/actions` | Copiar resposta/código ou abrir citação |
| `Ctrl+P` | Mostrar/ocultar terminal |
| `/queue` | Editar/remover instruções pendentes |
| `/context` | Orçamento, índice, alocação e diagnóstico; `C` solicita compactação |
| `/task` | Plano, fila, alterações, checks e permissões em um painel |
| `/changes`, `/checks` | Ver alterações e validações; `U`/`R` prepara undo/redo |
| `/sessions` | Buscar, renomear, arquivar e exportar conversas |
| `/settings`, `/models`, `/integrations` | Configurar a aplicação |
| `/skills`, `/skill NOME argumentos` | Consultar/invocar skills cadastradas |
| `/error`, `/retry` | Ver falha e retomar o último pedido |

## Permissões, contexto e limites

Perguntar e Planejar recebem somente capacidades de leitura. Executar mantém
aprovação por operação, proteção de caminhos e sandbox. Aprovações não são
restauradas do disco; skills, hooks, recursos externos e texto do modelo não
podem concedê-las. No Linux, Bubblewrap é o padrão; ausência do isolamento causa
falha. Execução sem sandbox exige configuração global explícita.

Undo/redo reverte edições de arquivos das ferramentas após prévia, aprovação e
revalidação. Não reverte efeitos de comandos, processos, serviços externos ou
alterações manuais. Recibos de verificações ficam vinculados à revisão/digest
atual e são invalidados quando os arquivos mudam.

Antes de gerar, o agente estima instruções, histórico, anexos, schemas e reserva
de saída. Essa é uma heurística conservadora, não o tokenizer do provedor.
`autoCompact` e `/compact` preparam localmente o contexto da mesma conversa;
não fazem uma geração escondida nem criam uma conversa resumida automaticamente.
Se ainda não couber, o pedido é recusado antes da rede. O histórico salvo
continua disponível.

MCP não inicia processos ou rede ao cadastrar/abrir a aplicação. Descoberta e
chamadas acontecem sob demanda, com aprovação e prazos. Alterações no cadastro
valem para a próxima chamada. Servidores LSP ativos são atualizados no próximo
início. Credenciais conhecidas são redigidas, mas esse mecanismo não identifica
segredos arbitrários escritos em código.

Detalhes: [ciclo do agente](docs/agent-runtime.md),
[contexto e extensões](docs/context-and-extensions.md),
[ferramentas de código](docs/code-tools.md),
[ferramentas e isolamento](docs/tools.md) e [arquitetura](ARCHITECTURE.md).

## Testes e avaliações

```sh
go test -race ./...
go vet ./...
go build .
go run . evaluate --output /tmp/opcode-fixtures.json
```

A avaliação offline verifica contratos das fixtures; não executa nem simula um
agente. O modo real exige `--live --provider-config /caminho/provedor.json` e pode
gerar custos em provedores remotos; casos de execução exigem também
`--allow-execution`. Ele usa projetos, HOME e bancos temporários. Passar critérios
não demonstra correção semântica geral.

Consulte [avaliações repetíveis](docs/evaluation.md),
[testes reais com Ollama](docs/ollama-live-tests.md) e
[testes das APIs dos provedores](internal/llm/provider/integration/README.md).
A suíte comum não requer credenciais nem executa inferência paga. Os resultados
observados com `llama3.1:8b`, incluindo os critérios reprovados, estão no
[registro de validação](docs/validation.md).

## Recuperação e controle do contexto

Há 42 perfis de linguagens com extração local, listagem e leitura com páginas verificadas,
índice incremental e memória de projeto. `/index` controla o índice; `/runtime`
mostra um diagnóstico sem credenciais; `/context` mostra origem e orçamento.
Embeddings locais são opcionais. Veja [guia de recuperação](docs/retrieval-runtime.md)
e [o registro desta implementação](docs/implementation-2026-10.md).

O [editor de tema](docs/theme-editor.md) permite personalizar a paleta com prévia
e salvamento explícito. [Raciocínio e clipboard](docs/model-and-clipboard.md)
descreve controles por capacidade e cópia por SSH. O comando
[`watch`](docs/shared-observer.md) acompanha mensagens salvas em outra janela
somente para leitura; os comandos existentes `serve`/`attach` continuam disponíveis
para reconectar um cliente à instância ativa. A
[matriz de regressão da interface](docs/ui-regression-matrix.md) registra a
cobertura determinística dos menus.
O [guia de perguntas e evidências](docs/question-and-evidence-recovery.md)
explica a recuperação de opções malformadas, a busca por assunto, as citações
em todos os modos e o reaproveitamento de fontes revalidadas.

## Proveniência e licença

Opcode deriva do [OpenCode original](https://github.com/opencode-ai/opencode).
A modernização neste repositório mantém a proveniência e a [licença MIT](LICENSE)
originais. Este fork tem identidade e desenvolvimento próprios; os binários e
métodos de distribuição do projeto original não representam esta versão.
