# Validação real do terminal e das instruções com Ollama

## Contexto automático e primeira pergunta

`TestLiveOllamaFirstQuestionUsesAdvertisedContext` executa o binário de produção
em um projeto descartável, com JSON sem `context_window`. Descobre os metadados,
faz uma primeira pergunta sobre o projeto e verifica a janela realmente alocada
em `/api/ps`, a resposta concluída e a preservação dos arquivos. Não usa
`--auto-approve`; o modo ask permanece ativo. Há inferência real, sem downloads.

```sh
go build -o /tmp/opcode-context-capacity .
OPCODE_LIVE_TESTS=1 OPCODE_LIVE_OLLAMA=1 \
  OPCODE_EVAL_BINARY=/tmp/opcode-context-capacity \
  go test -tags=integration ./internal/evaluation \
    -run '^TestLiveOllamaFirstQuestionUsesAdvertisedContext$' \
    -count=1 -v -timeout=280s
```

Na validação de 28/10/2026, passou com `llama3.1:8b` e **131.072 tokens alocados**.
O teste determinístico de metadados também cobre o caso que antes falhava:
um modelo já alocado com 16.384 tokens, mas anunciando capacidade de 131.072.
A alocação antiga não deve substituir a capacidade disponível pela API nativa.
Limites manuais e a redução após uma falha real de memória continuam válidos.
Os testes históricos abaixo usam 16.384 tokens deliberadamente para testar
qualidade e ferramentas sob esse orçamento; eles não definem o limite da aplicação.

## Atualização de 28/10/2026: respostas sobre o projeto

O caminho nativo do Ollama agora pode ser selecionado com `api: "ollama"` na definição do modelo local. O contexto é enviado por requisição (`options.num_ctx`), junto com o limite de saída e temperatura zero. O servidor recebe `truncate: false`: um contexto insuficiente deve causar erro em vez de descartar silenciosamente instruções. A implementação segue o protocolo de [chat do Ollama](https://docs.ollama.com/api/chat) e seu [streaming NDJSON](https://docs.ollama.com/api/streaming), sem SDK adicional.

O prompt local foi reduzido e instrui o agente a ler os arquivos antes de descrever o projeto. Corrigiu-se também a leitura de IDs com pontos, como `local.llama3.1:8b`, que o Viper expandia incorretamente como caminhos de configuração.

Quando ferramentas estão disponíveis, o adaptador oferece uma função interna `final_answer` para modelos cujo template exige uma chamada de função mesmo quando basta responder. Essa função só publica texto na conversa; não executa comandos nem passa pela execução de ferramentas do agente. Uma resposta final misturada com operações na mesma geração é rejeitada. Chamadas reais continuam sujeitas às permissões existentes.

O teste `TestLiveOllamaProjectOverview` envia a pergunta **“o que pode me falar sobre o projeto atual?”** a um agente real com todas as ferramentas. Ele exige que o modelo leia o README de um projeto temporário e mencione fatos presentes nesse arquivo. Antes da correção, falhou porque o modelo respondeu sem ler arquivos. Após a correção, passou com o `llama3.1:8b` original e `-race`. O aplicativo também foi executado sobre este repositório e leu seu README para responder sobre Opcode, Go, a interface de terminal e SQLite.

Para reproduzir o teste da pergunta, execute o mesmo comando abaixo trocando o filtro por `-run '^TestLiveOllamaProjectOverview$'`. Execute cada teste em um processo separado, pois a configuração é global ao processo.

Os dois testes agora usam a API nativa, contexto de 16.384 tokens e temperatura zero, sem criar variantes do modelo. O teste do terminal envia a nova instrução em português enquanto o comando aguarda entrada e passou com `-race`. Em tentativas com a instrução sintética em inglês, o modelo ainda tentou executar `echo` para produzir o marcador, e a permissão foi negada. Os testes mantêm essa negação como erro; um resultado positivo não garante obediência a todos os prompts.

O registro de 27/10 abaixo descreve o caminho anterior, compatível com OpenAI. Para a configuração atual, use o exemplo de Ollama nativo no README; não é necessário criar o Modelfile histórico.

## Terminal interativo

O teste `TestLiveOllamaTerminalAndSteering` usa o agente e os provedores reais, todas as ferramentas do agente, SQLite, permissões e a interface Bubble Tea. A interface recebe eventos de teclado programaticamente, sem abrir um terminal na tela. O subprocesso usa um PTY real dentro do Bubblewrap, com rede desativada.

Ele cria configuração, banco e um script sintético em diretórios temporários. Não usa o histórico nem modifica a configuração do usuário. O script confirma que stdin é um terminal, imprime `LIVE_READY`, espera uma linha e imprime o valor recebido. A compactação automática fica desativada para isolar o fluxo de terminal e instruções.

O teste verifica:

1. O modelo solicita `bash interactive.sh` com `pty=true` por uma chamada nativa de ferramenta.
2. Nenhum processo existe antes da aprovação pelo diálogo da interface. Pedidos diferentes do comando esperado são negados.
3. A saída aparece enquanto o comando ainda aguarda entrada.
4. Uma nova instrução enviada pelo editor entra na fila enquanto o agente está ocupado.
5. `Ctrl+P` abre o terminal; o texto digitado chega ao PTY e desbloqueia o script.
6. O comando termina com código zero, seu resultado é persistido antes da nova instrução e o modelo responde incluindo o marcador solicitado `LIVE_STEERING_OK`.
7. A resposta final aparece na interface e o programa encerra.

## Executar

Requer Linux, Bash, Bubblewrap e Ollama acessível por loopback. O modelo deve estar instalado; o teste não baixa modelos nem cria variantes. Execute este teste isoladamente porque a configuração do aplicativo é global ao processo:

```sh
OPCODE_LIVE_TESTS=1 OPCODE_LIVE_OLLAMA=1 \
LOCAL_ENDPOINT=http://127.0.0.1:11434/v1 \
go test -race -tags=integration ./internal/tui \
  -run '^TestLiveOllamaTerminalAndSteering$' -v -count=1 -timeout=5m
```

O modelo padrão é `llama3.1:8b`. `OPCODE_LIVE_OLLAMA_MODEL` permite selecionar uma variante local já instalada. A tag `integration` sozinha não ativa chamadas reais: são necessárias as duas variáveis de consentimento acima. Os testes comuns continuam sem geração de modelos.

## Resultado observado em 27/10/2026

Servidor Ollama 0.33.3, pesos `llama3.1:8b` de ID `46e0c10c039e`:

| Cenário | Resultado |
| --- | --- |
| Resposta normal e streaming pela interface `provider.Provider` | Passaram |
| Chamada da ferramenta sintética na suíte geral de provedores | Falhou: o modelo produziu texto em vez da chamada nativa esperada |
| Agente completo com o modelo original carregado em contexto de 4.096 tokens | Falhou antes da aprovação: produziu código ilustrativo sem executar a ferramenta |
| Agente completo com variante dos mesmos pesos, contexto de 16.384 tokens e comando curto | Passou: aprovação, saída ao vivo, entrada humana pelo PTY, instrução durante execução e resposta final |
| Mesmo fluxo com contexto de 16.384 tokens, temperatura zero e `-race`, após a correção abaixo | Passou, sem corridas de dados detectadas |

O contexto maior não resolveu todos os prompts: pedidos que incluíam o script inteiro ou instruções mais complexas também produziram chamadas em texto. Houve variação entre execuções mesmo com o comando curto; temperatura zero foi usada na verificação final para reduzir essa variação. O resultado positivo comprova o fluxo descrito, mas não garante que esse modelo siga ferramentas de forma consistente em outras tarefas. O teste falha nesses casos; não converte texto gerado em comandos, não troca de modelo e não aprova operações adicionais.

O primeiro teste real com `-race` encontrou compartilhamento da memória de mensagens entre o agente em streaming e a interface. A publicação agora cria uma cópia independente das partes da mensagem, incluindo anexos e estado opaco do provedor. Um teste de regressão cobre mensagens criadas e atualizadas; antes da correção, a alteração de uma mensagem modificava retroativamente o evento já publicado. Após a correção, passaram o fluxo real com `-race`, `go test -race ./...` e `go vet ./...`.

Para reproduzir a configuração de contexto maior sem alterar o modelo original, salve um Modelfile temporário:

```text
FROM llama3.1:8b
PARAMETER num_ctx 16384
PARAMETER temperature 0
```

Crie uma variante local com `ollama create llama3.1-opcode:8b -f CAMINHO_DO_MODELFILE` e defina `OPCODE_LIVE_OLLAMA_MODEL=llama3.1-opcode:8b` no comando do teste. Isso reutiliza os pesos instalados. A API compatível com OpenAI não oferece ajuste de contexto por requisição; o Ollama documenta o uso de `num_ctx` em um Modelfile: [OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility).

A variante usada durante esta validação foi temporária e removida ao final. A configuração do projeto e o modelo original permaneceram inalterados.

Os testes de API, que usam somente uma ferramenta sintética sem comandos de sistema, estão documentados em [integrações de provedores](../internal/llm/provider/integration/README.md).
# Validação do ciclo do agente — 28/10/2026

Após modos, contexto inicial e ferramentas progressivas, os testes reais com
Ollama 0.33.3 e `llama3.1:8b` passaram com `-race`, em processos separados:

- `TestLiveOllamaProjectOverview`: leitura real de contexto e resposta contendo
  os fatos esperados do README temporário (IgarapeWatch, sensores e SQLite).
- `TestLiveOllamaTerminalAndSteering`: comando exato `bash interactive.sh` com
  PTY, aprovação pela interface, saída parcial, entrada humana e nova instrução
  durante a espera. A nova instrução foi seguida após o resultado do comando,
  sem operações extras autorizadas; Bubblewrap permaneceu habilitado.

Na primeira verificação do terminal, o modelo escolheu `process_start` apesar
do pedido explícito de `bash`; o teste recusou a operação. A seleção inicial
passou a antecipar ferramentas de processos apenas para pedidos de segundo
plano ou nomes explícitos dessas ferramentas. O teste original passou depois,
mantendo as exigências de comando, PTY, autorização e ordem das mensagens.

Esses testes não qualificam todos os modelos nem comprovam a interpretação de
qualquer projeto. As novas perguntas, modos, persistência e validação também
têm regressões determinísticas e um fluxo da TUI sem dependência de inferência.
Detalhes de configuração em [agent-runtime.md](agent-runtime.md).

