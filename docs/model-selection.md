# Configurar provedores e escolher o modelo

Configure uma vez em `~/.opcode.json` (ou no arquivo global já utilizado pelo
aplicativo). Um `.opcode.json` no projeto pode sobrescrever essas configurações.
Reinicie o aplicativo depois de editar os provedores no JSON.

Para Ollama, não é necessário cadastrar manualmente cada modelo:

```json
{
  "providers": {
    "local": {
      "baseURL": "http://127.0.0.1:11434/v1",
      "api": "ollama",
      "contextWindow": 16384,
      "defaultMaxTokens": 2048
    }
  }
}
```

Use os limites adequados ao modelo e à memória da sua máquina. A lista contém
os modelos disponíveis no servidor; consultar essa lista não baixa nem carrega
os pesos. Na primeira inicialização sem um agente configurado, o aplicativo usa
um dos modelos retornados pelo servidor. A escolha feita depois na interface é
salva automaticamente. Não há download automático de modelos.

Para LM Studio, use seu endereço (geralmente `http://127.0.0.1:1234/v1`) e
`"api": "chat_completions"`. `LOCAL_ENDPOINT`, quando definido, tem precedência
sobre `providers.local.baseURL` tanto para listar quanto para gerar respostas.

Para uma API com autenticação, prefira guardar apenas o nome da variável no JSON:

```json
{
  "providers": {
    "openai": {
      "apiKeyEnv": "MINHA_CHAVE_OPENAI"
    },
    "anthropic": {
      "apiKeyEnv": "MINHA_CHAVE_ANTHROPIC",
      "disabled": true
    }
  }
}
```

Defina `MINHA_CHAVE_OPENAI` no ambiente antes de iniciar o aplicativo. As variáveis
usuais (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, etc.) e o campo
legado `apiKey` continuam funcionando. Com `apiKeyEnv` definido, ele tem
precedência sobre `apiKey`; uma variável ausente gera erro sem revelar o segredo.
Provedores desativados não exigem a variável. A chave resolvida nunca é gravada
quando você escolhe um modelo.

## Na aplicação

- Clique no nome do modelo com `▾`, no rodapé, ou pressione `Ctrl+O`.
- O seletor começa no provedor do modelo atual e consulta seu catálogo ao vivo.
- Use `←`/`→` para alternar entre os provedores habilitados. Também é possível
  clicar na metade esquerda/direita do título.
- Digite parte do nome ou ID para filtrar. `Backspace` apaga a busca.
- Use `↑`/`↓`, a roda do mouse, `Enter` ou clique em uma linha para escolher.
- `Ctrl+R` atualiza a lista; `Esc` fecha sem mudar o modelo.

A interface continua respondendo durante a consulta. Fechar, atualizar ou trocar
de provedor cancela a consulta anterior e descarta respostas atrasadas. Uma lista
vazia é exibida sem erro. Se a consulta falhar, o seletor avisa e mantém a lista
configurada como alternativa. A troca é recusada enquanto o agente trabalha;
aguarde terminar ou cancele a execução antes de tentar novamente.

A escolha altera o agente `coder`. As configurações dos agentes auxiliares
permanecem independentes. O ID e a definição do modelo são salvos para funcionar
também na próxima inicialização, sem depender de uma consulta para reconhecê-lo.
Se o projeto já define `agents.coder.model`, a escolha vai para esse
`.opcode.json`; caso contrário, vai para o arquivo global em uso. A atualização
preserva campos desconhecidos, referências a variáveis de ambiente e as demais
configurações. O arquivo é substituído atomicamente com permissão `0600`.

## Catálogos e metadados

Há consulta dinâmica para Ollama/LM Studio, OpenAI, Anthropic, Gemini, Groq,
OpenRouter e xAI. Azure, Bedrock, Vertex e Copilot continuam mostrando os modelos
cadastrados; o seletor identifica essa origem como catálogo configurado. Não
promete uma listagem das permissões ou deployments dessas plataformas.

`baseURL` pode personalizar o endpoint dos provedores com consulta dinâmica. O
mesmo endereço é usado para inferência. As plataformas com autenticação própria
continuam usando suas configurações específicas de endpoint.

As definições explícitas em `models` têm precedência sobre os metadados da
consulta e os padrões do provedor. Nomes e IDs são separados para distinguir
variantes como `:7b` e `:8b`. Modelos conhecidos preservam seus limites,
capacidades e preços cadastrados. Para modelos novos, usamos os limites
informados pelo servidor, ou padrões conservadores quando não há metadados;
capacidades avançadas e preços não são presumidos. Use `models` para corrigir ou
complementar essas informações. Preços ausentes não são uma estimativa de custo.

Referências dos protocolos de catálogo:
[OpenAI](https://developers.openai.com/api/reference/resources/models/methods/list),
[Anthropic](https://platform.claude.com/docs/en/api/models/list) e
[Gemini](https://ai.google.dev/api/models#method:-models.list).

## Perfis, favoritos e preferências

`Ctrl+P` dentro do seletor abre **Connections and preferences**. Use `N` para
cadastrar uma conexão, `Enter` para editar uma conexão existente e `P` para
editar preferências. No formulário, `Tab` alterna campos, `Ctrl+T` testa o
catálogo e `Ctrl+S` salva. O teste não gera respostas nem baixa modelos. `Esc`
cancela o teste mantendo as edições; um segundo `Esc` volta à lista.

O formulário pede o **nome da variável de ambiente**, sem exibir seu conteúdo.
Chaves já presentes em um arquivo privado são preservadas; segredos resolvidos
do ambiente não são gravados. Feche a execução atual antes de salvar ajustes de
provedor ou limites. Editar um provedor existente no projeto salva no mesmo
arquivo; conexões novas são salvas na configuração global em uso.

Perfis permitem dois servidores com o mesmo protocolo. O ID do perfil é
independente do `backend`, enquanto `name` é o rótulo exibido na interface:

```json
{
  "providerProfiles": {
    "casa": {
      "name": "Ollama em casa",
      "backend": "local",
      "baseURL": "http://127.0.0.1:11434/v1",
      "api": "ollama",
      "contextWindow": 16384,
      "defaultMaxTokens": 2048
    },
    "trabalho": {
      "name": "Gateway do trabalho",
      "backend": "openai",
      "baseURL": "https://gateway.example/v1",
      "apiKeyEnv": "CHAVE_GATEWAY",
      "api": "chat_completions"
    }
  },
  "preferences": {
    "resumeLast": true,
    "showProcess": false
  }
}
```

Modelos descobertos recebem IDs distintos, como `profile.casa.llama3.1:8b`, e
são salvos com `provider_profile: "casa"`. Definições manuais também podem usar
`provider_profile`, mantendo `provider` igual ao backend. O formato legado
`providers.local`, `providers.openai`, etc. continua válido. `LOCAL_ENDPOINT`
sobrescreve apenas a conexão legada `local`, sem alterar perfis nomeados.

No seletor, `Ctrl+F` marca/desmarca um favorito e os modelos usados recentemente
aparecem antes dos demais. `Ctrl+A` alterna a função configurada: principal
(`coder`), tarefas (`task`), títulos (`title`) e resumos (`summarizer`). Escolher
um auxiliar não altera o modelo principal. Os limites de contexto, saída,
protocolo e capacidades declaradas aparecem abaixo da lista. Quando o servidor
não fornece contexto, o seletor identifica o limite como **fallback estimate**;
configure um limite adequado em vez de presumir a capacidade máxima do modelo.

A primeira abertura sem modelo inicia a configuração. Após salvar uma conexão,
escolha um modelo para começar; os agentes auxiliares ainda não configurados
herdam essa escolha. Preferências permitem definir modo inicial, retomada da
última conversa, visibilidade do processo e os limites existentes do runtime.

IDs de perfis usam letras minúsculas, números, `_` ou `-` (até 64 caracteres).
O nome exibido pode conter letras maiúsculas e espaços. Em **Settings**, `I`
abre o [cadastro de integrações MCP e LSP](integrations-settings.md).
