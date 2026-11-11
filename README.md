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

Use Go 1.27 ou posterior para compilar o checkout. O módulo recomenda a toolchain
1.27.3; com `GOTOOLCHAIN=auto`, o Go pode obtê-la automaticamente. O CI acompanha
os patches da linha 1.27.

As versões das bibliotecas e a pendência de trocar o commit oficial do Lip Gloss
por uma release estão no [guia de dependências](docs/dependencies.md).

```sh
git clone --branch develop https://github.com/raulaguila/opcode.git
cd opcode
go build -o opcode .
./opcode
```

Se `go run .` ou `go build` falhar com `disk quota exceeded`, confira o
armazenamento temporário e o cache do compilador. Uma quota por usuário em
`/tmp` pode impedir gravações mesmo quando `df -h` mostra espaço livre. Para
usar uma pasta temporária no disco da sua conta, apenas nessa execução:

```sh
mkdir -p "${XDG_CACHE_HOME:-$HOME/.cache}/opcode/go-tmp"
GOTMPDIR="${XDG_CACHE_HOME:-$HOME/.cache}/opcode/go-tmp" go run .
```

`go env GOCACHE GOTMPDIR` mostra os diretórios usados pelo Go. O comando
`go clean -cache` remove o cache de compilação selecionado em `GOCACHE`, que será
recriado na próxima compilação. Se houver caches antigos com caminhos explícitos,
confira cada caminho antes de executar `GOCACHE=/caminho/do/cache go clean -cache`.

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

### Terminal ou app

O opcode tem duas interfaces sobre o mesmo agente, as mesmas conversas e a mesma
configuração. Escolha a que preferir; dá para alternar a qualquer momento.

```sh
opcode       # no terminal; funciona também via SSH
opcode app   # numa janela própria, fora do terminal
```

O binário comum não depende de bibliotecas gráficas, para rodar também em
servidores. Nele, `opcode app` abre uma janela de app do Chrome, Chromium, Edge
ou Brave, quando houver um instalado, e senão o navegador padrão.

Para a janela nativa (Wails), compile com a tag `desktop`. No Linux ela usa a
WebKitGTK do sistema:

```sh
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev   # Ubuntu 24.04 ou posterior
go build -tags desktop,production,webkit2_41 -o opcode .
```

Em distribuições com a WebKitGTK 4.0, instale `libwebkit2gtk-4.0-dev` e tire
`webkit2_41` das tags. No macOS e no Windows basta `-tags desktop,production`;
o Windows precisa do WebView2, que já vem no Windows 10 e 11 atualizados.

Em qualquer janela, a página só fala com o processo local, que escuta apenas em
`127.0.0.1` com uma chave nova a cada abertura. Use `opcode app --no-open` para
só mostrar o endereço.

### Automação

`opcode -p "pedido"` roda um pedido sem interface. O pedido também pode vir pela
entrada padrão (`git diff | opcode -p "revise"` ou `opcode < pedido.txt`, até
1 MiB). Com `-f json`, a saída traz `response`, `outcome`, `session_id`,
`files_changed` e `checks`.

| Saída | Significado |
| --- | --- |
| `0` | tarefa concluída |
| `1` | erro |
| `2` | tarefa parcial ou bloqueada; a resposta foi impressa |
| `3` | `--max-cost` atingido; a conversa fica salva |
| `130` | cancelada |

Sem interface ninguém aprova nada, então as alterações são recusadas.
`--approval sandbox` deixa editar o projeto e rodar comandos no sandbox, e recusa
internet e arquivos fora do projeto; `--approval auto` também restaura
dependências declaradas enquanto o worktree for igual ao último commit (com
alterações locais, a rede volta a pedir permissão); `--auto-approve` aprova
tudo (inseguro).
`--max-cost 0.50` cancela a execução quando o custo estimado passa de US$ 0,50,
verificado após cada resposta do modelo. `--debug` grava `debug.log` (0600) na
pasta de dados e mostra o caminho; o arquivo contém pedidos e saídas de
ferramentas.

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

Sem modelo configurado, `opcode` e `opcode app` abrem em **Nova conexão**:
escolha um ponto de partida, `Ctrl+T` testa o catálogo e `Ctrl+S` salva. Escolha
então o modelo com `Ctrl+O`. A autenticação aceita chave direta em campo
mascarado, variável de ambiente, o cofre do sistema ou nenhuma chave;
[veja como configurar](docs/model-selection.md#informar-a-api-key-diretamente).
O rodapé reúne a política de aprovação, a atividade, o uso do contexto, a
velocidade da última resposta e o modelo. `~/.opcode/config.json` é reutilizado entre projetos;
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
      "api": "ollama"
    }
  }
}
```

O contexto é consultado no provedor, sem guardar seu tamanho no JSON.
Um `contextWindow` positivo é uma opção para definir um limite manual.
Sem `defaultMaxTokens`, um modelo local responde com até 1/4 da janela
(entre 4096 e 16384 tokens); defina-o para fixar outro limite.
O seletor identifica contexto alocado, máximo anunciado ou estimativa alternativa,
e mostra suporte a ferramentas informado pelo provedor (ou desconhecido).
A configuração básica inclui o estado ativo da conexão; salvar um perfil
desativado não o ativa silenciosamente.
Consultar o catálogo não baixa
pesos nem gera respostas. Clique no modelo do rodapé ou pressione `Ctrl+O` para
buscar e escolher: `Tab` troca de conexão, `Ctrl+D` mostra os detalhes (onde o
modelo está carregado, velocidade, e no app os ajustes de contexto e
raciocínio) e `Ctrl+P` abre as conexões. `✓` identifica o modelo ativo.
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

As duas interfaces, suas telas, teclas e a segurança do app estão em
[Terminal e app](docs/interfaces.md).

Veja o [guia de uso diário](docs/daily-workflow.md) para configurar, definir o
resultado esperado de tarefas e trabalhar com Git/worktrees.

Para reabrir a última conversa disponível do projeto atual:

```sh
opcode --resume
opcode --resume ID_DA_SESSAO
```

`--resume=ID_DA_SESSAO` também é aceito. Sem conversa anterior disponível,
`--resume` informa isso e abre uma nova. A retomada interativa abre histórico e
rascunho, sem continuar ferramentas automaticamente nem restaurar aprovações.
Na conversa, `/resume` pede a continuação da tarefa salva. `/quit` inicia o
encerramento da interface, preservando o rascunho e as proteções de operações em
andamento. No cliente `attach`, `/quit` apenas desconecta do servidor.

## Recursos

| Frente | Disponível neste fork |
| --- | --- |
| Tarefas | Contratos explícitos em `/contract` ou `--task-kind`, com alvos, critérios e checks da revisão atual. |
| Auditoria | `/auditoria` revisa todo o projeto, inclusive com modelos pequenos: analisadores, três passadas por arquivo, revisão dos achados altos com chamadores e testes que os provam; repetir só pergunta sobre o que mudou |
| Configuração | Perfis com ID, nome e backend separados; múltiplos endpoints, formulários, teste de conexão, catálogo dinâmico, favoritos, recentes e modelos auxiliares independentes. |
| Chat | Mensagens em ordem cronológica intercaladas com grupos recolhíveis de ferramentas, seleção/cópia, aviso de novas mensagens e rascunhos por conversa. |
| Execução | Terminal PTY com entrada do usuário, saída ao vivo e fila de instruções editável durante o trabalho. |
| Contexto | Estimativa preventiva do payload e schemas, reserva de resposta e compactação local preservando a conversa salva. |
| Revisão | Diffs por interação, recibos de checks e undo/redo de edições de arquivos com detecção de conflitos e aprovação. |
| Conversas | Busca, renomeação, arquivamento, exportação, retomada opcional e ações de recuperação de erro. |
| Ferramentas | Listagem paginada, índice local com filtros e prévias curtas, mapa, outline/símbolos, artefatos paginados, checks, navegação/edição LSP e MCP sob demanda. |
| Linguagens | 26 perfis de parsers locais; diagnóstico `doctor --languages --json` distingue parser, LSP e checks sem instalar ferramentas. |
| Referências | Citações verificadas contra caminho, linhas realmente lidas e hash atual; reparo limitado preserva o pedido original. |
| Decisões | Painel destacado de perguntas individuais ou agrupadas, seleção explícita única/múltipla, texto livre e revisão antes do envio. |
| Extensões | Skills invocadas explicitamente pelo usuário e hooks globais opt-in, sujeitos às permissões e ao sandbox. |
| Avaliações | Fixtures e avaliações reais opt-in, drivers Codaro/Crush, comparação de condições e medição de ranking lexical/híbrido sem inferência. |
| Confiabilidade | Conclusão com evidência de alteração ou justificativa explícita, estado parcial/bloqueado e recuperação de contexto/OOM com calibração local temporária. |
| Produtividade | Índice incremental FTS5/BM25, exploração somente leitura com orçamento, formatação e worktrees protegidas. |
| Preferências | Input crescente, busca de configurações com origem efetiva, atalhos, idioma e notificações opcionais. |
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
no JSON global, `"tui": {"language":"pt","notifications":"off"}` seleciona
controles em português. Sem `language`, o idioma segue o locale (`LANG`).
Atalhos podem ser alterados em `tui.keybindings` (`commands`, `models`,
`sessions`, `terminal`, `files`) e a ajuda (`F1`) mostra os valores em uso; valores controlados por ambiente precisam ser alterados na
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

Comece por [Terminal e app](docs/interfaces.md). Os principais controles das
duas interfaces:

| Controle | Ação |
| --- | --- |
| `Ctrl+K`, `/` com o campo vazio | Comandos |
| `Shift+Tab` | Alterna Perguntar, Planejar e Executar |
| `Enter` / `Alt+Enter` | Envia / nova linha |
| `Ctrl+O` | Escolher modelo |
| `Ctrl+S` | Conversas: abrir, renomear, exportar, arquivar |
| `Ctrl+N` | Nova conversa |
| `Ctrl+P` | Comandos executados: saída e cópia |
| `F1` | Todas as teclas |
| `Esc` | Cancela a execução |
| `1` `2` `3` | Responde a uma aprovação |
| `@`, `Ctrl+F` | Cita um arquivo do projeto |

## Permissões, contexto e limites

Perguntar e Planejar recebem somente capacidades de leitura. Executar mantém
aprovação por operação, proteção de caminhos e sandbox. Aprovações não são
restauradas do disco; skills, hooks, recursos externos e texto do modelo não
podem concedê-las. No Linux, Bubblewrap é o padrão; ausência do isolamento causa
falha. Execução sem sandbox exige configuração global explícita. No macOS e no
Windows ainda não há sandbox: comandos falham até você definir
`security.sandbox=none` na configuração global, e a partir daí rodam com as
permissões da sua conta, protegidos apenas pela aprovação de cada comando.

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
go test -race -p 1 ./...
go vet ./...
go build .
go run . evaluate --output /tmp/opcode-fixtures.json
```

O gate de manutenção usa Gocognit 1.3.0 e Staticcheck 0.8.2. As ferramentas de
análise são instaladas separadamente; não entram nas dependências da aplicação:

```sh
go install github.com/uudashr/gocognit/cmd/gocognit@v1.3.0
go install honnef.co/go/tools/cmd/staticcheck@v0.8.2
make complexity staticcheck
```

Benchmarks de componente e comparação de alocações estão em
[medição de desempenho](docs/performance.md) (`make bench-performance` e
`make bench-compare`), com comparação pareada no CI e fixtures de SQLite,
busca completa, imagens e tokenizer reconhecido.

O limite é 15 por função, incluindo testes e código gerado, sem exclusões.
Staticcheck cobre também os arquivos com a tag `integration`, sem executar
as APIs reais. `make quality` reúne complexidade, Staticcheck, vet, a suíte comum
e `make test-integration`. Este último executa as regressões determinísticas com
a tag `integration` e detector de corridas, com os opt-ins de serviços reais
desativados. O CI executa essas duas suítes e verifica o inventário de linguagens.
Para regenerar o código SQLC, consulte `internal/db/generate.go`: o pós-processador
reproduzível de prepared statements mantém os métodos gerados dentro do limite.

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
[registro de validação](docs/validation.md). A
[nova comparação com Codaro e Crush](docs/comparison-2026-10-29.md) registra também
condições ausentes e limites dos adapters.

## Recuperação e controle do contexto

O [inventário gerado de linguagens](docs/language-support.md) registra os perfis de extração local, listagem e leitura com páginas verificadas,
índice incremental e memória de projeto. `/index` controla o índice; `/runtime`
mostra um diagnóstico sem credenciais; `/context` mostra origem e orçamento.
Embeddings locais são opcionais. Veja [guia de recuperação](docs/retrieval-runtime.md)
e [o registro desta implementação](docs/implementation-2026-10.md).

[Raciocínio e clipboard](docs/model-and-clipboard.md)
descreve controles por capacidade e cópia por SSH. O comando
[`watch`](docs/shared-observer.md) acompanha mensagens salvas em outra janela
somente para leitura; os comandos existentes `serve`/`attach` continuam disponíveis
para reconectar um cliente à instância ativa.
O [guia de perguntas e evidências](docs/question-and-evidence-recovery.md)
explica a recuperação de opções malformadas, a busca por assunto e o reaproveitamento de fontes revalidadas.

[Cofre nativo opcional](docs/native-vault.md) guarda
credenciais por referência; chave direta e ambiente continuam disponíveis.
A [busca web opcional](docs/tools.md#busca-web-opcional) começa desativada e
mantém a aprovação de rede. Para comparações, use o
[manifesto compartilhado](docs/evaluation.md), que identifica condições ausentes
ou diferentes antes de permitir um ranking.

## Proveniência e licença

Opcode deriva do [OpenCode original](https://github.com/opencode-ai/opencode).
A modernização neste repositório mantém a proveniência e a [licença MIT](LICENSE)
originais. Este fork tem identidade e desenvolvimento próprios; os binários e
métodos de distribuição do projeto original não representam esta versão.
