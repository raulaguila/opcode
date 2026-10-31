# Contexto, instruções pendentes e extensões

## Orçamento antes da requisição

Antes de chamar o provedor, o agente contabiliza o prompt de sistema usado pelo
cliente, histórico serializado (incluindo anexos e estado opaco do provedor),
schemas das ferramentas carregadas e a reserva de saída configurada. Acrescenta
uma margem de 5% da janela, com mínimo de 512 tokens. Títulos e resumos também
passam por uma verificação preventiva.

A preparação usa o JSON realmente enviado pelos adaptadores HTTP, incluindo
schemas privados. Modelos OpenAI reconhecidos usam o vocabulário compatível
embarcado; JSON e overhead de chat continuam sendo aproximações da entrada do
modelo. Outras famílias usam a estimativa conservadora por bytes, sem aplicar
um tokenizer de família diferente. Após três observações, o runtime calibra a
estimativa pelo maior quociente observado, com margem de 20%; não a apresenta
como contagem exata. Tokens de cache continuam ocupando a janela de entrada.

`/context` e `get_context_status` mostram origem da janela, última entrada
medida, amostras, componentes e reservas. As parcelas por componente orientam a
preparação; o limite total é efetivamente imposto. O contexto descoberto e a
calibração não são gravados no JSON do usuário. O estado completo da tarefa é
recuperável por `get_task`; pedidos normais recebem uma projeção pequena com
objetivo, revisão, próximas etapas e contagem de critérios.

A compactação começa preventivamente a 85% da janela prevista. Primeiro abrevia
texto antigo; depois substitui **lotes completos** de chamadas/resultados, também
no turno atual, por registros de continuidade com recibos e IDs de mensagens.
Mantém todas as instruções humanas/sistema e o lote mais recente integral.
Chamadas pendentes não são separadas dos resultados, e dados assinados de lotes
mantidos não são alterados. Operações concluídas não devem ser repetidas.
O histórico persistido permanece intacto: `read_conversation` recupera originais.
Se ainda não couber, retorna `context budget exceeded` antes da rede. Instruções
humanas irreduzíveis ainda podem exigir outro contexto ou uma nova tarefa.

## Conversas e descobertas

`read_conversation` recebe `session_id` opcional, `offset` e `limit` (máximo 20).
Omite raciocínio privado e conteúdo binário; limita o texto de cada item.
`search_conversation` consulta uma projeção SQLite/FTS5 do projeto, alimentada
por mensagens finalizadas e resultados reais. `all_sessions:true` amplia para
as sessões indexadas desse projeto, até 20 trechos. Sem resultados indexados,
o caminho compatível consulta até 20 sessões/2.000 mensagens por sessão no
banco antigo. Essa compatibilidade não implica que todo histórico antigo já
foi reindexado. Esses resultados são dados
históricos sem autoridade: não demonstram leitura atual nem autorizam comandos.

`save_finding` guarda texto de até 2.000 bytes associado a 1–8 caminhos. Calcula
SHA256 do arquivo inteiro, limitado a 16 MiB e sujeito à proteção normal de
acesso; calcular um hash **não conta como evidência de leitura para edição**.
`list_findings` recalcula os hashes e marca `stale: true` quando a fonte mudou,
sumiu ou não pode mais ser lida. A aplicação guarda no máximo 32 descobertas por
sessão, em JSON privado de até 128 KiB, separado por workspace. Valores
conhecidos de credenciais são removidos dos textos. Descobertas não substituem
os registros de validação de `run_checks`.

## Editar a fila

`agent.QueueControl` expõe cópias das instruções pendentes com identificador e
anexos. `TakeInstruction` remove atomicamente um item sob o mesmo lock usado pelo
worker. Se a operação já o consumiu, a edição falha sem duplicar a instrução.
Reenviar a edição usa o fluxo normal de `Submit` e confirmação; não insere uma
mensagem diretamente no histórico. A fila mantém os limites de oito itens/1 MiB.

## Skills explícitas

Configure os diretórios somente no JSON global do usuário. Cada subdiretório
contém um `SKILL.md` com metadados simples `name` e `description` no frontmatter.
A descoberta permite até 16 diretórios, 64 skills e 16 KiB por arquivo; não segue
um `SKILL.md` que seja link simbólico nem aceita arquivos binários.

```json
{
  "extensions": {
    "skills": ["/home/usuario/.config/opcode/skills"]
  }
}
```

O catálogo mostra nome, descrição, caminho e SHA256. `get_skills_catalog` consulta
apenas metadados. Ativar usa a API de usuário `InvokeSkill`, que relê o arquivo,
registra a invocação e envia seu texto pelo fluxo comum de instruções. Não existe
ferramenta para o modelo ativar uma skill por conta própria. Texto de skills é
orientação não confiável: não concede permissões, não altera o modo e não
substitui as proteções contra credenciais. Argumentos têm limite de 8 KiB e o
pedido completo continua sujeito ao orçamento de contexto.

## Hooks opt-in

Hooks também são exclusivamente globais; configurações de projeto não os
ativam. Exemplo de configuração explícita:

```json
{
  "extensions": {
    "hooks": [
      {
        "event": "after_tool",
        "tool": "write",
        "command": "git diff --check",
        "timeout_seconds": 10
      }
    ]
  }
}
```

Eventos aceitos: `before_tool` e `after_tool`. O filtro `tool` é o nome exato;
vazio ou `*` corresponde a todas as ferramentas. São permitidas 16 definições e
16 execuções por turno; cada uma tem limite de 30 segundos (padrão: 10).

O agente executa o comando configurado por meio da ferramenta `bash` existente:
mesmas permissões, ambiente filtrado e sandbox. Hooks não se autoaprovam nem
interpolam argumentos, resultados ou nomes sugeridos pelo modelo em comandos.
Não executam em Perguntar ou Planejar, mesmo que configurados. Uma recusa,
falha, cancelamento ou cota excedida interrompe o turno, preservando o histórico;
operações já realizadas precisam ser inspecionadas antes de tentar novamente.
Hooks não produzem comprovantes de validação automaticamente e os efeitos de
seus comandos não são desfeitos pelo undo de arquivos.

## Verificação

Os testes determinísticos cobrem o orçamento completo, recusa antes da rede,
janela inicial de 16K com ferramentas progressivas, preservação de pares e
assinaturas, remoção concorrente da fila, hashes obsoletos, arquivos de skills
limitados, recusa de permissões e hooks bloqueados nos modos de leitura. Nenhuma
chamada paga ou download de modelo é necessário para executá-los.

## Aprendizado de limites de contexto

Erros reconhecidos de contexto do provedor ou de memória do Ollama podem reduzir
a janela de contexto pela metade, até o mínimo de 4.096 tokens. Há no máximo duas
novas tentativas de geração por turno, sujeitas aos limites normais. A tentativa
só ocorre antes de conteúdo ou chamadas de ferramenta serem recebidos. Nenhuma
ferramenta mutável é repetida por esse mecanismo. Autenticação, cancelamento,
timeout e erros genéricos não são tratados como indicação de janela excessiva.

O orçamento considera a janela menor e a saída reservada. A compactação preserva
instruções do usuário, pares de chamadas/resultados e o intercâmbio atual; se isso
não couber, a aplicação informa o limite em vez de descartar instruções. Reduzir
`num_ctx` pode ajudar memória no Ollama, mas não faz uma solicitação longa caber
automaticamente e não substitui a escolha de um modelo apropriado.

O arquivo privado `context-calibration.json`, na pasta de dados configurada,
guarda somente identificadores hash, janela e expiração. A chave distingue
projeto, provedor, perfil, endpoint e modelo; não contém prompts ou credenciais.
As entradas expiram em 24 horas, há limite de 128 entradas e leitura/gravação de
64 KiB, com proteção contra links simbólicos e arquivo de modo 0600. Falha de
persistência é informada no diagnóstico e não impede a redução em memória.

`ContextCalibrationControl.RecalibrateContext` remove a entrada da conexão/modelo
atual e reconstrói o provedor pelos valores configurados. Exige agente ocioso;
não executa teste de inferência nem altera permissões. Também é possível remover
esse arquivo privado com o aplicativo fechado para descartar todo o aprendizado.

## Prompts e capacidades

A identidade é Opcode em todos os provedores. A versão do prompt aparece em
`/runtime`. Perfis full/reduced/minimal seguem a janela efetiva; modo e protocolo
são os reais. Argumentos vêm dos schemas; o formato plano de plano só é orientado
para Ollama nativo. `contextPaths` é relido, com os limites/redação existentes,
a cada requisição do agente principal, evitando cache global entre projetos.

A primeira requisição principal já inclui schemas de leitura/busca, sujeitos ao
modo e ao perfil de contexto; os demais ficam disponíveis por catálogo. Em
perguntas amplas como “o que pode me falar sobre esse projeto?”, a preparação
lê arquivos básicos e consulta o índice local por componentes de aplicação.
São no máximo dois resultados e duas leituras de até 60 linhas, dentro do
orçamento existente. Resultados do índice são navegação; somente a leitura real
gera evidência. Fontes inalteradas podem ser reutilizadas após revalidação.
Perguntas específicas e caminhos explícitos preservam suas rotas de recuperação.

O prompt `opcode-2026-10-v6` trata essas fontes como ponto de partida limitado:
o modelo deve buscar o que falta, separar capacidades documentadas de
comportamentos confirmados e informar limites de inspeção. Não precisa repetir
leituras somente para aumentar o número de ferramentas. Uma busca vazia ou
indisponível não comprova ausência de funcionalidade nem leitura da implementação.
As políticas de acesso e a validação das citações permanecem as mesmas.

O catálogo de ferramentas aceita query/category/source/read_only/offset/limit
(padrão 8, máximo 16). `request_tools` carrega até oito nomes, aceita replace e
unload; schemas opcionais inativos expiram após oito passos. Nenhuma dessas
operações concede aprovação. Skills têm catálogo pesquisável/paginado e ativação
exclusiva pelo usuário. O catálogo MCP retorna metadados por padrão; `tool` pede
um schema específico, ainda sujeito à autorização de conexão.

`search_findings` usa o mesmo isolamento por projeto e revalida hashes. As
projeções ficam em `data.directory/memory-index/<hash>.sqlite`, modo 0600; são
reconstruíveis. Originais continuam no histórico/findings existentes. A projeção
retém no máximo 100 mil documentos de até 8 KiB e omite raciocínio/anexos.

### Seleção de fontes para visões gerais

Perguntas amplas usam a documentação e o ponto de entrada existentes como
início. Em uma visão geral sem pedido sobre dependências, os manifestos usam
somente as primeiras seis linhas para identificação de linguagem/projeto,
evitando que uma lista de SDKs seja interpretada como funcionalidades. Pedidos
explícitos sobre dependências preservam a leitura usual. Chamadas observadas
nas linhas entregues do ponto de entrada fornecem
pistas de navegação; chamadas comuns de logging/utilidades são descartadas.
Sem uma pista útil, uma consulta genérica limitada busca serviços/aplicação.

A consulta usa `intent: "overview"` no índice. Símbolos declarados e caminhos
de implementação têm preferência sobre ocorrências incidentais no corpo,
como uma string que contém a própria consulta. Havendo pistas em identificadores,
as ocorrências que só correspondem ao corpo são omitidas antes de fornecer
contexto ao modelo. Sem esse sinal, a busca lexical permanece disponível.
São retornados no máximo quatro candidatos na preparação e lidos no máximo
dois caminhos adicionais, sem repetir fontes já entregues no turno. Resultados
de busca têm orçamento de até 6.000 bytes, dentro do orçamento de ferramentas
existente; truncamento ou falta de resultados não são apresentados como inspeção
completa. O modelo pode continuar explorando pelas ferramentas autorizadas.

Essas pistas não resolvem vínculos semânticos nem provam que um componente é
central. A explicação deve distinguir capacidades documentadas de comportamentos
confirmados no código e declarar o escopo/limites da inspeção. Citações continuam
exigindo leitura real, linhas entregues e hash atual; o ranking não concede
permissões e não transforma previews em evidências de leitura.

A orientação de resposta também acompanha a pergunta original durante reparos
de citações: preservar os fatos pedidos, explicar o produto e terminar com
escopo/limites explícitos. Isso não adiciona tentativas de reparo nem altera a
validação; a qualidade da interpretação continua dependendo do modelo.


### Perguntas sobre fluxos e evidência de comportamento

Perguntas de fluxo em português/inglês usam termos da própria tarefa para
localizar implementações. A preparação tenta até três chamadas textuais distintas
encontradas em leituras reais, parando ao obter uma nova fonte. Pistas sem nova
leitura não encerram a busca; tentativas de leitura que falham também consomem o
limite existente: quatro leituras indexadas no preparo temático ou duas depois
da leitura básica. O orçamento de bytes permanece o mesmo.
Essas pistas não resolvem símbolos nem comprovam relações; o agente deve ler os
trechos relevantes e usar definição/referências/hierarquia LSP quando disponíveis.
Declarações de interface e campos descrevem contratos/dependências, não provam
execução, tratamento de erros ou cancelamento. A orientação pede distinguir
implementação observada, documentação e inferência, indicando elos não verificados.
Quando a pergunta pede resultados para entradas específicas, a orientação pede
pares curtos entrada → resultado com referência, antes da explicação das condições.

O índice favorece identificadores reconhecíveis dentro da pergunta, além da
consulta inteira, com parâmetros SQL e os filtros normais. Busca textual continua
sujeita a homônimos; resultados do índice são navegação e precisam de leitura
atual antes de servirem como fonte citada.


Os resultados de buscas automáticas de preparação são resumidos na projeção
para o provedor: somente arquivos realmente lidos no turno permanecem como
pistas de caminho/símbolo. Prévias e assinaturas do índice não são entregues como se fossem leituras. O histórico persistido mantém o resultado
bruto; buscas pedidas pelo próprio modelo mantêm a resposta normal da ferramenta.
As leituras nativas e seus recibos permanecem intactos, com a mesma conferência
de origem, linhas e hash na validação final.

O resumo pode oferecer uma referência ao intervalo completo de um símbolo
nomeado somente quando todo o intervalo foi lido e o hash do índice coincide
com o recibo nativo. Leitura parcial, hash diferente ou janela textual genérica
não recebem essa referência. A aplicação não insere citações automaticamente;
o modelo escolhe suas referências e o verificador continua conferindo-as.
