# Testes de integração com APIs reais

Estes testes usam a mesma interface `provider.Provider` do aplicativo. Fazem chamadas de geração que podem consumir créditos da conta. Os testes comuns e o CI padrão continuam locais: a execução real exige **a tag `integration` e `OPCODE_LIVE_TESTS=1`**. Ter chaves no ambiente não ativa os testes.

Cada provedor selecionado executa, em sequência:

1. Uma resposta normal com um marcador conhecido.
2. Uma resposta por streaming, verificando os deltas e o evento de conclusão.
3. Uma chamada à ferramenta sintética `opcode_integration_probe`, seguida do envio do resultado e da continuação por streaming. A continuação inclui as assinaturas e os dados opacos do provedor, quando presentes.

A ferramenta retorna texto fixo. Não executa comandos, não acessa arquivos e não usa as ferramentas reais do agente. São quatro operações de geração por caso quando todas as etapas passam; reenvios HTTP podem aumentar o número de requisições. Se uma etapa falha, as etapas seguintes desse caso não são executadas. Os testes não acessam o banco de conversas do usuário.

## Configurar uma vez

Na raiz do repositório:

```sh
./scripts/test-providers.sh --init
```

Isso cria `$XDG_CONFIG_HOME/opcode/integration.json`, ou `~/.config/opcode/integration.json` quando `XDG_CONFIG_HOME` não está definido. O arquivo é criado com permissão `0600` no Unix; arquivos existentes não são sobrescritos. Abra-o no editor, preencha o modelo disponível na sua conta e ative somente os casos desejados. O template começa com todos desativados e nenhum modelo predefinido.

Exemplo mínimo usando uma chave já exportada no ambiente:

```json
{
  "timeoutSeconds": 180,
  "providers": {
    "openai-responses": {
      "enabled": true,
      "model": "ID_DO_MODELO_DISPONIVEL_NA_SUA_CONTA",
      "apiKeyEnv": "OPENAI_API_KEY",
      "canReason": true,
      "maxTokens": 4096
    }
  }
}
```

Use `canReason: true` para modelos OpenAI compatíveis que exigem `max_completion_tokens`; `reasoningEffort` é opcional e só deve receber um valor aceito pelo modelo. Para Anthropic/Bedrock, `thinkingMode` pode ser `enabled` ou `adaptive`, conforme o modelo. Ajuste `maxTokens` ao orçamento de saída/raciocínio necessário. O limite do teste é de 1 a 16384 tokens por operação. Para modelos sem ferramentas, `"tools": false` desativa explicitamente apenas essa etapa e registra um `SKIP`.

Para guardar a chave uma única vez no próprio arquivo, substitua `apiKeyEnv` por `"apiKey": "PREENCHA_NO_EDITOR_LOCAL"`. O arquivo é JSON, não é executado nem interpretado como shell. Variáveis de ambiente têm prioridade, inclusive quando estão explicitamente vazias. `apiKeyEnv` permite escolher um nome de variável diferente do padrão de cada provedor.

**As chaves no arquivo ficam em texto simples.** A permissão `0600` limita o acesso no Unix, mas não criptografa o arquivo. No Windows, configure as ACLs da conta ou prefira variáveis de ambiente/credenciais nativas. O leitor rejeita arquivos não regulares, links simbólicos e, no Unix, qualquer permissão para grupo/outros. Para corrigir um arquivo copiado manualmente, use `chmod 600 CAMINHO_DO_ARQUIVO`.

Para executar novamente sem reconfigurar:

```sh
./scripts/test-providers.sh --run
```

Para selecionar apenas um subconjunto, ou usar um arquivo específico:

```sh
./scripts/test-providers.sh --run --providers openai-responses,anthropic
./scripts/test-providers.sh --init --config /caminho/privado/integration.json
./scripts/test-providers.sh --run --config /caminho/privado/integration.json
```

`--providers` seleciona exatamente os casos indicados, mesmo se `enabled` estiver falso. Um caso selecionado sem modelo ou sem credencial obrigatória falha antes das chamadas. Nenhum caso selecionado também é um erro, para evitar um resultado verde sem testar nada.

Este arquivo é específico dos testes e não altera o `opcode.json` do aplicativo. `integration.json`, `integration.local.json` e `.opcode-integration.json` estão no `.gitignore`; mantenha arquivos com outros nomes fora do repositório.

## Somente variáveis de ambiente

Não é necessário criar um arquivo. Exemplo em Bash, sem gravar a chave literal no histórico do shell:

```bash
read -rsp 'OPENAI_API_KEY: ' OPENAI_API_KEY
printf '\n'
export OPENAI_API_KEY
export OPCODE_TEST_OPENAI_RESPONSES_MODEL='ID_DO_MODELO_NA_SUA_CONTA'
export OPCODE_TEST_OPENAI_RESPONSES_CAN_REASON=true
./scripts/test-providers.sh --run --providers openai-responses
```

Cada caso usa `OPCODE_TEST_<CASO_EM_MAIUSCULAS_COM_UNDERSCORES>_MODEL`. Definir essa variável com um modelo não vazio seleciona o caso; apenas exportar a chave não o seleciona. As variáveis por caso `MAX_TOKENS`, `CAN_REASON`, `REASONING_EFFORT` e `THINKING_MODE` também sobrescrevem o arquivo.

| Caso | Variável do modelo | Credencial padrão |
| --- | --- | --- |
| `openai` | `OPCODE_TEST_OPENAI_MODEL` | `OPENAI_API_KEY` |
| `openai-responses` | `OPCODE_TEST_OPENAI_RESPONSES_MODEL` | `OPENAI_API_KEY` |
| `anthropic` | `OPCODE_TEST_ANTHROPIC_MODEL` | `ANTHROPIC_API_KEY` |
| `gemini` | `OPCODE_TEST_GEMINI_MODEL` | `GEMINI_API_KEY` |
| `groq` | `OPCODE_TEST_GROQ_MODEL` | `GROQ_API_KEY` |
| `openrouter` | `OPCODE_TEST_OPENROUTER_MODEL` | `OPENROUTER_API_KEY` |
| `xai` | `OPCODE_TEST_XAI_MODEL` | `XAI_API_KEY` |
| `local` | `OPCODE_TEST_LOCAL_MODEL` | `LOCAL_API_KEY`, opcional |
| `azure` | `OPCODE_TEST_AZURE_MODEL` | `AZURE_OPENAI_API_KEY` ou Entra ID |
| `azure-responses` | `OPCODE_TEST_AZURE_RESPONSES_MODEL` | `AZURE_OPENAI_API_KEY` ou Entra ID |
| `vertexai` | `OPCODE_TEST_VERTEXAI_MODEL` | Application Default Credentials |
| `bedrock` | `OPCODE_TEST_BEDROCK_MODEL` | Credenciais AWS ou `AWS_BEARER_TOKEN_BEDROCK` |
| `copilot` | `OPCODE_TEST_COPILOT_MODEL` | `GITHUB_TOKEN`, com acesso ao Copilot |

Configurações adicionais:

- Local: `LOCAL_ENDPOINT`, ou `endpoint` no arquivo, incluindo o prefixo `/v1` quando necessário.
- Azure: `AZURE_OPENAI_ENDPOINT` e `AZURE_OPENAI_API_VERSION`, ou `endpoint` e `apiVersion` no arquivo. Use o nome da implantação como modelo. `apiVersion: "v1"` usa `/openai/v1`; versões datadas preservam as rotas anteriores. Sem chave, o adaptador usa as credenciais Entra ID existentes.
- Vertex: `VERTEXAI_PROJECT`/`GOOGLE_CLOUD_PROJECT`, `VERTEXAI_LOCATION`/`GOOGLE_CLOUD_LOCATION`, ou `project`/`location` no arquivo. `credentialsFile` pode apontar para um arquivo ADC existente; `GOOGLE_APPLICATION_CREDENTIALS` tem prioridade. Não copie o conteúdo do arquivo ADC para este JSON.
- Bedrock: `AWS_REGION`/`AWS_DEFAULT_REGION`, `AWS_PROFILE`, ou `region`/`awsProfile` no arquivo, além das credenciais da cadeia AWS existente. A opção `apiKey`/`apiKeyEnv` desse caso é um bearer token do Bedrock, não um AWS access key ID. Access keys temporárias, secret keys e session tokens continuam nas variáveis/arquivos nativos do AWS.

`OPCODE_LIVE_CONFIG` escolhe o arquivo; um caminho explícito inexistente é erro. `OPCODE_LIVE_PROVIDERS` aceita os mesmos nomes separados por vírgula. `OPCODE_LIVE_TIMEOUT_SECONDS` sobrescreve o prazo de requisições por caso (padrão 180 segundos, máximo 600). O script limita a execução total a 30 minutos.

Sem o script, inclusive no Windows, o comando equivalente é:

```sh
OPCODE_LIVE_TESTS=1 go test -tags=integration ./internal/llm/provider/integration \
  -run '^TestLiveProviders$' -count=1 -v -timeout=30m
```

No PowerShell, defina as variáveis com `$env:NOME = 'valor'` antes de executar `go test`. Configure as chaves por um meio que não grave seus valores no histórico.

## Relatórios e validação local

Os relatórios mostram somente nomes fixos dos casos/etapas e categorias seguras de erro, como código HTTP ou timeout. Não imprimem chaves, cabeçalhos, prompts, respostas, assinaturas de raciocínio nem corpos de erro do servidor. Não há opção de chave na linha de comando nem modo de log detalhado nessa suíte. Não a execute com tracing externo de HTTP ou shell se precisar preservar essa garantia.

Os testes de configuração e do fluxo usam dados sintéticos e rodam normalmente, sem rede:

```sh
go test ./internal/llm/provider/integration
go test -race ./...
go vet ./...
```

Para conferir apenas a compilação e o bloqueio da suíte real:

```sh
OPCODE_LIVE_TESTS=0 go test -tags=integration ./internal/llm/provider/integration \
  -run '^TestLiveProviders$' -count=1 -v
```

Uma falha de conteúdo pode decorrer de instruções não seguidas pelo modelo, falta de suporte a ferramentas, filtros ou orçamento insuficiente. Uma falha HTTP também pode indicar modelo indisponível, implantação incorreta, falta de permissão ou quota. Os testes não escolhem automaticamente outro modelo nem reenviam uma geração depois que seu streaming começou.
