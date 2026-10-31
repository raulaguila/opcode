# Medir desempenho e alocações

As fixtures de componente não consultam APIs reais. Execute com a mesma toolchain,
mesmas entradas e sem testes, builds ou inferência concorrentes durante a medição:

```sh
make bench-performance > /tmp/opcode-after.log
make bench-compare BEFORE=/tmp/opcode-before.log AFTER=/tmp/opcode-after.log
```

O alvo de medição executa os pacotes sequencialmente (`-p 1`). O segundo
comando emite JSON e retorna 1 se a mediana de B/op ou allocs/op de
qualquer caso do baseline crescer mais de 10%. Retorna 2 para logs vazios,
métricas inválidas, caso removido do candidato ou menos de três amostras por
caso nos dois lados. Tempo é informado e revisado, sem um limite fixo entre
máquinas. Casos presentes apenas no candidato também são rejeitados: copie as fixtures
compatíveis para o baseline e preserve as mesmas condições antes de atribuir
um ganho. Não remova casos para fazer o gate passar.

Use exatamente as mesmas fixtures nos dois checkouts; um benchmark adicionado
nesta revisão pode precisar ser copiado para a revisão anterior, desde que use
as mesmas APIs/entradas. Não substitua o trabalho da versão anterior por uma
operação menor. O benchmark de troca de conversa, por exemplo, executa os
comandos retornados e mede a primeira apresentação concluída, incluindo a
versão assíncrona. Preserve os logs brutos e a identificação dos commits.

Fixtures disponíveis:

- Hash do workspace com 200 e 2.000 arquivos pequenos.
- Busca aquecida com watcher ativo, mais edição de um arquivo, alteração de
  política e reconciliação periódica. Abertura do banco e preparação ficam fora
  da região medida nesses casos; os resultados não representam a ferramenta
  completa com verificação de hits.
- Revalidação de citação de um arquivo de 1 MiB, com hash e contagem de linhas.
- Preparação real do contexto com histórico de 160 kB e imagem PNG sem
  compressão de aproximadamente 1 MiB, sem envio ao provedor.
- Projeção repetida de mensagens com redação e confirmação do último registro
  gravado; escrita do histórico autoritativo fica fora dos dois lados.
- Eventos do chat, resposta crescente, histórico maior que os dois caches,
  troca de conversa e Markdown com múltiplos blocos e larguras.

`B/op` é o total de bytes alocados por operação; não é a memória residente do
processo. Caches estáveis, atualizações reais, construção inicial e inferência
são cargas diferentes. As fixtures de crescimento usam ciclos limitados com
entradas iguais nos dois lados para não comparar tamanhos diferentes após a
calibração do runner. O caso de overflow é propositalmente grande; uma melhora
nele não é uma promessa de ganho igual em conversas pequenas.

As regressões funcionais continuam sendo o gate de integridade. Incluem
expiração entre lotes, falha de watcher, rotação de credenciais, quotas,
rollback, cancelamento, substituição do armazenamento privado, limites de
citação, paginação visual, seleção, busca, posições dos links e paridade do
Markdown. Reduzir alocações não autoriza remover essas verificações.


A comparação do CI roda em `.github/workflows/performance.yml`. O baseline é o
commit base do PR ou o `before` do push; execução manual/primeiro push usa o pai
verificado de HEAD. Um runner separado compila as duas revisões antes de medir,
usa a mesma linha Go 1.27, copia uma lista explícita de fixtures compatíveis e
mede os pacotes/revisões sequencialmente. Falha de compilação, fixture ausente,
caso sem par ou regressão de alocações não vira skip. Logs, revisões e comparação
JSON são preservados como artifacts. O tempo continua sendo informado, sem
limite fixo de latência entre máquinas.

As fixtures incluem também a ferramenta completa de busca (abertura/schema,
consulta, autorização/hash dos hits e JSON), a abertura de conversa com SQLite
real e todos os comandos assíncronos concluídos, resposta com parágrafos estáveis,
e tokenizer reconhecido com histórico estável e histórico alterado. O caso estável
mede reuso de contagem exata; o caso alterado continua executando a tokenização.
A busca completa usa uma cobertura sintética conhecida, em workspace imutável,
para excluir a latência do watcher sem sleeps ou concorrência na região medida.

A amostragem padrão usa 100 operações por caso e três amostras por revisão.
Rodadas muito curtas podem ficar dominadas pelo aquecimento/descarte de buffers
internos de JSON; aumentar a região medida não muda o limite de regressão de 10%.
Histórico crescente continua usando o mesmo ciclo limitado nos dois lados.

## Atualização das dependências visuais — 30/10/2026

Comparação final com `5eb3ff5`, Go 1.27.3, `GOMAXPROCS=2`, mesmo tema Opcode,
fundo escuro e TrueColor explicitamente definidos nos dois lados. Matriz de
32 casos, seis pacotes, 100 operações e três amostras por revisão, sem inferência
ou testes concorrentes. Fixtures idênticas após normalizar somente imports Charm.
A base preservada da primeira rodada foi comparada novamente com o candidato
após os ajustes finais de seleção sem cores e resets de atributos do Markdown.

| Operação | Tempo | Bytes alocados | Quantidade de alocações |
| --- | --- | --- | --- |
| Markdown com múltiplos blocos | −37,2% | −32,5% | −47,1% |
| Chat, último item invalidado | −7,0% | −10,1% | −37,1% |
| Chat durante streaming | −14,3% | −25,6% | −35,6% |
| Busca aquecida, 2.000 arquivos | −30,4% | −11,6% | −60,2% |
| Reconciliação periódica do índice | −3,1% | −1,7% | −6,4% |

O chat aquecido permaneceu em zero bytes/zero alocações. O gate existente de
10% passou nos **32 casos**, sem regressões de alocações acima desse limite.
Esses resultados usam o commit **oficial** do Lip Gloss que agrupa escritas;
a primeira medição da release 2.0.6 aumentava objetos alocados. Motivo do pin
e procedimento para trocar por release em
[Dependências](dependencies.md#lip-gloss-substituir-o-commit-por-uma-versão-publicada).

Há regressões de tempo na matriz final: overflow do cache do chat (+25,9%),
resposta com blocos crescentes (+19,7%), resposta crescente com 1.000 mensagens
(+11,8%) e tokenização com histórico alterado (+20,2%). O overflow alocou 3,6%
mais bytes, dentro do limite existente; a tokenização manteve suas alocações.
Tempo é informado e revisado, sem limiar automático; os critérios não mudaram.
As amostras locais não medem velocidade do modelo nem garantem melhora em todas
as cargas. Casos crescentes e tokenização merecem investigação posterior.

A comparação anterior de nove casos em cinco pares alternados de 300 ms ficou
preservada na evidência como rodada histórica, antes dos últimos ajustes de
apresentação. A tabela acima usa a matriz final, sem misturar as duas amostragens.
Ver [evidência](evidence/2026-10-30-dependency-upgrade-validation.json).

O workflow permite comparar com uma base Charm v1: copia as mesmas fixtures,
verifica sua igualdade e normaliza somente os dois imports visuais no baseline.
Os corpos dos benchmarks, contagem de amostras e critérios não são relaxados.

## Menus e fluxo de conversa — 30/10/2026

Comparação com `abdf380`, já usando Charm v2 nos dois lados: Go 1.27.3,
`GOMAXPROCS=2`, tema Opcode escuro e TrueColor. As 14 fixtures são idênticas,
sem adaptação de imports. Foram medidas 100 operações, três amostras por revisão,
em seis pacotes, sem inferência ou testes concorrentes.

| Operação | Tempo | Bytes alocados | Quantidade de alocações |
| --- | --- | --- | --- |
| Markdown com múltiplos blocos | −41,26% | −31,16% | −49,38% |
| Chat com blocos crescentes | −75,45% | −80,07% | −79,59% |
| Chat durante streaming | −41,82% | −38,01% | −35,62% |
| Chat, último item invalidado | −20,87% | −19,66% | −16,13% |
| Overflow do cache do chat | −34,60% | −48,35% | −64,73% |

O chat aquecido manteve zero bytes/zero alocações. Os **32 casos** passaram no
gate original de 10% para bytes e alocações. A renderização de prosa agora quebra
as linhas uma única vez, mantendo recuos, largura e referências originais.

Há duas regressões de tempo superiores a 10% nesta amostragem: preparação de
contexto (`prepare_only`, +11,58%) e projeção de conversa (+15,40%). Suas contagens
de alocações não mudaram; bytes variaram +0,002% e −0,013%, respectivamente.
As medições locais não estabelecem causalidade nem garantem melhora em todas as
cargas. Tempo continua informado sem alterar os critérios do gate.

Amostras brutas, hashes das fixtures e resultados completos estão na
[evidência desta rodada](evidence/2026-10-30-ui-menu-refinement-validation.json).

## Blocos de código, detalhes e legendas — 30/10/2026

Comparação com `a9bfb16`, Go 1.27.3, `GOMAXPROCS=2`, tema Opcode escuro e
TrueColor. Foram preservadas as 14 fixtures byte a byte, em seis pacotes,
100 operações por caso e três amostras por revisão. A rodada final inclui as
legendas do chat; uma rodada anterior, antes desse ajuste, ficou separada.

| Operação | Tempo | Bytes alocados | Quantidade de alocações |
| --- | --- | --- | --- |
| Markdown com múltiplos blocos | +10,13% | +7,27% | +3,73% |
| Chat durante streaming | +0,53% | −1,73% | −3,19% |
| Chat, último item invalidado | +0,44% | −2,36% | −3,07% |

O Markdown agora desenha superfície, margem e cabeçalho próprios nos blocos de
código; essa apresentação tem custo. A contagem passou de 15.931 para 16.525
alocações por operação na fixture. Os **32 casos** passaram no gate original de
10% para bytes/alocações, sem alterar limiares. O chat aquecido continua com
zero bytes/zero alocações. O tempo do Markdown merece acompanhamento; estas
amostras locais não garantem a mesma variação em outras máquinas/conversas.

Os benchmarks medem componentes e não a inferência ou a qualidade do modelo.
A exploração de uma pergunta ampla pode executar uma busca e até duas leituras
extras da implementação; seus limites e reutilização ficam descritos em
[Contexto](context-and-extensions.md#prompts-e-capacidades). Amostras e condições
na [evidência desta rodada](evidence/2026-10-30-response-polish-validation.json).

## Custo do Markdown e fundos das mensagens — 30/10/2026

Comparação com `c60aeb8`, Go 1.27.3, `GOMAXPROCS=2`, tema Opcode escuro e
TrueColor nos dois lados. As 14 fixtures permaneceram idênticas byte a byte:
seis pacotes, 100 operações por caso e três amostras por revisão, sem inferência
ou testes concorrentes. A tabela usa as medianas da matriz final.

| Operação | Tempo | Bytes alocados | Quantidade de alocações |
| --- | --- | --- | --- |
| Markdown com múltiplos blocos | −10,96% | −5,83% | −9,21% |
| Chat com blocos crescentes | −16,32% | −11,78% | −16,56% |
| Chat durante streaming | −5,76% | −22,93% | −23,08% |
| Chat, último item invalidado | −6,31% | −23,00% | −23,28% |
| Carga SQLite com 1.000 mensagens | −13,34% | −28,29% | −27,86% |

O Markdown passou de 16.511 para 14.990 alocações/op, mantendo cabeçalho,
superfície e margem dos blocos de código. As tabelas imutáveis do parser são
compartilhadas; contexto, referências e AST continuam separados por chamada.
O scanner de cores usa um buffer, preserva grupos RGB e restaura o fundo após
resets de prosa. Código curto e mensagens já diagramadas evitam trabalho de
preenchimento repetido, com comparação exata das células e fallback nativo para
entradas fora desses casos. Não foi adicionado cache de fontes ou ASTs.

O chat aquecido mantém zero bytes/zero alocações. Os **32 casos** passaram no
gate original de 10% para bytes e alocações, sem mudar fixtures ou critérios.
Há variações de tempo superiores a 10% em projeção da conversa (+26,50%),
reconciliação periódica do índice (+10,93%) e atualização de política (+11,60%);
esses caminhos não foram modificados e suas contagens de alocações não mudaram.
As amostras locais não demonstram a causa dessas variações nem garantem ganhos
em todas as cargas. O tempo é informado, sem mudar o limiar do gate.

Amostras brutas, condições e hashes estão na
[evidência desta rodada](evidence/2026-10-30-markdown-cost-validation.json).
