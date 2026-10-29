# Avaliações repetíveis

O comando `evaluate` prepara fixtures descartáveis e gera um relatório JSON por
critério observável. Não escreve no repositório original, não baixa modelos e
não inclui credenciais, respostas completas nem conteúdos de arquivos no
relatório.

## Offline: validar o conjunto de avaliação

```sh
go run . evaluate --output /tmp/opcode-fixtures.json
# Ou use um conjunto próprio no mesmo formato:
go run . evaluate --cases internal/evaluation/testdata/cases.json \
  --output /tmp/opcode-fixtures.json
```

Esse modo é determinístico: verifica o contrato, limites, caminhos e preparação
de cópias temporárias. O relatório declara `offline_fixture_validation`, cada
caso fica `validated` e cada resultado `passed` fica `null`. **Isso não executa
nem simula um agente e não é uma nota de qualidade do modelo.** Nenhum provedor
ou chave é necessário.

O conjunto incluído cobre seis fluxos: explicar um projeto a partir de arquivos,
localizar um erro, corrigir um arquivo, executar uma verificação nomeada,
interpretar um erro de leitura e retomar um plano na mesma conversa.

## Live: executar o provedor real

```sh
go build -o /tmp/opcode-eval .
/tmp/opcode-eval evaluate --live \
  --provider-config /caminho/para/seu/provedor.json \
  --allow-execution \
  --output /tmp/opcode-live.json
```

`--live` é opt-in explícito e pode gerar custos nos provedores remotos.
`--provider-config` deve selecionar `agents.coder.model` explicitamente. Use
`apiKeyEnv` para referenciar uma chave por variável; o relatório não a recebe.
O arquivo pode reutilizar os objetos `providers`, `providerProfiles`, `models` e
`agents` existentes. Perfis auxiliares ausentes usam o modelo principal.

Cada caso roda em **outro processo**, com workspace, HOME e banco temporários:
os singletons de configuração e o cache de instruções não atravessam os casos.
Apenas campos de provedores/modelos/agentes são copiados para um JSON privado;
MCP, LSP, skills e hooks do usuário não são ativados. O sandbox continua
Bubblewrap, sem rede para comandos. O acesso HTTP do provedor segue o cliente
normal da aplicação. A aplicação usa o agente e o provedor reais; não há resposta
simulada no caminho de produção.

Casos Execute exigem `--allow-execution` antes de qualquer requisição do conjunto.
O runner concede uma autorização por solicitação somente às ferramentas
mutáveis conhecidas e somente dentro da fixture. Não chama
`AutoApproveSession`, não lembra aprovações e recusa caminhos externos e ações
de rede. Essa opção permite executar os comandos explicitamente declarados nas
fixtures; revise conjuntos personalizados antes de executar. Se Bubblewrap não
estiver disponível, os critérios que precisam de comandos falham sem desativar
a proteção.

Critérios aceitos:

- `response_contains`, `response_not_contains`: texto da **última** resposta.
- `file_contains`, `file_not_contains`: arquivo regular dentro da fixture.
- `tool_called`: chamada real registrada pelo agente.
- `expect_error`: erro real do turno, comparado sem incluí-lo no relatório.
- `task_status`: estado persistente da tarefa.
- `check_passed`: registro aprovado para a revisão e digest atuais.

As comparações textuais são simples e explícitas; aprovação não demonstra
correção semântica geral. O comando retorna erro quando qualquer caso live
falha, mantendo o relatório com os critérios aprovados/reprovados. Falhas de
configuração, limite de tempo e relatório inválido também são diferenciadas sem
copiar payloads de provedores. Workspaces temporários são removidos ao final.

Relatórios live também registram `task_status`, necessidade/observação de
alterações, presença de justificativa sem mudança e nomes limitados/redigidos de
ferramentas. Esses metadados permitem distinguir trabalho parcial/bloqueado de
conclusão; não incluem argumentos, texto da justificativa, respostas completas,
hash de arquivos do usuário ou credenciais. Campos ausentes no modo offline
não representam resultados executados.

## Testes

```sh
go test -race ./internal/evaluation ./cmd
```

Há também um teste real de explicação de projeto no Ollama, limitado a loopback:

```sh
go build -o /tmp/opcode-eval .
OPCODE_LIVE_TESTS=1 OPCODE_LIVE_OLLAMA=1 \
OPCODE_EVAL_BINARY=/tmp/opcode-eval \
go test -tags=integration ./internal/evaluation \
  -run '^TestLiveOllamaOverviewEvaluation$' -count=1 -timeout=12m
```

O teste não instala modelos. Usa `llama3.1:8b` por padrão e admite
`OPCODE_LIVE_OLLAMA_MODEL` e `LOCAL_ENDPOINT`. O teste é opt-in; a suíte comum
não faz inferência nem acessa uma API externa.

Para as seis tarefas originais, incluindo edição, citação e retomada de plano:

```sh
OPCODE_LIVE_TESTS=1 OPCODE_LIVE_OLLAMA=1 \
OPCODE_EVAL_BINARY=/tmp/opcode-eval \
go test -tags=integration ./internal/evaluation \
  -run '^TestLiveOllamaWorkflowEvaluation$' -count=1 -timeout=12m
```

Esse teste permite as ferramentas mutáveis somente nas fixtures descartáveis,
com os mesmos controles do runner. Não altera prompts, critérios ou timeouts
da suíte. É uma avaliação real opt-in, não um teste determinístico da aplicação.

## Comparar mudanças e repetir os mesmos critérios

```sh
opcode evaluate --live --case edit,resume --repeat 2 \
  --provider-config /caminho/provedor.json --allow-execution \
  --output /tmp/opcode-repeated.json
```

`--case` seleciona IDs existentes; não modifica prompts nem critérios. O hash
continua identificando a suíte completa de origem, e `selected_cases` registra o
subconjunto. `--repeat` aceita 1–3 execuções live, cada uma com processos e
workspaces novos; multiplica as requisições e possíveis custos. O relatório
registra `trial` quando há repetição e duração por caso. O modo offline permanece
determinístico e só valida uma vez. Compare uma variável por vez — instruções,
schema ou modelo — e preserve os resultados reprovados como linha de base.

A correção do arquivo não implica checks aprovados: uma fixture que exige apenas
conteúdo pode passar com a tarefa `partial`. Uma avaliação de conclusão completa
deve exigir também `task_status: completed` e `check_passed` para seus checks.

## Comparação Opcode / Codaro / Crush

Há drivers concretos em `scripts/evaluation_adapter.py`. Eles executam aplicações
reais e não substituem respostas por fixtures. O Codaro usa a API pública Python
`Agent.ask`, pois `codaro chat` é interativo. O Crush usa `crush run --quiet`,
com continuação da mesma sessão para os pedidos seguintes. Nada é instalado.

O driver deve ser executado pelo Python do ambiente que já contém o Codaro:

```json
{
  "name": "codaro",
  "command": "/caminho/codaro/.venv/bin/python",
  "args": ["/caminho/opcode/scripts/evaluation_adapter.py", "--application", "codaro"]
}
```

Para Crush, selecione um Python 3 existente em `command` e use os argumentos:
`["/caminho/opcode/scripts/evaluation_adapter.py", "--application", "crush",
"--crush-command", "/caminho/absoluto/crush"]`. O driver de subprocesso do Crush
requer POSIX para limitar a saída em disco. O executável precisa estar instalado;
a avaliação não o baixa nem usa `--yolo`.

Use configuração de provedor explícita para que os fatores sejam verificáveis:

```json
{
  "providers": {"local": {"baseURL": "http://127.0.0.1:11434/v1"}},
  "models": {"local.llama3.1:8b": {
    "provider": "local", "api": "ollama", "api_model": "llama3.1:8b",
    "context_window": 16384, "default_max_tokens": 2048
  }},
  "agents": {"coder": {"model": "local.llama3.1:8b", "maxTokens": 2048}}
}
```

Os drivers só recebem fixtures descartáveis e a cópia privada de configuração.
O Codaro conserva os modos reais, exige Bubblewrap para comandos e aprova edição
somente nos arquivos existentes da fixture quando `--allow-execution` foi dado.
MCP/plugins/LSP são desligados. Comandos livres são recusados. O check shell
nomeado do caso `validate` não tem binding `argv` idêntico no Codaro; o driver
marca esse caso como `unavailable`, sem reescrever o pedido ou fabricar recibo.

O driver Crush suporta somente Perguntar, com ferramentas mutáveis, comandos,
busca de rede e delegação desativados. Planejar/Executar ficam `unavailable`.
APIs OpenAI/Anthropic explícitas são traduzidas; o protocolo Ollama nativo não é
trocado silenciosamente por OpenAI. Essa comparação fica indisponível no driver
Crush. A saída CLI não expõe recibos confiáveis de ferramentas, tarefa ou tokens:
essas medidas são `null` e critérios dependentes ficam `unavailable`.

`unavailable` nunca significa aprovado e faz a execução retornar erro depois de
gravar o relatório. Critérios observáveis que falharam continuam reprovados,
mesmo quando outro critério é indisponível. Para comparar o subconjunto comum,
selecione explicitamente os mesmos casos nos dois comandos:

```sh
/tmp/opcode-eval evaluate --live --case overview,localize,error \
  --provider-config /caminho/provedor.json --context-window 16384 --repeat 3 \
  --experiment baseline --output /tmp/opcode-native.json
/tmp/opcode-eval evaluate --live --case overview,localize,error \
  --provider-config /caminho/provedor.json --context-window 16384 --repeat 3 \
  --adapter /caminho/codaro-adapter.json --output /tmp/codaro-live.json
/tmp/opcode-eval evaluate compare /tmp/opcode-native.json /tmp/codaro-live.json \
  --output /tmp/comparison.json
```

`compare` exige relatórios live limitados, registra os totais por aplicação e
marca diferenças de hash da suíte, casos/repetições/critérios, modelo da API,
protocolo, contexto efetivo e reserva de resposta. Não soma métricas ausentes
como zero e não inclui critérios indisponíveis entre os aprovados. As políticas,
prompts e ferramentas dos aplicativos continuam diferentes; condições iguais
não demonstram superioridade geral ou isolamento de uma variável causal.

O relatório nativo inclui versão da aplicação e do prompt, perfil observado
(`minimal`/`reduced`/`full`), método e componentes do orçamento. `--experiment`
aceita um rótulo ASCII de até 64 caracteres para comparar execuções observadas;
não muda o system prompt. Alterar contexto também muda o perfil e precisa ser
declarado como diferença, em vez de atribuir o resultado apenas ao prompt.

### Recuperação com fontes concorrentes

O benchmark direto de ranking independe do agente e não gera chat:

```sh
opcode evaluate retrieval --output /tmp/ranking.json
# Opcional, apenas com um modelo de embeddings já instalado:
opcode evaluate retrieval --embedding-config /caminho/embeddings.json \
  --output /tmp/ranking-hybrid.json
```

O JSON opcional contém somente `endpoint` e `model`, por exemplo
`{"endpoint":"http://127.0.0.1:11434/api/embed","model":"nomic-embed-text"}`.
Fornecê-lo autoriza chamadas locais limitadas para embeddings. São recusados
endpoints externos, redirects, chaves e campos adicionais. Sem esse arquivo,
nenhum serviço de embeddings é contatado; o híbrido registra fallback lexical.

Cada fixture e estratégia usam outro processo e workspace descartável; a query é
repetida no mesmo índice para registrar estado `cold` e `warm`. A ferramenta real
`search_code_index` conserva suas permissões e limites. O relatório inclui recall,
MRR, estratégia solicitada/efetiva, arquivos lidos/reutilizados/atualizados,
quantidade de resultados, bytes de resposta e tempo. Nenhum conteúdo de código é
incluído. Os casos cobrem símbolo com linguagem/escopo, implementação concorrente
e sinônimos deliberadamente ausentes da fonte. Zero recall no caso de sinônimos
é uma medida possível; não é convertido em sucesso fictício de embeddings.

`--cases` aceita o contrato separado de `internal/evaluation/testdata/retrieval.json`:
até 16 casos, cada um com `id`, `files`, `query`, `path`/`language` opcionais e
`expected` obrigatório. O arquivo de workflow original não é alterado.

`examples/evaluation-retrieval.json` é uma suíte adicional com caminhos esperados,
linguagens e diretórios distintos, implementações concorrentes e constantes
diferentes. Os seis casos originais permanecem intactos. Execute com `--cases`
e o mesmo provedor opt-in; `expected_retrieval` produz recall e MRR a partir dos
caminhos realmente retornados. A resposta também precisa citar e interpretar a
fonte correta; acertar apenas o ranking não aprova todos os critérios.

O relatório nativo registra `retrieval_strategies`, incluindo o fallback efetivo.
Busca híbrida já exige embeddings locais configurados explicitamente; a suíte
não instala nem ativa um modelo de embeddings. Os drivers externos não fingem
equivalência de ranking quando não observam as páginas de busca. Para comparar
embeddings, use fixtures e configurações declaradas separadamente e preserve os
relatórios, inclusive resultados de cache frio/quente e falhas de serviço.

Testes dos drivers sem inferência:

```sh
python3 scripts/evaluation_adapter_test.py
/caminho/codaro/.venv/bin/python scripts/evaluation_adapter_test.py
```

A segunda execução também exercita a API pública do Codaro com provedor
roteirizado: realiza leitura de arquivo real e confere os recibos. Isso valida
compatibilidade do driver; não é uma avaliação de qualidade do modelo.

Leituras iniciais do Codaro possuem recibos reais em `local_retrievals`, separados
das chamadas emitidas pelo modelo. O driver inclui esses recibos sem contar suas
cópias novamente. `native_tool_names` guarda os nomes nativos, `tool_names` guarda
os equivalentes do avaliador (`read_lines`/`read_symbol` → `view`) e
`local_tool_names` identifica leituras do bootstrap. Uma tentativa de leitura que
falhou comprova somente a chamada, nunca uma leitura bem-sucedida. Sem recibo,
nenhuma equivalência é inferida a partir da resposta em prosa.

O runner aceita um adapter executável explícito via JSON:

```json
{"name":"codaro","command":"/caminho/absoluto/driver","args":[]}
```

```sh
opcode evaluate --live --case overview,localize \
  --provider-config /caminho/provedor.json --context-window 16384 \
  --adapter /caminho/adapter.json --output /tmp/codaro-report.json
```

Nomes aceitos: opcode/codaro/crush. O driver é integração explícita confiável,
não uma chamada de ferramenta do modelo. Recebe `Job` em stdin: workspace
descartável, config privada, caso e conditions (model, context_window, mode,
fixture_sha256). Devolve `AdapterObservation`: mesmas conditions, response,
version, tools, input_tokens/output_tokens/tool_calls (null se ausentes) e
retrieved_paths, além de task (incluindo recibos de verificação) quando aplicável. Consulte os tipos em `internal/evaluation/adapter.go`.
O adapter deve utilizar a API/CLI da versão instalada e respeitar o modo;
o Opcode não instala nem modifica os outros projetos. Métricas/autorizações
externas ficam identificadas como declaradas pelo adapter, não recibos nativos
verificados. Condições divergentes são recusadas. Esse contrato comum não afirma
que uma comparação real dos três aplicativos já foi realizada.

No worker nativo, o relatório soma uso real do runtime (incluindo cache na
entrada; título separado fica fora), contabiliza ferramentas e mantém os
critérios originais. `expected_retrieval` (até 16 caminhos relativos) acrescenta
recall e reciprocal rank da recuperação observada; não é avaliação semântica da
resposta. Mesmo modelo, janela, fixture, prompts e permissões são necessários
para comparar resultados. Tempos, memória/cache aquecido e versões também devem
ser registrados pelo operador. O total de tokens fica ausente quando o provedor
não informa uso; ausência não significa custo zero.

Regressões locais adicionais cobrem continuidade durante um turno longo,
instruções preservadas, arquivos alterados entre páginas, watcher/cancelamento,
rebuild falho, expiração de schemas e requisitos explícitos de chamadas. O teste
real `TestLiveOllamaCompoundRetrievalRequirements` verifica duas páginas,
busca filtrada e leitura de fonte num único pedido, com arquivos preservados.

Falhas de ferramenta podem aparecer como contagens por nome/categoria em
`tool_failures`; `agent_failure_kind` identifica a categoria do encerramento.
Não incluem argumentos, mensagens completas de erro nem payloads de provedores.


### Amostragem controlada no Ollama

Relatórios incluem `temperature` quando observada; desconhecida fica `null`.
`evaluate compare` distingue temperaturas diferentes ou observação ausente de
zero. O adapter Codaro nativo usa a extensão pública do codec `wire_payload`
para fixar somente a temperatura em zero, igual ao Opcode. Agente, prompts,
transporte/cancelamento, ferramentas e permissões do Codaro permanecem reais.
Sem esse controle, o padrão Codaro 0,1 seria um fator adicional da comparação.
Outros protocolos não recebem uma temperatura presumida. Mesmo com os fatores
compartilhados, aplicativos mantêm prompts/políticas diferentes; não há inferência
de superioridade geral ou de efeito causal de uma mudança isolada.

## Diagnóstico de tempo por fase

O runtime expõe métricas agregadas por sessão em `/runtime` → detalhes, no objeto
`usage`. Cada caso nativo da avaliação inclui `phase_metrics`, mesmo se o
provedor falhar sem informar tokens. Os valores são números/counters, sem prompt,
argumentos de ferramentas, conteúdo de arquivos ou credenciais:

- `context_ms`: preparação inicial de histórico/bootstrap e preparação/orçamento
  das requisições; não mede todo processamento interno da aplicação.
- `provider_ms`: duração observada das chamadas de streaming, incluindo espera de
  transporte/retentativas, tratamento de eventos e persistência das mensagens.
  Não é tempo puro de inferência do servidor.
- `tools_ms`: duração das chamadas reais de ferramentas, incluindo aprovação,
  sandbox e espera por processo quando ocorrem dentro da ferramenta. Uma chamada
  recusada ou cancelada continua sendo uma observação, sem executar novamente.
- `first_token_ms` e `first_token_samples`: soma das esperas pelo primeiro evento
  gerado observável e quantidade de amostras. Dividir a soma pelas amostras dá a
  média observada; quando não há amostra, a média é indisponível. Um evento pode
  trazer texto, raciocínio ou chamada de ferramenta; não é uma contagem de tokens.
- `provider_calls`, `tool_calls` e `repairs`: chamadas observadas e regenerações de
  protocolo, contexto, conclusão ou citações. Retentativas HTTP internas integram
  o tempo da chamada lógica e não são novas chamadas nesse contador.
  O contador de ferramentas considera o loop explícito do agente; leituras de
  bootstrap pertencem à fase de contexto e podem aparecer separadamente nos
  recibos históricos. Não confunda esses contadores com o total de recibos.

Os tempos usam relógio monotônico e arredondam para baixo cada observação em
milissegundos. São acumulados na memória do processo atual; não sobrevivem ao
reinício nem incluem a geração auxiliar do título. Cancelamentos/erros encerram
os temporizadores uma única vez. Não se deve somar o primeiro-token ao tempo do
provedor: ele é um subconjunto desse tempo. As fases não decompõem todo o tempo
fim a fim do caso e não provam uma melhoria de desempenho.

`evaluate compare` soma fases somente quando todos os casos do relatório incluem
essas métricas; caso contrário, apresenta `null`. Relatórios antigos e adapters
sem instrumentação permanecem utilizáveis, sem receber números inventados ou
alterar os critérios e condições da comparação. Medidas negativas ou com soma
fora do limite de inteiro são recusadas.
