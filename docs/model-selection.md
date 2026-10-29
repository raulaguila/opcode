# Configurar provedores e escolher o modelo

Configure uma vez em `~/.opcode/config.json` (ou no arquivo global já utilizado
pelo aplicativo). `./.opcode/config.json` pode sobrescrever essas configurações
por projeto. A inicialização normal cria o arquivo global canônico quando ausente:
preserva nele a configuração global antiga, mantendo o original, ou usa um template
se nenhuma existir. Não sobrescreve um destino existente. O template
está em [`internal/config/template.json`](../internal/config/template.json), com
Ollama, LM Studio e exemplos remotos desativados, sem credenciais ou modelo escolhido.
Reinicie o aplicativo depois de editar os provedores no JSON.

Clique no modelo no rodapé para abrir o seletor. Contexto, origem e suporte a
ferramentas aparecem ao selecionar uma linha. “Não informado” significa que o
provedor não enviou essa capacidade; não é uma inferência pelo nome do modelo.
`Ctrl+D` mostra ID, protocolo e outros detalhes. O formulário básico da conexão
mostra nome, URL, variável da chave e Conexão ativa. `Ctrl+S` salva sem mudar
esse estado; `Ctrl+G` salva e ativa explicitamente. Perfis desativados permanecem
visíveis em Conexões, mas não entram no catálogo ativo. `Ctrl+A` abre os campos
técnicos sem alterar valores escondidos.

Uma cópia de referência também é criada em `~/opcode/config.example.json` a cada
inicialização normal em que estiver ausente. Esse arquivo não é carregado e não
recebe suas credenciais; use seus campos como guia ao editar a configuração
ativa. `opcode --reset` faz backup da configuração global, restaura os padrões e
inicia o aplicativo. Veja [reset e recuperação](daily-workflow.md#primeiro-acesso).

Para Ollama, não é necessário cadastrar manualmente cada modelo:

```json
{
  "providers": {
    "local": {
      "baseURL": "http://127.0.0.1:11434/v1",
      "api": "ollama",
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
- Use `Alt+←`/`Alt+→` para alternar entre os provedores habilitados. Também é possível
  clicar na metade esquerda/direita do título.
- Digite parte do nome ou ID para filtrar. A busca tem cursor próprio; `←`/`→`,
  `Home`, `End` e `Backspace` editam o texto sem alterar o rascunho da conversa.
- Use `↑`/`↓`, `PgUp`/`PgDn` ou a roda do mouse para navegar; `Enter` ou clique
  em uma linha confirma. `›` indica o destaque e `✓` identifica o modelo ativo.
  O contador informa a posição e a quantidade de resultados; setas indicam mais linhas.
- Nomes longos são abreviados na lista. `Ctrl+D` mostra ID, contexto/origem,
  protocolo e capacidades; ausência de metadados aparece como não informada.
- `Ctrl+R` atualiza a lista; `Esc` fecha sem mudar o modelo.

A interface continua respondendo durante a consulta. Fechar, atualizar ou trocar
de provedor cancela a consulta anterior e descarta respostas atrasadas. Uma lista
vazia é exibida sem erro. Se a consulta falhar, o seletor avisa e mantém a lista
configurada como alternativa. A troca é recusada enquanto o agente trabalha;
aguarde terminar ou cancele a execução antes de tentar novamente.

A escolha altera o agente `coder`. As configurações dos agentes auxiliares
permanecem independentes. O ID e a referência do modelo são salvos para reconhecê-lo na próxima
inicialização. O contexto automático é consultado no provedor e fica em memória.
Se o projeto já define `agents.coder.model`, a escolha vai para esse
`.opcode/config.json` (ou o formato anterior em uso); caso contrário, vai para
o arquivo global. A atualização
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

Limites positivos definidos manualmente em `models.context_window` têm
precedência sobre os metadados da consulta e os padrões do provedor. Nomes e IDs são separados para distinguir
variantes como `:7b` e `:8b`. Modelos conhecidos preservam seus limites,
capacidades e preços cadastrados. Para modelos novos, usamos os limites
informados pelo servidor, ou padrões conservadores quando não há metadados;
capacidades avançadas e preços não são presumidos. Use `models` para corrigir ou
complementar essas informações. Preços ausentes não são uma estimativa de custo.

Referências dos protocolos de catálogo:
[OpenAI](https://developers.openai.com/api/reference/resources/models/methods/list),
[Anthropic](https://platform.claude.com/docs/en/api/models/list) e
[Gemini](https://ai.google.dev/api/models#method:-models.list).

### Contexto automático

Os presets omitem `contextWindow`; não é necessário preencher o tamanho do
contexto. Omitir `models.<id>.context_window` também solicita descoberta automática.
Ollama consulta
`GET /api/ps` e `POST /api/show`, sem carregar modelos nem executar inferência.
Consulta os detalhes mesmo se o modelo já estiver carregado. Usa `num_ctx` do
Modelfile quando presente, ou o máximo anunciado em
`model_info["<arquitetura>.context_length"]`. A alocação anterior retornada por
`/api/ps` é uma alternativa quando os detalhes não estão disponíveis; ela não
limita permanentemente um modelo nativo capaz de receber um novo `num_ctx`.
LM Studio preserva o contexto
carregado ou o máximo de seu catálogo. O máximo arquitetural pode exigir mais
memória que a disponível; limites aprendidos após falhas de memória continuam ativos.
As APIs compatíveis que não expõem esses dados usam uma estimativa identificada.
Veja as referências de [metadados do Ollama](https://docs.ollama.com/api-reference/show-model-details)
e [contexto alocado](https://docs.ollama.com/api/ps).

A inicialização normal e `Ctrl+R` consultam os metadados. A seleção automática
não grava `context_window`, `context_source`, `context_estimated` nem
`context_auto` no JSON: o valor descoberto e sua origem ficam somente em memória.
Offline ou sem metadados do provedor, usa o catálogo interno quando disponível ou
uma estimativa de 4.096 tokens, identificada na interface. Não há cache de contexto
no arquivo de configuração. Nem todos os provedores expõem esse dado em sua API.

Um `contextWindow` positivo no provedor ou `context_window` positivo no modelo
continua sendo um limite manual. Omitir o campo (ou usar zero) ativa o automático.
Por exemplo, uma referência suficiente em `models` é:

```json
{
  "models": {
    "local.llama3.1:8b": {
      "provider": "local",
      "api": "ollama",
      "api_model": "llama3.1:8b"
    }
  },
  "agents": {
    "coder": { "model": "local.llama3.1:8b" }
  }
}
```

A conexão local deve estar configurada em `providers.local`; um perfil nomeado
usa `provider_profile` na referência. O formato anterior com `context_auto: true`
e valores salvos continua aceito e atualizado em memória; selecionar novamente
remove os metadados de contexto antigos do JSON. Para ativar o automático em uma
definição manual antiga, remova `context_window` e o `contextWindow` do provedor,
sem alterar os demais campos. Para voltar a uma versão antiga que exige contexto
no JSON, restaure um backup da configuração ou informe o limite manualmente.

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

## Presets e opções avançadas

Em `/settings`, `N` abre presets de Ollama, LM Studio, OpenAI, Anthropic, Gemini e
OpenRouter. `↑↓` escolhe e `Enter` prepara o formulário; nada é salvo ou conectado
nesse passo. Revise o ID, endereço e referência da chave antes de salvar.
`Ctrl+A` mostra/oculta protocolo, limites de contexto/saída e desativação.
Os modelos não são baixados automaticamente.

A consulta `Ctrl+T` do formulário verifica **somente metadados do catálogo**.
Ela não confirma a qualidade do modelo nem sua capacidade de chamar ferramentas.
O catálogo cancelável e a seleção mantêm o modelo ativo quando uma consulta falha;
resultados de uma consulta fechada ou anterior são descartados.

A origem efetiva e referências redigidas ficam em `/settings` → `P` → `Ctrl+E`.
Variáveis `OPCODE_...` e aliases anteriores `OPENCODE_...` são identificadas pelo
nome da variável realmente definida, sem serializar seu valor.

No seletor de modelos, `Ctrl+T` abre uma confirmação separada para **teste real de
ferramentas**. Revise o modelo e pressione `Enter` para enviar uma inferência;
`Esc` volta sem enviar. Pode haver cobrança do provedor. O teste usa um desafio
aleatório e schema inerte, limita contexto a 4096 e saída a 256 tokens, e não
executa comandos nem ferramentas. Ele não envia o repositório ou a conversa.
Enquanto testa, `Esc` cancela; resultados atrasados são descartados. A seleção
ativa não é alterada pelo teste, e um sucesso comprova apenas aquela chamada
estruturada, não a qualidade geral nem a segurança de futuras execuções.

## Alocação real do Ollama

`/local-status` e `opcode doctor --ollama-status [--model ID]` usam somente
[GET /api/ps](https://docs.ollama.com/api/ps). Mostram a janela configurada, o
contexto observado no modelo carregado, CPU/GPU e `size_vram`. Não medem a memória
livre e não prometem que outra janela caberá. Se o modelo estiver descarregado,
o diagnóstico informa isso sem carregá-lo. Não há redirects com credenciais.

O próximo pedido pode mudar a alocação; um limite aprendido após erro de memória
pode reduzir a janela da requisição. Use `/context` para o orçamento efetivo e
`/recalibrate` para remover esse limite, sem repetir ferramentas.
