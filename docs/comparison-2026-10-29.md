# Comparação após as nove melhorias — 29/10/2026

Referências: Opcode desta entrega, Codaro local `b54a686` (27/10), aplicação
Python principal em `src/codaro`, e [Crush v0.98.2](https://github.com/charmbracelet/crush/releases/tag/v0.98.2)
(`d6329fbd`, 23/10). O experimento Go separado do Codaro não é tratado como a
aplicação Python completa. A comparação combina inspeção de código/documentação
com as provas explicitamente indicadas abaixo; ausência de observação não prova
ausência de recurso.

## O que mudou no Opcode

| Área | Entrega | Relação com Codaro/Crush |
| --- | --- | --- |
| Avaliação | Manifesto único, hash de fixtures/critérios, condições solicitadas/observadas e recusa de ranking incompleto | Permite comparar sem converter protocolos ou inventar recibos para os concorrentes |
| Modelo pequeno | Título local sem inferência auxiliar quando usa o mesmo modelo; métricas de preparação, reutilização, chamadas, reparos e TTFT | Ataca trabalho auxiliar desnecessário; ainda exige medir os casos, não presume ganho geral |
| Conversa | Busca local e seleção por teclado, salto, cópia e rascunho preservado | Aproxima o fluxo de interfaces com navegação e edição mais completas; atalhos personalizados e PTY mantêm prioridade |
| Conexões | Salvar/editar abre o catálogo correto e permite voltar; contexto anunciado separado do efetivo | Reduz a ambiguidade que existia no fluxo do Opcode; catálogo não comprova qualidade do modelo |
| Tok/s | Duas amostras, primeiro bloco excluído, tokenizador compatível limitado ou bytes/4; medição final do provedor | Adota o cuidado de amostragem já visto no Codaro, sem chamar a estimativa de medição exata |
| Credenciais | Cofre opcional, referência no JSON e rollback fora da UI; direto/ambiente continuam | Aproxima a opção de keyring do Codaro. Login/OAuth e armazenamentos dos outros produtos não são presumidos equivalentes |
| Linguagens | Matriz gerada do runtime, fixtures de declarações/índice e detecção de drift | Compara unidades: Opcode tem 42 perfis exercitados; Codaro reconhece 81 nomes e possui 36 linguagens de fixtures; LSP é uma capacidade separada |
| Distribuição | CI selecionado em três sistemas, smoke isolado, ZIP Windows reproduzível | Amplia a entrega; compilação cruzada não comprova execução nativa nem resolve PTY Windows |
| Pesquisa | Busca web opcional, limitada e sujeita à aprovação de rede, sem instalar dependência adicional | Acrescenta uma categoria de ferramenta presente no Crush; snippets não substituem leitura nem recibos do repositório |

## Diferenças que continuam relevantes

**Opcode** concentra controles explícitos de contexto, recibos de fonte por
linhas/hash, validação de citações e edição, terminal com PTY Unix e instruções
durante execução. A seleção/busca e a navegação de conexões foram comprovadas no
binário em PTY. As proteções podem exigir mais leituras e reparos; menor número
de chamadas, por si só, não demonstra maior correção.

**Codaro** mantém uma aplicação Python/Textual própria, índice SQLite/FTS com
recuperação lexical e recursos semânticos opcionais, além de keyring opcional e
cuidados de amostragem de geração. Os contadores de linguagens não demonstram
semântica: reconhecer uma extensão, extrair declarações e resolver referências
com LSP são capacidades distintas. Esta entrega não modificou o Codaro.

**Crush** oferece troca de modelos na sessão, LSP, MCP e um ecossistema de
interface, configuração e distribuição próprio. Sua versão consultada usa
`crushrc` Bash como configuração principal, mantendo JSON legado. O Opcode
mantém JSON declarativo. Consulte o [README da versão fixada](https://github.com/charmbracelet/crush/blob/v0.98.2/README.md).
Não foi copiado código do Crush nem atribuída a ele uma nota por recursos que o
adapter não consegue observar.

## Avaliação real compartilhada

O experimento completo foi executado uma única vez:
seis casos originais, duas repetições por aplicação, critérios preservados,
`llama3.1:8b`, protocolo nativo Ollama, janela 16384, saída 2048 e temperatura 0.
Execução é sequencial em fixtures e HOME descartáveis; comandos/edições exigem
`--allow-execution` e continuam sob as aprovações/sandbox do runner.

| Aplicação | Casos aprovados | Reprovados | Indisponíveis | Critérios aprovados/observados | Tempo observado |
| --- | ---: | ---: | ---: | ---: | ---: |
| Opcode | 12 | 0 | 0 | 40/40 | 95,373 s |
| Codaro | 7 | 3 | 2 | 29/34 | 116,458 s |
| Crush | 0 | 0 | 12 | 0/0 | sem inferência |

O Codaro falhou nas duas tentativas de edição e na chamada de leitura de arquivo
ausente da primeira tentativa. Os dois casos de validação ficaram indisponíveis:
o adapter não possui binding exato para os checks nomeados; não os substituímos
por outra verificação. Existem seis critérios indisponíveis no Codaro e quarenta
no Crush. O adapter Crush não instrumenta o protocolo nativo escolhido nem os
casos de execução; sua indisponibilidade **não demonstra falha do produto**.

O comparador devolveu `comparable_conditions:false` e `rankable:false`, preservando
os campos de amostragem não observados nas execuções indisponíveis. Não há ranking
geral nem alegação de que o Opcode ficou causalmente mais rápido. A ordem foi
Opcode → Codaro → Crush, após o aceite PTY ter aquecido o modelo; caches/estado
inicial não foram controlados. Os 1,153 s do adapter Crush são apenas overhead.

Relatórios: [Opcode](evidence/2026-10-29-nine-opcode.json),
[Codaro](evidence/2026-10-29-nine-codaro.json),
[Crush](evidence/2026-10-29-nine-crush.json),
[comparação](evidence/2026-10-29-nine-comparison.json) e
[experimento](evidence/2026-10-29-nine-experiment.json).
As evidências publicadas contêm critérios/resultados e condições sanitizados,
sem respostas integrais, configuração privada ou credenciais.

Suíte SHA-256: `08b9aff679c7f9743feb5019658ffde63e9a9f679b566037edc016054860db24`.
Manifesto: `b99e3f53b9b1bf5643993fbe31b09fd87746831d89309c2d21a385f3eedbbea8`.
Binário testado: `0b2473340a37eb11a5f1a42615d0a32caaa65eed39180d83485b020db32fa885`,
worktree Opcode baseado em `6e3fcbd` com as alterações desta entrega. Os 12 pares
caso/tentativa e os hashes de critérios foram conferidos por aplicação.
Uma correção posterior restrita ao descritor PTY Unix foi validada separadamente
com race e o fluxo real do Ollama; os fixtures desta comparação não solicitam
PTY. Os resultados e o hash originais foram preservados, sem repetir o benchmark
para melhorar o placar. Veja o [registro de validação](validation.md).

No Opcode foram observadas 31 chamadas de provedor, 16 chamadas de ferramentas
do loop e cinco reparos. A preparação tem contadores separados; esses números
não representam toda operação de disco nem qualidade semântica universal.

## Limites da entrega

- A suíte Go completa com race, vet, build sem CGO, smoke Linux e fluxo PTY real
  com Ollama foram executados localmente. Os backends de cofre foram exercitados
  com doubles; nenhum cofre pessoal foi aberto nos testes.
- macOS/Windows tiveram compilação cruzada e ganham jobs nativos de CI; isso não
  equivale a execução local nesses sistemas. O executor padrão exige Bubblewrap
  Linux; o PTY Windows continua indisponível, com recusa explícita.
- A busca web tem testes HTTP controlados. Disponibilidade e formato do serviço
  público podem mudar; falhas não viram resultados fictícios.
- A ordem dos aplicativos, cache, carga do host, prompts e políticas distintas
  impedem atribuir diferenças de duração a uma alteração isolada. Duas repetições
  não permitem declarar superioridade geral.

As próximas lacunas verificáveis são PTY/isolamento nativo fora do Linux e maior
observabilidade dos adapters externos, além de amostras de avaliação mais amplas.
Esses pontos não são apresentados como suporte já entregue.
