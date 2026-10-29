# Perguntas ao usuário e fontes do projeto

## Recuperação do formato de opções

`ask_user_question` e `ask_user_questions` continuam anunciando `choices` como
lista JSON. Se o modelo enviar esse campo como uma string contendo uma lista
JSON de strings, a aplicação decodifica uma única camada antes da validação:

```json
{"question":"Qual área revisar?","choices":"[\"UI\",\"Contexto\"]"}
```

Esse formato tem o mesmo resultado de `"choices": ["UI", "Contexto"]`.
A conversão não gera uma chamada adicional ao modelo. O serviço interativo
continua aguardando a resposta humana real; opções não são respostas nem
autorizações. A mesma conversão atende aos itens de uma pergunta agrupada.

O documento tem limite de 32 KiB. Permanecem os limites de cinco perguntas,
cinco opções por pergunta, 300 bytes por opção, 500 bytes por pergunta e 1.000
bytes de explicação. CSV, prosa, objetos, números, listas aninhadas, opções
vazias e codificações recursivas são recusados. Os erros de tipo indicam o campo
e um exemplo fixo, sem reproduzir o payload. Outros campos não são convertidos.
IDs, tipos e revisão de respostas agrupadas seguem a validação anterior.

## Buscar o assunto, não apenas o manifesto

Perguntas genéricas sobre o projeto recebem arquivos básicos existentes, como
README, manifesto e entrada. Um arquivo explicitamente nomeado no pedido tem
prioridade sobre esses arquivos genéricos. Para perguntas identificadas por assunto —
linguagens, arquitetura, permissões ou controle de contexto — o agente usa a
busca lexical existente com até dois resultados e depois lê trechos numerados
dos arquivos encontrados. Cada leitura tem até 60 linhas e 2.400 bytes; a
preparação inteira respeita o orçamento de ferramentas. Sem índice ou resultado
utilizável, permanece o caminho básico. Não há classificador LLM adicional,
embedding remoto nem inclusão recursiva do diretório no prompt.

Essas rotas são heurísticas em português/inglês, não um entendimento semântico
universal. O índice conserva suas exclusões, limites, pausa e verificação de
hashes. Seus previews são navegação; apenas as leituras reais contam como fonte.
O modelo pode buscar e ler mais páginas dentro dos controles existentes.

No próprio checkout do Opcode, identificado por um manifesto local regular e
limitado, a pergunta de linguagens recebe o resumo do inventário e a busca pela
definição `localParsers`. Esse vínculo é apenas roteamento; não concede permissão.
Comentários ou links no manifesto não estabelecem a identidade.
O resumo de `get_language_support` aceita `summary: true` com paginação e retorna
`profile_count` e nomes, sem varrer o projeto nem procurar executáveis.
`profile_count` também aparece no inventário completo; `total` desse inventário
pode incluir tipos de arquivo detectados sem parser e não deve substituí-lo.

Perguntas sobre linguagens também disponibilizam `get_language_support`. Esse
inventário descreve as capacidades das ferramentas do Opcode e a configuração
LSP. Ele não comprova quais linguagens um projeto de terceiros suporta.
A linguagem usada para implementar um projeto tampouco comprova suas capacidades.

## Citações e reaproveitamento

Perguntas reconhecidas sobre fatos do projeto exigem referências verificáveis
em Perguntar, Planejar e Executar, incluindo formulações genéricas como
“o que pode me falar do projeto atual?” e “quais linguagens suporta?”.
A resposta precisa citar ao menos uma das fontes selecionadas para aquele
assunto; numa apresentação genérica, a primeira fonte básica disponível é
prioritária, normalmente o README. Referências adicionais continuam sujeitas à
verificação de arquivo, hash atual e intervalo efetivamente entregue.

Sem leitura verificável, o agente solicita recuperação em vez de aceitar uma
afirmação sobre o projeto. Após uma correção de citação malsucedida, o turno
encerra com erro preservando as evidências; não inventa uma referência.
As exceções explícitas de estilo solicitadas pelo usuário permanecem respeitadas.
O gate verifica proveniência e o vínculo com a fonte selecionada; não certifica
a interpretação de cada afirmação. As avaliações de conteúdo permanecem necessárias.

Uma leitura automática anterior pode ser reaproveitada somente quando seu
resultado visível e recibo continuam completos e o hash do arquivo é revalidado
dentro do workspace e da política de caminhos. Arquivo alterado, fonte sensível,
link externo, resultado abreviado ou ausência de recibo impedem o reaproveitamento.
Um pedido explícito de chamada da ferramenta exige uma chamada nova.
Fontes reutilizadas não contam como ferramentas executadas nos critérios ou nas
contagens da avaliação.

A fonte revalidada reaparece no turno atual. A projeção enviada ao provedor
abrevia somente cópias antigas idênticas, preservando pares de ferramentas,
argumentos, estado privado do provedor, instruções humanas e o histórico salvo.
A revalidação ainda faz leitura para calcular o hash; isso não é um cache sem I/O
nem uma promessa de latência menor em todo caso.

## O que o processo mostra

O resumo distingue leituras/buscas de contexto, fontes reutilizadas, chamadas de
terminal, outras ferramentas e reparos. `Ctrl+B` continua abrindo os detalhes.
Essas contagens representam operações da aplicação; não são contagens de
requisições ao modelo nem prova de que a resposta interpretou as fontes corretamente.
A classificação usa metadados da aplicação, que são removidos de chamadas
recebidas do provedor. Conversas antigas sem esses metadados mostram ferramentas
genéricas, sem adivinhar sua origem pelo texto ou pelo identificador.
