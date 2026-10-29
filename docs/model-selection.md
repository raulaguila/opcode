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
mostra nome, URL, autenticação e Conexão ativa. Em **Autenticação**, use
`←`/`→` para escolher **Chave direta**, **Variável de ambiente** ou **Sem autenticação**.
Apenas o campo correspondente é exibido. `Ctrl+S` salva sem mudar
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
- Use `Alt+←`/`Alt+→` para alternar entre os provedores habilitados. `Shift+Tab`/`Tab`
  fazem a mesma navegação quando o terminal ou editor intercepta o atalho Alt.
  Também é possível clicar na metade esquerda/direita do título. Com apenas um
  provedor habilitado, a tentativa mostra um aviso e `Ctrl+P` abre as conexões.
  Navegar consulta o catálogo da conexão escolhida e limpa o filtro anterior;
  o modelo ativo só muda após confirmar uma linha.
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
vazia é exibida sem erro. A troca de provedor e a atualização limpam a lista
imediatamente. Se a consulta falhar, o título continua no provedor escolhido,
a lista permanece vazia e aparece uma mensagem de falha; `Ctrl+R` tenta novamente.
Modelos de outro provedor ou de uma consulta anterior não são usados nessa falha.
Provedores sem API de catálogo exibem o catálogo configurado, identificado como tal.
A mudança do modelo ativo é recusada enquanto o agente trabalha; aguarde terminar
ou cancele a execução antes de tentar novamente.

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
OpenRouter e **OpenAI compatible**. `↑↓` escolhe e `Enter` prepara o formulário; nada é salvo ou conectado
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


## Servidor OpenAI compatible e TLS

Em `/settings` → `N`, escolha **OpenAI compatible**, preencha a URL completa
(normalmente terminada em `/v1`) e escolha a autenticação. **Chave direta** permite
colar a chave no campo mascarado; **Variável de ambiente** recebe o nome da variável;
**Sem autenticação** atende servidores que não exigem chave. Esse preset usa
`backend: "local"` para o protocolo genérico, inclusive quando o servidor é remoto.
`Ctrl+T` consulta o catálogo; salvar não executa uma inferência de teste.

Veja [o exemplo de configuração](../examples/openai-compatible.example.json).
Adicione seu perfil a `providerProfiles` no arquivo ativo, ajustando o endereço.
O exemplo fica desativado para não consultar um endereço fictício. Ative-o com
`disabled: false` depois de configurar. Se a API exige autenticação, acrescente
`"apiKeyEnv": "MINHA_CHAVE_GATEWAY"` e defina essa variável no ambiente.

**TLS inseguro** começa desligado. Para um servidor HTTPS com certificado que
você decidiu não validar, habilite esse controle no formulário ou use
`"tlsInsecure": true` no perfil. Ele desativa a verificação do certificado e da
identidade do servidor apenas nessa conexão, tanto no catálogo quanto nas
requisições do modelo. Não altera a validação das demais conexões. O transporte
fica restrito à origem HTTPS configurada; redirects não enviam suas credenciais
a outro servidor. A opção exige URL HTTPS explícita, backend `local` ou `openai`
e API compatível (`chat_completions`, `responses` ou padrão). O protocolo nativo
`ollama` não aceita essa opção.

A [revisão da janela de provedores](provider-ui-review.md) descreve as melhorias
de UX incluídas e as próximas prioridades. Veja também a [validação desta mudança](evidence/2026-10-29-footer-provider-tls-validation.json).


## Informar a API key diretamente

No formulário de conexão, selecione **Autenticação → Chave direta** com `←`/`→`
ou espaço, avance com `Tab`, cole a chave em **Chave da API** e salve com `Ctrl+S`.
A chave fica mascarada durante a edição. Ela é armazenada como `apiKey` no arquivo
ativo, com permissão `0600`; não é criptografada. Isso também funciona ao editar
uma conexão existente. A interface preserva a chave já salva e permite substituí-la.

Também pode configurar o JSON diretamente:

```json
{
  "providerProfiles": {
    "my-gateway": {
      "name": "Meu gateway",
      "backend": "local",
      "baseURL": "https://gateway.example/v1",
      "api": "chat_completions",
      "apiKey": "SUA_CHAVE_AQUI"
    }
  }
}
```

Escolher **Variável de ambiente** mantém apenas `apiKeyEnv` no arquivo, sem copiar
o segredo resolvido. Escolher **Chave direta** grava apenas a chave digitada e remove
a referência anterior. **Sem autenticação** remove ambos os campos. Trocar a opção
não apaga o rascunho dos campos escondidos; só a opção escolhida é aplicada ao salvar.
Não coloque uma configuração com chave literal no controle de versão.


## Excluir uma conexão

Em `/settings` → lista de conexões, destaque o provedor e pressione `D` ou `Delete`
(também pode clicar em **Excluir**). A confirmação começa em **Cancelar**; escolha
**Excluir** com `←`/`→` ou `Tab` e pressione `Enter`. `Esc` cancela sem alterar o arquivo.

A exclusão remove credenciais locais e definições de modelos daquela conexão nas
camadas global e do projeto atual em que estiverem presentes. Campos desconhecidos
fora da conexão excluída e outras conexões são preservados. Arquivos são substituídos individualmente de
forma atômica; se uma escrita posterior falhar, a operação tenta restaurar os
arquivos anteriores antes de retornar o erro. A configuração em memória só muda
após todas as escritas terem sucesso.

Conexões em uso por `coder`, `task`, `title` ou `summarizer` não podem ser excluídas.
O aviso identifica as funções; escolha modelos de outra conexão para elas no
seletor (`Ctrl+A` alterna a função) e tente novamente. Essa proteção também verifica
referências salvas nas duas camadas. A exclusão fica bloqueada durante uma execução.
Conversas e histórico de mensagens não são apagados.

Backends legados em `providers` mantêm apenas um marcador `disabled: true`, sem
chave, URL ou outros dados, para impedir recriação automática por variável de
ambiente. Esse marcador não aparece na lista de conexões. Perfis nomeados em
`providerProfiles` são removidos do JSON.

## Formatos de catálogo compatível

Além dos formatos nativos dos provedores, a listagem aceita arrays em `data`,
`models`, `items` ou `result`, um array na raiz e wrappers de objeto em `data` ou
`result`. Cada registro pode trazer `id`, `model`, `model_id`, `model_name` ou
`name`; listas de IDs em strings também são aceitas. O ID externo do gateway é
preservado: `openai.id` não substitui um alias publicado pelo servidor.

Os limites podem ser números inteiros ou strings numéricas, como `"131072"`.
A busca considera os campos do registro e, depois, `openai`, `limits`, `metadata`,
`model_info`, `info` e `info.meta`, nessa ordem.

| Informação | Campos reconhecidos |
| --- | --- |
| Contexto | `loaded_context_length`, `context_length`, `context_window`, `max_model_len`, `max_context_length`, `contextLength`, `contextWindow`, `maxModelLen`, `maxContextLength`, `context_size`, `contextSize`, `n_ctx`, `max_seq_len`, `max_sequence_length`, `max_context_tokens` |
| Entrada | `max_input_tokens`, `input_token_limit`, `inputTokenLimit`, `maxInputTokens` |
| Saída | `max_output_tokens`, `output_token_limit`, `outputTokenLimit`, `maxOutputTokens`, `max_completion_tokens`, `max_tokens` |

O limite de saída não é usado para deduzir o contexto. Na ausência de uma janela
explícita, o limite de entrada pode servir como referência conservadora. Limites
carregados do LM Studio e metadados nativos do Ollama mantêm suas regras de
precedência. Booleans, frações, valores negativos, overflow e unidades ambíguas
como `"128k"` são ignorados; na ausência de um limite válido, a interface identifica
o valor alternativo como estimativa.

Por exemplo, `max_model_len: 131072`, inclusive dentro de `openai`, aparece como
**128k (informado pelo provedor)**. Para usar descoberta automática, deixe
`contextWindow` ausente ou em `0`; um valor positivo configurado pelo usuário
continua tendo precedência. A atualização não fixa o limite descoberto no JSON.

Flags de interface em `info.meta.capabilities`, como `builtin_tools`, `terminal`
ou `vision`, não comprovam capacidades do modelo nem concedem permissão às
ferramentas do Opcode. A normalização mantém os limites de tamanho das respostas,
até 5000 modelos e até três níveis de envelope, com paginação limitada.

## Capacidade anunciada e contexto ativo

O seletor mostra a capacidade recebida do catálogo. A requisição pode usar uma
janela menor por configuração manual ou por uma redução aprendida após erro real
de contexto/memória. O rodapé consulta a janela do agente ativo antes da primeira
mensagem; quando existe um orçamento de requisição, mostra o limite desse pedido.
Essas informações ficam em memória, sem consultar o catálogo a cada renderização.

`/context` identifica a origem. Uma redução reaplicada do cache aparece como
`learned after provider context/memory failure`, inclusive quando o agente é
recriado. Ela não deve ser atribuída a `provider` ou `fallback`. Esse limite
aprendido pode durar até 24 horas. `/recalibrate` permite removê-lo explicitamente
e preparar novamente o provedor; a geração seguinte ainda pode ser reduzida se o
servidor voltar a rejeitar a janela. O comando não repete ferramentas.

Omitir o contexto no JSON para manter descoberta automática não elimina os
metadados selecionados em memória. O teste de regressão acompanha a descoberta de
131072 tokens, a seleção, a persistência automática e o orçamento da primeira
requisição. Outro cenário verifica a aplicação intencional de um limite aprendido
de 4096 e a apresentação correta da sua origem.
