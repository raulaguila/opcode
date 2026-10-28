# Opcode

Agente de desenvolvimento em Go para trabalhar em repositórios pelo terminal.
A interface permite perguntar, planejar e executar com ferramentas protegidas,
acompanhar comandos ao vivo e enviar novas instruções durante o trabalho.
O suporte a Ollama inclui o protocolo nativo e APIs locais compatíveis com OpenAI.

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
editar arquivos do shell. Builds de release e checksums só estarão disponíveis
quando uma versão for publicada neste repositório. Não há distribuição Homebrew
ou AUR configurada para este fork.

## Primeiro acesso e modelos

Sem modelo configurado, a abertura interativa mostra **Settings**. `N` cadastra
uma conexão; `Ctrl+T` testa o catálogo e `Ctrl+S` salva. Escolha então o modelo
pelo seletor. Um `.opcode.json` global pode ser reutilizado entre projetos;
`./.opcode.json` pode acrescentar ou sobrescrever configurações do projeto.

Para configurar Ollama uma vez, por exemplo:

```json
{
  "providerProfiles": {
    "local": {
      "name": "Ollama local",
      "backend": "local",
      "baseURL": "http://127.0.0.1:11434/v1",
      "api": "ollama",
      "contextWindow": 16384,
      "defaultMaxTokens": 2048
    }
  }
}
```

Use limites adequados ao modelo e ao servidor. Consultar o catálogo não baixa
pesos nem gera respostas. Clique no modelo do rodapé, pressione `Ctrl+O` ou use
`/models` para buscar, trocar de perfil e escolher. `Ctrl+F` marca favoritos,
`Ctrl+A` alterna a função principal/auxiliar e `Ctrl+R` atualiza o catálogo.
A escolha e os metadados são salvos, sem serializar chaves resolvidas do ambiente.

Provedores remotos podem usar `apiKeyEnv` com o nome de uma variável. As variáveis
usuais (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, etc.) e o formato
`providers` continuam disponíveis. Uma assinatura de aplicativo de chat não
fornece credenciais de API a este programa. No modo `--prompt`, a configuração
válida deve existir antes da execução.

Veja [configuração de provedores/modelos](docs/model-selection.md),
[o schema JSON](opcode-schema.json) e
[integrações MCP/LSP](docs/integrations-settings.md).

## Recursos

| Frente | Disponível neste fork |
| --- | --- |
| Configuração | Perfis com ID, nome e backend separados; múltiplos endpoints, formulários, teste de conexão, catálogo dinâmico, favoritos, recentes e modelos auxiliares independentes. |
| Chat | Processo recolhível por interação, resposta final separada, ações de texto/código/arquivos, aviso de novas mensagens, histórico e rascunhos por conversa. |
| Execução | Terminal PTY com entrada do usuário, saída ao vivo e fila de instruções editável durante o trabalho. |
| Contexto | Estimativa preventiva do payload e schemas, reserva de resposta e compactação local preservando a conversa salva. |
| Revisão | Diffs por interação, recibos de checks e undo/redo de edições de arquivos com detecção de conflitos e aprovação. |
| Conversas | Busca, renomeação, arquivamento, exportação, retomada opcional e ações de recuperação de erro. |
| Ferramentas | Mapa, outline/símbolos, artefatos paginados, descoberta de checks, hierarquia/renomeação/substituição LSP e ferramentas/recursos MCP sob demanda. |
| Extensões | Skills invocadas explicitamente pelo usuário e hooks globais opt-in, sujeitos às permissões e ao sandbox. |
| Avaliações | Fixtures determinísticas e avaliações reais opt-in, com critérios observáveis e suporte a Ollama. |

Comece pelo [guia da interface](docs/workspace-ui.md). Os principais controles:

| Controle | Ação |
| --- | --- |
| `/` e `Ctrl+K` | Sugestões de comandos e paleta |
| `/mode ask`, `/mode plan`, `/mode execute` | Perguntar, Planejar e Executar |
| `Ctrl+B`, `/process` | Expandir/recolher o processo da última interação |
| `Ctrl+End`, `/latest` | Ir às mensagens novas |
| `Ctrl+Y`, `/actions` | Copiar resposta/código ou abrir citação |
| `Ctrl+P` | Mostrar/ocultar terminal |
| `/queue` | Editar/remover instruções pendentes |
| `/context` | Ver orçamento; `C` compacta no painel |
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

## Proveniência e licença

Opcode deriva do [OpenCode original](https://github.com/opencode-ai/opencode).
A modernização neste repositório mantém a proveniência e a [licença MIT](LICENSE)
originais. Este fork tem identidade e desenvolvimento próprios; os binários e
métodos de distribuição do projeto original não representam esta versão.
