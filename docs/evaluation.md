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
