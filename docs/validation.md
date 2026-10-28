# Validação do Opcode

Os testes Go determinísticos verificam protocolo de provedores, permissões,
transações, persistência, contexto, extensões e fluxos da interface. As avaliações
com um modelo real medem outra coisa: se esse modelo consegue cumprir os
critérios de uma tarefa usando a aplicação. Um resultado não substitui o outro.

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
