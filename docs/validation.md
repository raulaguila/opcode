# Validação do Opcode

Os testes Go determinísticos verificam protocolo de provedores, permissões,
transações, persistência, contexto, extensões e fluxos da interface. As avaliações
com um modelo real medem outra coisa: se esse modelo consegue cumprir os
critérios de uma tarefa usando a aplicação. Um resultado não substitui o outro.

## Manutenção e desempenho — 29/10/2026

A auditoria partiu de `a25c481`. `gocognit -over 15 .` encontrou 336 funções,
incluindo 31 em testes e duas no arquivo gerado pelo SQLC. Após a decomposição
por responsabilidade, o mesmo comando e a variante `-exact-recursion` retornaram
zero ocorrências. O limite permaneceu em 15; nenhum arquivo foi excluído.
Os 91 diagnósticos iniciais do Staticcheck foram resolvidos; a análise com
`-tags integration` também terminou sem diagnósticos.

Passaram a suíte completa `go test -race -p 1 ./...`, `go vet ./...`, a compilação
dos testes com tag `integration` sem execução das APIs e o inventário de linguagens.
O pós-processador SQLC reproduziu exatamente o `db.go` atual a partir da saída
gerada do commit inicial; não foi necessário executar um SQLC externo.

Foram corrigidas falhas de continuação do lote após negar permissão, descarte de
comandos de atividade/conclusão na interface, descritor de log deixado aberto,
falha na criação do watcher, índice negativo em resultados Sourcegraph malformados
e erros de leitura do Scanner do cliente attach. O limite de entrada de 32.000
bytes foi preservado; falhas de leitura agora chegam ao caller sem falso EOF.
Os testes preservam aprovações, sandbox, CAS/rollback, recibos de leitura e os
critérios das avaliações.

A varredura complementar do gopls 0.23.0 considerou 575 arquivos. Modernizações
oficiais do Go removeram alocações em iterações sobre texto e simplificaram
coleções e escritas de saída. Sugestões que mudariam JSON gerado, layout de
campos atômicos, cancelamento explícito de testes ou ciclo de goroutines não
foram aplicadas automaticamente. As assinaturas públicas continuam com os
mesmos tipos e aridade.

O módulo passou a exigir Go 1.27.0, recomenda a toolchain 1.27.3 e o CI acompanha
`1.27.x`. Fixtures de projetos Go 1.26 permanecem como casos de compatibilidade;
os dados e critérios dessas fixtures não foram migrados. A troca da toolchain
não altera formatos de configuração, bancos ou recibos de leitura. Os
benchmarks abaixo foram coletados com Go 1.27.0 e não constituem uma comparação
entre Go 1.26 e Go 1.27.

Microbenchmarks locais em Linux amd64, Go 1.27.0, Core i9-12900K:

| Trecho e fixture | B/op antes | B/op depois | Alocações antes → depois |
| --- | ---: | ---: | ---: |
| Sourcegraph: 100 ocorrências, arquivo de 2.000 linhas | 3.372.399 | 108.593 | 1.634 → 1.021 |
| Sombra do overlay, 120×40 | 637.748 | 17.712 | 142 → 26 |
| Formatação grep, 100 ocorrências | 1.798.918 | 115.377 | 407 → 142 |
| Conversão UTF-16, 1.000 edições / 100 kB | 70.208 | 41.008 | 10 → 3 |
| Regex da versão de adapter | 65.921 | 0 | 303 → 0 |

Na conversão UTF-16, a mediana local caiu de 7,774 ms para 67,603 µs por operação.
As medições são de componentes e fixtures sintéticas, com três amostras onde
indicado no [registro estruturado](evidence/2026-10-29-maintenance.json); regex e
grep têm uma amostra. Elas não medem a latência geral do agente nem a inferência
do modelo. A equivalência da saída é verificada pelos testes dos formatters e
do overlay, incluindo Unicode e ANSI.

A aceitação `TestLiveOllamaPTYUI` passou com o binário real e `llama3.1:8b` no
Ollama em loopback. A execução final com Go 1.27.3 terminou em 27,32 segundos
(a execução anterior com Go 1.27.0 levou 28,73 segundos). Esses tempos isolados
não demonstram uma melhora de latência da toolchain. O fluxo cobriu colagem Unicode, input de
3–8 linhas, três redimensionamentos, catálogos/modelos, foco de modais, evidências
em modo Ask, probe LSP aprovado separadamente, um comando PTY aprovado, saída ao
vivo, entrada humana e uma nova instrução enquanto o comando aguardava entrada.
HOME, credenciais e repositório eram fixtures descartáveis; Bubblewrap permaneceu
habilitado e os arquivos de fonte foram comparados no final. Builds Go 1.27.3
também passaram para Linux amd64, Windows amd64 e macOS ARM64; os dois últimos
foram compilados de forma cruzada, sem execução nativa.

A varredura dos marcadores removeu o remendo Copilot inativo. A limitação de imagens
no leitor de texto ficou explícita; as compatibilidades de URI e a ação `quickfix`
do protocolo LSP foram preservadas. O TODO de tornar as exclusões do watcher
configuráveis permanece como possibilidade de configuração futura; as exclusões
atuais continuam protegendo o workspace. Makefile e CI verificam complexidade e
Staticcheck com versões fixas, incluindo a tag `integration` na análise estática.

## Ollama real — 28/10/2026

Modelo instalado: `llama3.1:8b`, API nativa do Ollama em loopback, janela
configurada de 16.384 tokens e reserva de resposta de 2.048 tokens. Os testes
usaram workspaces descartáveis; comandos permaneceram no Bubblewrap e passaram
pelas autorizações previstas pelo teste.

Na versão inicial, o fluxo completo de terminal passou: aprovação na TUI, comando real com PTY,
saída parcial ao vivo, entrada humana e instrução adicional enquanto o comando
aguardava entrada. O modelo seguiu a instrução posterior e a ordem de chamadas,
resultados e mensagens foi preservada no histórico.

Uma execução inicial das seis fixtures do [avaliador](evaluation.md) resultou em:

| Caso | Resultado observado |
| --- | --- |
| Explicar o projeto | Passou; leu arquivos e citou os fatos exigidos. |
| Localizar um defeito | Passou; identificou o arquivo e preservou o conteúdo. |
| Editar um arquivo | Falhou; o arquivo permaneceu com o operador incorreto. |
| Executar verificação | Passou; `run_checks` registrou a verificação esperada. |
| Interpretar erro | Passou; tratou a ausência do arquivo sem inventar conteúdo. |
| Retomar plano | Falhou no estado final `planned`; consultou a tarefa e preservou o identificador na resposta. |

Portanto, **4 de 6 casos passaram nessa execução**. Os critérios reprovados não
foram relaxados e o comando de avaliação retornou erro. São limitações observadas
nessa combinação de modelo, prompt e contexto; não demonstram uma taxa geral de
acerto. A aplicação não promete que a disponibilidade de ferramentas faça um
modelo utilizá-las corretamente.

O modo offline valida as seis fixtures e seus contratos sem gerar respostas.
`validated` nesse modo não equivale à aprovação de um modelo. Reproduza ambos
os modos conforme [Avaliações](evaluation.md). Os relatórios excluem chaves,
respostas completas e conteúdo dos arquivos do usuário.

## Segunda evolução — 28/10/2026

A entrega acrescentou conclusão verificável, recuperação contextual, calibração
de contexto, diagnóstico offline, teste de ferramenta opt-in, índice incremental,
exploração limitada, formatação, worktrees, preferências e revisão adaptável,
OAuth MCP e servidor loopback reconectável. Veja os controles atuais em
[interface](workspace-ui.md), [ferramentas](tools.md) e
[servidor/autenticação](server-and-auth.md).

Verificação da implementação:

- `go test -race ./...` passou em todos os pacotes, incluindo os novos testes
  de concorrência, isolamento de exploração, autorização, OAuth e interface.
- `go vet ./...`, `go build` e `git diff --check` passaram.
- A avaliação offline validou as seis fixtures; isso não recebe nota de modelo.
- Um teste com servidor real em loopback confirmou autenticação, ausência de
  execução antes da aprovação, entrada humana em PTY e reconexão ao mesmo
  processo, sem repetir comandos. Bubblewrap permaneceu habilitado.
- A TUI real foi exercitada em PTY 80×24 para primeiro acesso, presets e busca
  de preferências. Testes de geometria também cobrem 36×14 e 160×40.
- `doctor --probe-model` confirmou uma chamada inerte com nome e desafio
  corretos no `llama3.1:8b`. Nenhuma ferramenta foi executada pelo probe. O
  diagnóstico identificou `ruff` e `prettier` ausentes, sem instalá-los.
- Builds Linux amd64 locais foram produzidos com SHA256; duas execuções com
  fontes/toolchain iguais produziram arquivos idênticos. Não houve publicação.

A execução final das seis tarefas reais, com os mesmos prompts e critérios,
continuou com **4 de 6 casos aprovados**. O [relatório sanitizado](evidence/2026-10-28-ollama-v2.json)
registra o modelo, hash da suíte, ferramentas chamadas e estado final:

| Caso | Resultado final |
| --- | --- |
| Explicar projeto | Passou. |
| Localizar defeito | Passou. |
| Editar arquivo | Falhou no conteúdo; nenhuma mudança foi aplicada. A tarefa terminou como `partial`, com `requires_changes=true`, sem falso estado de conclusão. |
| Executar verificação | Passou, com recibo válido. |
| Interpretar erro | Passou. |
| Retomar plano | Falhou: o modelo não salvou um plano válido. Após tentativas limitadas de reparo, a tarefa terminou como `blocked`; não houve plano fabricado nem estado `planned` automático. |

O comando retornou erro pelas falhas reais; nenhum critério foi removido ou
flexibilizado. As proteções melhoraram a fidelidade do estado exibido, mas não
demonstraram melhora na capacidade desse modelo de editar ou manter o plano.
Um probe de ferramenta aprovado não substitui esses testes de tarefas.
OAuth foi validado com servidor HTTP controlado, PKCE/estado/emissor, recusa de
redirects e invalidação de refresh após logout; não houve login em conta externa.
Textos legados e diagnósticos externos ainda podem manter o idioma original.

## Configuração em pasta e template inicial

Os caminhos novos `~/.opcode/config.json` e `.opcode/config.json` foram verificados
com precedência global/projeto, segurança global, gravação no arquivo de origem
e compatibilidade com os JSONs anteriores. A criação do diretório de configuração
não esconde um banco antigo em `.opencode`.

Testes cobrem criação privada, publicação atômica com 16 inicializações
concorrentes, recusa de diretório simbólico, preservação de JSON inválido, ausência
de cópia de credenciais antigas e ausência de rede durante o bootstrap sem
provedor selecionado. Um teste com o binário real e HOME temporário confirmou
criação na primeira execução, preservação das edições, recriação após remoção
explícita e `doctor` sem criar arquivos. `go test -race ./...`, `go vet ./...`,
build e `git diff --check` passaram após a mudança. Não houve nova inferência.

## Configuração guiada, contratos e planos locais — 28/10/2026

O diagnóstico das fixtures identificou duas causas concretas: o modelo mostrava
uma correção sem chamar `edit`, e serializava arrays do plano como strings. As
instruções locais passaram a indicar a operação real; o binding nativo de planos
agora anuncia campos simples (`steps_text`, `criteria_text`, `checks_text`),
validados no mesmo Store, sem executar ou interpretar literais Python.

A avaliação final manteve os seis prompts e todos os critérios originais e foi
repetida **duas vezes**, em processos/workspaces novos: **6/6 em cada execução**.
O [relatório sanitizado](evidence/2026-10-28-ollama-contracts.json) preserva hash da
suíte, ferramentas, estados e durações. A fixture de edição verifica o conteúdo
corrigido e termina **partial**, pois não configura checks. A de verificação
registra um recibo válido; a de retomada salva o plano e consulta `get_task` na
segunda mensagem. Não há declaração de teste aprovado para a edição nem taxa
geral de acerto inferida desses doze casos. Os relatórios anteriores de 4/6
permanecem acima como histórico.

Testes adicionais cobrem contratos incompatíveis recusados antes da inferência,
chamadas de escrita forjadas no modo Verificar, checks atuais obrigatórios,
preservação de rascunho, foco por teclado/mouse, formulário 36×14, confirmação
antes de inferência opcional e recusa de worktree mutável em modo de leitura.
Migração de configuração preserva bytes originais em backup privado protegido,
campos desconhecidos e inteiros grandes, com rollback por restauração e recusa
de JSON inválido/symlinks. Metadados do Ollama têm limite, timeout e bloqueio de
redirects com credenciais.

Um benchmark com mil mensagens mede a navegação/renderização do viewport; no
ambiente local ficou na ordem de 0,5 ms por operação. É uma medição local com
cache aquecido, não um limite de desempenho garantido.

A checagem do binário real em PTY 80×24 passou com HOME novo e projeto Git
descartável: template, cadastro do Ollama, busca/seleção de `llama3.1:8b`, contrato
explícito, alocação observada e diff Git. Esse teste consulta apenas catálogo e
status, sem inferência. Reproduza com `OPCODE_LIVE_TESTS=1 OPCODE_LIVE_OLLAMA=1
OPCODE_EVAL_BINARY=/caminho/binario go test -tags=integration ./internal/tui
-run '^TestLiveBinaryFirstRunContractAndWorkspace$' -count=1`.

O teste real de terminal e steering também passou novamente com `llama3.1:8b`: a
TUI pediu aprovação, exibiu saída parcial, enviou entrada humana ao PTY e o modelo
seguiu a instrução posterior, preservando a ordem das mensagens e resultados.
`go test -race ./...`, `go vet ./...`, build e checagem de whitespace passaram.
Os quatro arquivos Linux/macOS amd64/arm64 foram cross-compilados e tiveram
conteúdo e SHA256 conferidos. Não houve teste manual em equipamentos macOS/ARM.

A primeira CI remota identificou a ausência de `rg` no runner. Os workflows de
verificação/release passaram a instalar explicitamente essa dependência de teste,
para executar também os testes de privacidade do índice em vez de ignorá-los.
Sem `rg` na máquina do usuário, o índice agora retorna uma orientação clara
antes de criar o banco; não instala pacotes nem enfraquece regras de descoberta.
Um teste cobre esse caminho e preserva a prioridade do cancelamento.

## Recuperação estrutural — 28/10/2026

O binário desta etapa foi exercitado com o `llama3.1:8b` local, API Ollama nativa:

- Primeira pergunta num repositório com 600 arquivos, janela manual 16.384: passou.
- Quatro turnos na mesma conversa: duas páginas ls, busca Python excluindo decoy
  TypeScript e leitura da fonte com resposta 37: passaram.
- Um único pedido composto com essas quatro operações: passou; o histórico
  confirmou chamadas/resultados reais e os arquivos permaneceram iguais.

Essa medição não substitui as seis tarefas gerais acima, não demonstra melhora
geral do modelo nem superioridade sobre Codaro/Crush. Embeddings foram validados
com serviço HTTP controlado; não foi baixado um modelo nem presumido ganho real
na busca. As dez fixtures de parser verificam nomes, escopos e conteúdo, não a
correção de programas em toda a linguagem. O fechamento da suíte completa está
no [registro da implementação](implementation-2026-10.md).


Após os testes de integração, foi corrigida uma regressão de apresentação: o
comando produzia saída real no PTY, mas a prévia só era visível com foco no
terminal. A prévia limitada reaparece enquanto o comando está ativo, sem retirar
o foco do chat; desaparece ao terminar e não comprime o editor em telas pequenas.
O teste real completo de aprovação, stdout, entrada humana e instrução concorrente
passou depois da correção.

A avaliação de seis tarefas gerais desta etapa resultou em **3/6**: overview,
validate e error passaram; localize falhou no critério textual de caminho; edit
não alterou a fonte e ficou partial; resume falhou nos argumentos de plano e
ficou blocked. Os critérios não foram afrouxados. Foi corrigido o falso requisito
que tratava “não use bash” como obrigação de chamar bash; há regressão bilíngue.
Veja o [relatório sanitizado](evidence/2026-10-28-runtime-roadmap.json). Esse
resultado limita a conclusão: os recursos e guardas funcionam, mas não há prova
de melhora geral do llama3.1:8b nem de superioridade entre aplicativos.

## Correção de edição, retomada e citação — 28/10/2026

A reprodução das três falhas usou as mesmas fixtures e API nativa do
`llama3.1:8b`, com janela de 16.384 e saída reservada de 2.048 tokens. O diagnóstico
identificou:

- O modelo agrupava leitura e edição na mesma resposta, sem ter recebido a
  fonte, e substituía o nome da função pelo texto de uma expressão. A alteração
  dependente agora recebe um recibo explícito sem mudança; a leitura real volta
  ao modelo antes de ele gerar os argumentos da edição seguinte.
- No segundo turno do plano, o Ollama misturava `final_answer` com operações
  como `get_task`. O parser rejeitava o lote inteiro. Agora descarta a resposta
  prematura e encaminha somente as operações reais para os controles normais.
  Schemas, validação do plano, permissões e estado `planned` continuam exigidos.
- A resposta identificava a operação errada, mas omitia o arquivo. `view` agora
  inclui o caminho no texto do resultado; uma guarda limitada pede a citação
  ausente usando recibos de fonte bem-sucedidos do turno, sem fabricar referências.

O prompt passou para `opcode-2026-10-v3`. O arquivo da suíte de avaliação não foi
alterado: hash `08b9aff679c7f9743feb5019658ffde63e9a9f679b566037edc016054860db24`.
Com os seis pedidos e critérios originais, a repetição em processos e workspaces
novos passou **6/6 em cada uma de três execuções (18/18)**. O
[relatório sanitizado](evidence/2026-10-28-ollama-workflow-repair.json) registra
critérios, ferramentas, tokens, estados e durações; os relatórios anteriores
permanecem como histórico. A edição passa pelos critérios de conteúdo e termina
`partial`, pois essa fixture não configura checks; não há recibo de teste
inventado nem conclusão completa automática.

Os testes determinísticos cobrem edição prematura em ambas as ordens do lote,
leituras paginadas, patches, arquivos independentes, limites de reparo de citação,
recusa de leituras antigas/erros como prova, negações e preferências explícitas,
respostas Ollama mistas e recusa de argumentos inválidos. A suíte completa com
`-race`, `go vet` e build com `CGO_ENABLED=0` passaram. O teste opt-in
`TestLiveOllamaWorkflowEvaluation` também executa as seis fixtures originais
diretamente no Ollama, separado dos testes determinísticos.

O mesmo código também passou nos fluxos reais de primeira pergunta com contexto
automático de 131.072 tokens, repositório grande com janela manual de 16.384 e
pedido composto de recuperação. O PTY real passou com aprovação pela TUI,
saída ao vivo, entrada humana e nova instrução enquanto o agente aguardava;
Bubblewrap permaneceu ativo e a ordem persistida das mensagens foi conferida.

Esses resultados comprovam os critérios declarados dessas tarefas, não a
correção semântica de toda a prosa. A guarda de referências verifica presença de
caminho, não valida automaticamente a linha citada; checks reais e revisão do
resultado continuam necessários. Não foi feita comparação de qualidade com
outros aplicativos nesta correção.

## Comparação controlada, citações e linguagens — 28/10/2026

O novo validador encontrou um problema real no contexto inicial: as leituras
chegavam ao modelo, mas seus metadados não eram persistidos. O candidato inicial
passou 12/18 tarefas; o [relatório anterior](evidence/2026-10-28-citations-before-repair.json)
foi preservado. As leituras agora conservam o recibo nativo. Caminhos inventados,
linhas não entregues, fontes alteradas, erros e resumos continuam recusados.

O prompt é `opcode-2026-10-v4`. Há uma tentativa de reparo de citação, com o
pedido original preservado. No Ollama, `answer_with_sources` separa explicação
e fonte e oferece apenas intervalos atuais como opções. A resposta ainda passa
pela mesma verificação de caminho/faixa/hash. Uma resposta final de protocolo
malformada pode ter uma regeneração própria, sem texto parcial nem chamadas
publicadas; testes verificam limite e ausência de repetição de efeitos.
O [candidato intermediário](evidence/2026-10-28-protocol-before-repair.json) com
erro após edição correta também permanece como evidência, sem ser contado como
sucesso da rodada final.

O conjunto e seus critérios não mudaram: SHA-256
`08b9aff679c7f9743feb5019658ffde63e9a9f679b566037edc016054860db24`.
As últimas execuções usaram processos/workspaces novos, `llama3.1:8b`, API nativa
Ollama, janela explícita 16.384 e reserva 2.048. Essa janela é uma condição do
experimento, não um limite gravado na configuração normal do usuário.

A primeira comparação tinha amostragem diferente: o padrão Codaro era 0,1.
Na rodada controlada, o adapter usa o codec público do provedor para fixar
temperatura zero, como no Opcode, preservando agente, prompts, transporte,
cancelamento e permissões. Temperatura observada aparece nos relatórios; o
comparador distingue observação ausente de zero e recusa diferenças.

| Rodada final | Resultado |
| --- | --- |
| Opcode: explicar/localizar, três repetições | 6/6 casos, 24/24 critérios |
| Codaro `0.2.0+b54a68640ee5`: mesmos casos/repetições | 6/6 casos, 24/24 critérios |
| Opcode: seis tarefas originais, três repetições | 18/18 casos |

Na [comparação controlada](evidence/2026-10-28-comparison-controlled.json), o
Opcode levou 50.934 ms e consumiu 25.789 tokens de entrada/1.479 de saída; o
Codaro levou 26.688 ms e consumiu 26.995/1.022. São totais observados de seis casos,
incluindo trabalho/reparos de cada agente. Não estabelecem superioridade geral,
correção semântica da prosa ou efeito causal isolado do prompt. Observações do
adapter externo não são recibos nativos independentes de Opcode. Não houve
execução real do Crush: o executável não está disponível neste ambiente.

O [benchmark de ranking](evidence/2026-10-28-retrieval-ranking.json) executou a
ferramenta protegida com cache frio/quente: duas fixtures específicas atingiram
recall/MRR 1; a fixture de sinônimos ficou em 0. Sem embeddings configurados,
todas as solicitações híbridas declararam fallback lexical. Não há medição de
ganho semântico real. Nenhum modelo foi baixado.

Passaram `go test -race ./...`, `go vet ./...`, build com `CGO_ENABLED=0` e sete
testes Python do adapter. A suíte inclui 26 perfis de parsers, migração da política
do índice, paginação/privacidade, diagnóstico LSP sem efeitos, seleção cancelável
de modelos, perguntas agrupadas com revisão e preservação do composer, validação
de fontes, protocolo Ollama e permissões/sandbox existentes. A primeira pergunta
de 16k preserva as leituras iniciais quando o payload completo cabe; o orçamento
duro e a reserva de saída continuam obrigatórios. Perguntas avançadas carregam
seu schema sob demanda.

Veja o [resumo sanitizado](evidence/2026-10-28-comparative-validation.json), o
[resultado completo do Opcode](evidence/2026-10-28-suite-controlled.json) e o
[guia das mudanças](comparative-evolution.md).

## Renderização, input e seletores — 29/10/2026

As regressões determinísticas cobrem quadro completo e rodapé em resize,
composer de três a oito linhas, colagem/Unicode, prévia limitada de anexos,
foco exclusivo em diálogos/perguntas/terminal, catálogo com 16 modelos,
busca editável, paginação, modelo ativo, mouse e mensagens atrasadas.
Também verificam contraste auxiliar em temas claro/escuro e duração em segundos.
O LSP é exercitado com linha de 256 KiB, rajada de dez mil linhas, cancelamento,
fechamento concorrente e executável ausente, sem imprimir ou reter stderr bruto.

O teste optativo `TestLiveOllamaPTYUI` executa o binário num PTY real, interpreta
ANSI com VT e usa HOME/projeto descartáveis. Requer Linux, Bubblewrap e um modelo
já instalado no Ollama local; não herda credenciais nem baixa modelos. O cenário
inclui 80×24, 120×36 e 180×40, input com Unicode e oito linhas, seletores, probe LSP
explicitamente aprovado, pergunta sobre arquivos reais, comando com aprovação
pela interface, saída ao vivo, entrada humana e instrução enviada durante execução.
O probe e o comando têm aprovações independentes. O LSP ruidoso é uma fixture
controlada; isso não comprova funcionamento de todos os servidores de linguagem.

Para reproduzir a aceitação completa, com Ollama acessível em loopback:

```sh
go build -o /tmp/opcode-ui-acceptance .
OPCODE_LIVE_TESTS=1 OPCODE_LIVE_OLLAMA=1 \
  OPCODE_EVAL_BINARY=/tmp/opcode-ui-acceptance \
  go test -tags=integration ./internal/tui \
  -run '^TestLiveOllamaPTYUI$' -count=1 -timeout=10m -v
```

`TestPTYUIControlFilterFragmentedUnicode` e `TestPTYUIVTLastColumnErase` rodam
sem Ollama com `go test -tags=integration ./internal/tui -run '^TestPTYUI'`.
O segundo reproduz o apagamento da última célula após autowrap e protege a
margem do rodapé. Esta rodada verifica o fluxo da interface; não substitui a
avaliação comparativa de qualidade, nem altera seus critérios ou os gates de citações.

Na rodada local de aceite, `TestLiveOllamaPTYUI` passou em **16,10 s** com
`llama3.1:8b`, incluindo as duas aprovações independentes e um único comando Bash.
A explicação final teve citação válida e a instrução posterior foi persistida
após o resultado do terminal, sem repetir o comando. O limite manual de 16.384
tokens pertence somente à fixture deste teste; não altera a configuração normal.
Também passaram a suíte completa com `-race`, `go vet ./...`, build com
`CGO_ENABLED=0`, os testes VT sem Ollama e `go vet -tags=integration ./internal/tui`.
Veja o [registro sanitizado](evidence/2026-10-29-ui-pty-validation.json).

## Fluxo diário, permissões e recuperação — 29/10/2026

A revisão posterior prioriza quatro pontos: reparos internos identificados por
metadados, parâmetros visíveis nas permissões genéricas, erros acionáveis e
atalhos de edição separados da navegação. As regressões verificam a persistência
da origem, que texto humano não possa imitar esses metadados, redação de
credenciais sem modificar a solicitação aprovada, preservação de diagnóstico e
rascunho, e distinção de timeout/cancelamento frente ao orçamento do modelo.

Também foram exercitados modelos atual/favoritos/recentes, opções tipadas,
argumentos literais com espaços/Unicode e valores longos, descarte da edição ao
reabrir integrações, prévia de temas sem gravação, largura de prosa sem truncar
código, painel de tarefa e abas de contexto. Enfileirar uma instrução preserva
o status e o relógio da operação em curso. Notificações têm teste de opt-in,
foco e conteúdo fixo; a entrega pelo desktop não foi validada visualmente.

O aceite final `TestLiveOllamaPTYUI` passou em **15,97 s** com `llama3.1:8b` e o
build `CGO_ENABLED=0`. Além do cenário anterior, inclui `Ctrl+U` no input,
abertura/fechamento de Tarefa e Contexto, editor de argumentos e inspeção do alvo
na aprovação de LSP. Os critérios de conteúdo, citação, nenhuma execução antes
da aprovação, um único comando e arquivos-fonte inalterados continuam exigidos.
Passaram também `go test -race -p 2 ./...`, `go vet ./...`, os testes VT sem
Ollama e `go vet -tags=integration ./internal/tui`.

Durante o desenvolvimento, houve falha de compilação por cota em `/tmp`; os
temporários/cache foram movidos para a área de trabalho. O harness também foi
ajustado para aguardar o fechamento real dos painéis e inspecionar argumentos
longos. Marcadores do próprio teste estavam entrando no digest de fontes e
produzindo corretamente um resultado parcial sem checks: foram movidos para a
área de cache da fixture. O gate de conclusão não mudou; a comparação de todos
os arquivos-fonte e a conferência dos marcadores antes/depois da aprovação foram
mantidas. Nenhum desses ajustes altera os critérios da avaliação comparativa.

Veja o [registro desta revisão](evidence/2026-10-29-ui-ux-validation.json) e o
[guia atualizado da interface](workspace-ui.md). O resultado cobre a fixture
local; não demonstra superioridade geral de qualidade ou latência.

## Linguagens, métricas e conveniência — 29/10/2026

Esta evolução amplia os perfis de linguagens de 26 para 42, com fixtures que
verificam declaração, linhas e corpo indexado. O índice usa `parser-v5` para
reconstruir entradas antigas. O suporte de recuperação não instala compiladores
ou servidores LSP e não equivale a validar programas em todas essas linguagens.

Há uma matriz de 156 cenários de menus, controles de raciocínio somente para
capacidades declaradas, editor de tema com prévia e rollback, clipboard nativo
com entrada literal e cópia SSH por OSC 52. O comando `watch` acompanha mensagens
salvas somente para leitura. `serve` e `attach` já existiam: sua omissão na
comparação anterior não representava ausência desses recursos no Opcode.

Na avaliação nativa com `llama3.1:8b`, os seis casos originais passaram nas duas
rodadas: **12/12 casos e 40/40 critérios**. Foram observados 85.042 ms no total,
86 ms de preparação de contexto, 76.440 ms nas chamadas ao provedor, 40 ms nas
ferramentas do loop e seis reparos. Leituras de bootstrap pertencem à preparação
e aos recibos; não são novas chamadas do loop nesse contador. Os tempos não
formam uma decomposição completa e não provam redução de latência.

O Codaro instalado (`0.2.0+b54a68640ee5`) teve seis casos aprovados, quatro
reprovados e dois indisponíveis, somando 64.126 ms. Edição não produziu as
alterações exigidas nas duas rodadas; o caso de erro não registrou a ferramenta
exigida. A validação nomeada ficou indisponível porque o adaptador não traduz
shell checks sem vínculo exato de argv. O comparador declarou
`comparable_conditions: false`: não há observação de temperatura no caso
indisponível. Esses resultados não sustentam uma comparação global de velocidade
ou qualidade. O Crush não foi executado: não há binário instalado e o adaptador
existente não oferece comparação de protocolo idêntico com Ollama nativo.

Foram preservados prompts, critérios, hash da suíte, temperatura zero observada
nos casos executados, janela de 16.384 tokens e reserva de 2.048 nas fixtures.
As aplicações mantêm políticas e prompts internos diferentes; cache e tempo de
carregamento do modelo não foram isolados. Os limites da fixture não são salvos
na configuração do usuário.

O aceite `TestLiveOllamaPTYUI` passou em **22,13 s**, com build sem CGO, incluindo
citação válida, aprovações independentes de LSP/comando, uma única execução Bash,
saída ao vivo, entrada humana, instrução enfileirada e OSC 52 observado na saída
serializada. Também passaram os três perfis compactos de tema, inclusive edição,
resize e cancelamento. Isso verifica emissão de OSC 52, não o recebimento pelo
clipboard de outra máquina.

Um teste adicional pelo endpoint OpenAI compatível do Ollama passou resposta e
streaming, mas falhou duas vezes no marcador final após o retorno da ferramenta.
Uma chamada sintética direta também respondeu fora do formato. Essa limitação
do modelo/protocolo permanece registrada; o teste não foi afrouxado nem marcado
como aprovado. Ela é distinta do fluxo nativo com ferramentas que passou no PTY.

O aceite encontrou e corrigiu uma asserção de tipo incorreta na ligação da saída
da TUI e um diálogo inicial que cobria o rodapé em 60×20. A investigação também
reproduziu um bug de Viper: resolver `apiKeyEnv` via override aninhado escondia
endpoint e outros campos do provedor. A resolução agora ocorre após o decode,
sem persistir segredos e preservando a precedência da variável nomeada.

Relatórios sanitizados: [Opcode](evidence/2026-10-29-evolution-opcode.json),
[Codaro](evidence/2026-10-29-evolution-codaro.json) e
[comparação com limitações](evidence/2026-10-29-evolution-comparison.json).
Passaram `go test -race -p 2 ./...`, `go vet ./...`,
`go vet -tags=integration ./...`, build com CGO desativado e `git diff --check`.
O [registro consolidado](evidence/2026-10-29-evolution-validation.json) distingue
esses gates do teste adicional reprovado na API compatível. A avaliação nativa
foi executada antes das correções finais exclusivas da TUI; o PTY passou com o
build final sem CGO.
Consulte também [as métricas](evaluation.md),
[a matriz de menus](ui-regression-matrix.md),
[raciocínio e clipboard](model-and-clipboard.md),
[temas](theme-editor.md) e [observação de sessões](shared-observer.md).

## Perguntas malformadas e evidências por assunto — 29/10/2026

O erro do print foi reproduzido: `choices` chegava como string contendo uma lista
JSON. O decoder agora aceita uma única camada desse formato nas perguntas simples
e agrupadas, antes da validação existente. Rejeita prosa, CSV, listas inválidas e
excessos de tamanho. A regressão do runtime verifica a espera pela resposta humana
sem regeneração de reparo; respostas continuam sem autoridade para aprovar ações.

Perguntas genéricas sobre o projeto passaram a exigir fontes em todos os modos.
A busca por assunto usa o índice e leituras numeradas, com prioridade para arquivos
nomeados pelo usuário. Um recibo válido de manifesto não substitui a fonte
selecionada para explicar uma funcionalidade. Leituras automáticas reaproveitadas
exigem hash atual e trecho visível; cópias antigas são abreviadas somente na
projeção do provedor, mantendo o histórico persistido. Elas não contam como nova
execução de ferramenta nem satisfazem uma instrução explícita de chamada.

O processo distingue essas leituras, buscas, fontes reutilizadas, chamadas de
terminal e reparos. O inventário oferece resumo paginado e `profile_count`,
separando os 42 perfis de parser dos tipos de arquivo encontrados no projeto.
No checkout do próprio Opcode, a busca usa a definição `localParsers`, evitando
usar o helper de classificação de perguntas como prova do suporte de linguagens.

Passaram os seis casos originais em duas rodadas: **12/12 e 40/40 critérios**.
O hash da suíte é idêntico ao da versão anterior. O conjunto adicional passou em
**6/6 execuções e 32/32 critérios**, com apresentação genérica, linguagens e
continuação. Seus critérios permaneceram intactos após a primeira rodada.
As falhas intermediárias — duas perguntas genéricas e a prioridade errada de
README numa localização explícita, entre outras — estão registradas junto dos
resultados finais, sem serem apresentadas como aprovações.

O PTY com `llama3.1:8b` passou em **22,01 s**, com uma pergunta real pela ferramenta,
resposta humana e retomada, além do cenário de comandos, aprovações independentes,
saída ao vivo, entrada de terminal, fila e OSC 52. Passaram também os perfis
compactos e a suíte completa com `-race -p 2`, vet com integration e build sem CGO.

A consulta numa cópia limpa das fontes recebeu `profile_count: 42` e leu
`internal/llm/tools/languages.go:50-76`. O modelo passou a citar essa definição,
mas ainda listou os 41 parsers do mapa sem separar o parser padrão Go, omitiu o
total solicitado e respondeu além do formato pedido. Esse probe é **parcial em
qualidade da resposta**; sucesso de leitura e de processo não foi convertido em
aprovação semântica. A validação de citações não comprova toda interpretação.

Veja o [guia de comportamento](question-and-evidence-recovery.md), o
[relatório original](evidence/2026-10-29-question-recovery-original.json), o
[conjunto adicional](evidence/2026-10-29-question-recovery-generic.json) e o
[registro consolidado com limitações](evidence/2026-10-29-question-recovery-validation.json).

## Nove melhorias — 29/10/2026

A suíte completa `go test -race -p 1 ./... -count=1`, vet, build sem CGO, smoke
Linux e teste de drift do inventário passaram. A execução sequencial mantém os
orçamentos de parsing; uma tentativa anterior concorrente atingiu o timeout da
fixture C++, sem alteração de prazo, critérios ou skips para aprovar. O fluxo
PTY final com Ollama `llama3.1:8b` passou, incluindo terminal, aprovações, entrada,
instrução durante execução, modelos, busca e seleção.

A comparação completa executou os seis casos duas vezes por aplicativo, sem
rerun: Opcode 12 aprovados; Codaro 7 aprovados/3 reprovados/2 indisponíveis;
Crush 12 indisponíveis no adapter. O gate estrito recusou ranking e condições
completamente comparáveis. Isso não declara falha do Crush nem superioridade
geral. Ver [comparação e limites](comparison-2026-10-29.md) e
[evidência de validação](evidence/2026-10-29-nine-validation.json).

O cofre foi testado com backend injetado, inclusive quit e rollback assíncrono;
nenhum cofre pessoal foi acessado. Compilação cruzada Windows/macOS e ZIP foram
verificados; a execução nativa desses sistemas pertence aos jobs de CI.

Os workflows também passaram no `actionlint v1.7.12` (sintaxe e contextos de
Actions). Caminhos temporários do runner são exportados no primeiro passo, antes
do setup-go; não usam `runner` no `env` do job, onde esse contexto não existe.
O script de exportação foi exercitado com caminho Windows contendo espaços.
Essa correção não alterou código da aplicação nem critérios de avaliação.

A execução nativa do CI expôs ainda a criação do master PTY Darwin sem polling
do Go e uma fixture que enviava entrada antes de observar prontidão. O master
agora tem propriedade única e O_NONBLOCK antes de `os.NewFile`; deadline/read e
interrupção por fechamento possuem regressões. A fixture aguarda READY e mantém
os cinco segundos originais. Race repetido 20 vezes, processos/permissões e o
fluxo real Ollama/PTY passaram após o ajuste (25,71 s no caso real).

O CI Linux também instala e verifica Bubblewrap, necessário aos testes de
recuperação/listagem sob a política de execução; sua ausência continua sendo
erro, sem fallback. A comparação compartilhada preserva o binário e os dados
originais: seus casos não solicitam PTY. A correção do descritor foi validada
separadamente e não motivou uma repetição para mudar o placar.

O runner Ubuntu 24.04 exigiu também um perfil AppArmor específico para permitir
user namespaces ao executável root-owned `/usr/bin/bwrap`. O workflow cria esse
perfil somente no runner descartável e prova `--unshare-all`, inclusive rede
isolada. Não desativa AppArmor/sysctl global nem troca a aplicação para
`sandbox=none`. A medida segue o mecanismo descrito nas
[notas oficiais do Ubuntu](https://documentation.ubuntu.com/release-notes/24.04/#unprivileged-user-namespace-restrictions).
Os smokes selecionados Linux, Windows e macOS já executaram seus passos de teste
e CLI com sucesso no CI; isso não transforma o smoke em suíte integral macOS/Windows.
