# Arquitetura das integrações e ferramentas deste fork

Este documento descreve os limites alterados na modernização. A visão geral do restante do aplicativo permanece no README.

## Limites e dependências

O agente consome a interface `provider.Provider`. Adaptadores privados convertem mensagens, ferramentas e streams de cada protocolo; `apiTransport` concentra HTTP e reenvios. Os SDKs de geração foram removidos. Bibliotecas de autenticação/assinatura de nuvem e a biblioteca MCP permanecem.

Modelos locais podem selecionar `api: "ollama"` para usar `/api/chat` com NDJSON e contexto aplicado por requisição. O adaptador traduz a função interna `final_answer` em conteúdo de resposta, descarta respostas prematuras em lotes mistos e mantém todas as ferramentas reais no caminho normal de autorização. O protocolo local compatível com OpenAI permanece disponível.

As fábricas de ferramentas dos agentes aplicam `tools.SecureTool` a cada entrada. A política de acesso é propagada pelo contexto e usa o serviço de permissões. O agente de pesquisa recebe somente ferramentas de leitura. O executor `tools/shell` é usado por comandos, inspeção Git, verificações, processos, LSP e MCP stdio. Configurações de segurança vêm da configuração global do usuário; opções do repositório não podem enfraquecê-las.

A leitura e a gravação de arquivos usam raízes `os.Root`. A instalação de edições pertence a `tools/transaction.go`, com staging, revalidação e restauração após falhas de execução. A persistência de mensagens e histórico continua em SQLite, sem migração de schema para o estado opaco de continuação dos provedores.

A navegação LSP utiliza os clientes existentes. Alterações no registro de clientes passam por `lsp.SetClient`/`DeleteClient`, e consumidores de ferramentas usam snapshots protegidos. As gravações do protocolo são serializadas e chamadas aguardam resposta com cancelamento.

O aplicativo é dono de um `ProcessManager` compartilhado por shell, verificações,
processos e painel de terminal. A interface consulta snapshots limitados; não
consome nem remove a saída que as ferramentas recebem. PTYs usam `creack/pty` e
uma tela VT limitada de `hinshun/vt10x`, sem suas sequências de controle chegarem
ao terminal externo. São duas dependências focadas, sem novos serviços. Ferramentas que recebem esse
manager apenas o emprestam: fechar um agente para trocar conexão não o encerra.
`App.Shutdown` encerra os processos compartilhados; fábricas independentes
atribuem um único dono ao manager que criam.

`agent.Submit` usa um único worker por sessão. Instruções durante uma geração
ficam em uma fila limitada, aplicada em limites entre operações. O worker é o
único escritor das mensagens, mantendo chamadas/resultados de ferramentas antes
da próxima mensagem do usuário. Os canais de conclusão são bufferizados para
que a interface não precise drená-los para liberar o worker.

O worker mantém o modo e o catálogo de ferramentas por execução. Perguntar e
Planejar usam listas explícitas de capacidades; o executor recusa ferramentas
ocultas ou proibidas, independentemente das instruções do modelo. Contexto inicial
usa as ferramentas protegidas de leitura e gera chamadas/resultados reais.

`workflow.Store` guarda plano, modo, revisão e recibos de verificações na tabela
aditiva `agent_workflows`, pertencente à sessão e à raiz do workspace. Não guarda
permissões. Commits de arquivos notificam a invalidação; hashes limitados do
workspace detectam mudanças externas. Recibos vinculam checks ao hash do comando
configurado e à revisão atual. A migração pode ser revertida sem remover o
histórico da conversa.

`interaction.Questions` pertence ao aplicativo e mantém no máximo uma pergunta
pendente por sessão. A TUI consulta snapshots, recebe uma escolha/texto real e
devolve o resultado pelo protocolo de ferramentas. Cancelamento libera a espera;
respostas não alteram o serviço de permissões.

O editor mantém o filtro, a seleção e o fechamento das sugestões de comandos
`/`; a página posiciona o menu acima do campo e entrega suas teclas antes do
cancelamento da sessão. A seleção usa o mesmo envio/ACK dos comandos digitados.
Falhas locais e recusa da fila preservam o rascunho. O menu não aparece no PTY
nem captura a entrada destinada a perguntas ao usuário.

Limites de etapas, duração e resultados pertencem ao worker. Um wrapper da
interface Provider limita a primeira resposta e o silêncio entre eventos, sem
contar a execução local das ferramentas. Não há reenvio por timeout. Falhas
reparam chamadas incompletas no histórico e preservam instruções já aceitas,
exceto no cancelamento explícito. Veja `docs/agent-runtime.md` para configuração,
exclusões da impressão de arquivos e limites de garantia.

O inventário estático `RegisteredLanguageSupport` é compartilhado pela ferramenta
de suporte e pelo gerador `scripts/language-docs`; fixtures de declarações/chunks
cobrem cada perfil e um teste de drift mantém a matriz Markdown atualizada. O
gerador não consulta configuração, PATH, fontes ou servidores instalados.

## Contratos e falhas

Resultados de ferramentas preservam `ToolResponse`, `IsError` e metadados JSON. Comandos retornam exit code, timeout, cancelamento, truncamento e timestamps. Inspeção Git e navegação LSP retornam estruturas limitadas. Processos têm identificadores e propriedade por sessão, prazo de expiração e encerramento no cancelamento da sessão ou do aplicativo.

Permissões de arquivos têm escopo de caminho resolvido; as de navegação externa usam origem de rede. Sem aprovação, acesso sensível/externo e operações mutáveis falham. Uma sessão autoaprovada é uma exceção explícita do usuário, não uma mudança no isolamento do processo.

Bubblewrap é o isolamento padrão no Linux. Ausência do binário ou recusa de namespaces causa falha; execução sem sandbox exige opção global explícita. Chamadas de geração aos provedores continuam no processo do aplicativo e não dependem da liberação de rede dos comandos.

Edições não fornecem transação distribuída entre SQLite e filesystem nem garantia de commit conjunto após falha de energia. Backups que não possam ser restaurados são mantidos e a falha é exposta. Estado em memória de processos não sobrevive ao encerramento do aplicativo.

## Observabilidade e credenciais

A captura de comandos e processos é limitada em memória. A fronteira das ferramentas remove valores conhecidos de credenciais de conteúdo, metadados e erros, preservando causas de erro para checagem de permissão. Isso não substitui controle de acesso a arquivos nem identifica segredos arbitrários escritos no código. O ambiente dos subprocessos é mínimo, com inclusões explícitas por configuração global. Payloads completos de mensagens LSP não são registrados pelo transporte.

A suíte real dos provedores exige tag de build e opt-in por ambiente; seu relatório omite credenciais, cabeçalhos e corpos. Não há métricas ou objetivos de latência/custo adicionais definidos por esta mudança.

Credenciais de provedores podem usar referências opcionais ao cofre nativo via
`go-keyring`. A lista e o diagnóstico offline permanecem em metadados; operações
autenticadas resolvem somente a conexão usada. A TUI prepara a entrada no cofre
fora do loop, aguarda o resultado nativo e persiste metadados no dono da configuração.
Falhas removem apenas a entrada nova; não há fallback para chave literal. O backend
injetável separa testes do cofre real. Limites e recuperação: [cofre nativo](docs/native-vault.md).

## Entrega e verificação

Go 1.27 ou posterior. O build continua sendo `go build .`; o módulo recomenda a toolchain Go 1.27.3 e os workflows acompanham os patches `1.27.x`. A verificação comum é `go test -race ./...` e `go vet ./...`. Testes de sandbox usam dados temporários e indicam quando o ambiente não suporta namespaces. Testes externos de geração são separados e optativos.

`scripts/build-release` gera tar.gz (Linux/macOS), ZIP (Windows) e SHA256 locais com metadados fixos, trimpath,
CGO desativado e versão explícita. `make release-local` e o workflow de build
usam esse caminho; geração de artefatos não publica releases. O CI preserva a
suíte completa Linux e acrescenta smoke nativo limitado de CLI/config/render/permissões
em Linux/macOS/Windows, com cofre falso e startup sem inferência. O runner
`scripts/platform-smoke` isola ambiente e verifica preservação no segundo startup;
Windows continua recusando PTY, e sandbox de comandos continua Linux/Bubblewrap. O índice FTS5/BM25
pertence a um SQLite privado separado, incremental e limitado, sem migração do
banco principal. A consulta revalida acesso e conteúdo antes de retornar trechos;
não concede evidência de leitura para edição.

## Servidor local e autenticação MCP

`internal/server` oferece API autenticada somente em loopback. Seu ciclo de vida
é dono do cancelamento de workers, assinaturas de permissão e sessões anexadas.
Clientes desconectados não cancelam trabalhos aceitos, e snapshots não executam
operações. Requests de prompts podem ser deduplicados por ID; resultados usam
identidade da execução para não sobrescrever uma execução posterior. O encerramento
revoga permissões e cancela processos. Não há persistência do servidor ou dos PTYs
após encerramento, nem serviço web multiusuário. `cmd/attach.go` mantém instruções
em ordem e aprovações/respostas independentes para não bloquear um PTY.

OAuth MCP reutiliza o cliente existente, com PKCE/estado e vínculo de emissor e
recurso. Credenciais são documentos privados globais separados da configuração;
uma geração de login impede que um refresh antigo restaure tokens removidos.
Os transportes de dados e de autenticação recusam redirects. Tokens registrados
em memória participam da redação de ferramentas e snapshots. As APIs e os limites
operacionais estão em `docs/server-and-auth.md`.

Consulte `docs/tools.md` para configuração, limites e diferenças entre rede de subprocessos, navegação web e serviços MCP. Consulte `internal/llm/provider/integration/README.md` para testar contas reais dos provedores.

## Provedores, perfis e primeiro acesso

O caminho global padrão é `~/.opcode/config.json`; overrides de projeto usam
`.opcode/config.json`. A resolução central em `config/migration.go` mantém os
JSONs anteriores como fallback, atualizados no próprio caminho. A primeira
inicialização normal sem arquivo global compatível publica o template embutido
com arquivo 0600 e pastas novas 0700. Um link atômico impede substituição por
inicializações concorrentes; nenhuma chave resolvida entra no template.
Diagnósticos offline e configurações explícitas não o criam. Criar a pasta nova
apenas para configuração não muda um banco existente na pasta anterior.

`models.DiscoverProviderModels` consulta catálogos HTTP limitados e canceláveis,
sem mutar o registro. `config.ModelCatalog`/`ProfileModelCatalog` capturam snapshots
no chamador e preservam definições explícitas. Perfis distinguem ID, nome e
backend; `Model.ProviderProfile` seleciona a conexão enquanto `Model.Provider`
continua selecionando o adaptador. `ProviderForModel` fornece credenciais e
endpoint da mesma conexão usada para a descoberta. `LOCAL_ENDPOINT` sobrescreve
somente a conexão local legada. Variáveis de ambiente não são serializadas.

A TUI consulta no abrir/atualizar, filtra localmente e descarta respostas atrasadas.
Cliques no modelo do rodapé e `Ctrl+O` abrem o mesmo seletor. Favoritos/recentes e
funções coder/task/title/summarizer persistem no JSON. Modelos sem contexto
informado identificam seu limite conservador como estimativa. Metadados
persistidos permitem reconhecer uma escolha mesmo sem catálogo disponível.

`app.configurableCoder` mantém identidade de serviço e broker estáveis antes do
primeiro modelo. `App.ConfigureCoderAgent` só troca o delegate ocioso, encerra o
anterior e mantém um único encaminhador de eventos. Pedidos sem setup retornam
um erro explícito; endpoint ou credenciais alterados sem reconstrução bem-sucedida
não continuam usando o cliente anterior. O primeiro acesso interativo abre o
cadastro; `--prompt` exige configuração válida antecipadamente.

Os formulários guardam rascunhos e referências a variáveis. Testes de provedores
consultam somente catálogos. Testes MCP/LSP exigem aprovação antes de iniciar o
cliente, usam escopo efêmero revogado no final, prazo de 8 s após aprovação e
fecham o cliente. Não instalam ferramentas nem chamam uma operação MCP. Salvar
LSP aplica no próximo início; MCP consulta o cadastro atual em cada chamada.
Escritas JSON preservam campos desconhecidos, usam temp privado e rename
atômico; alterações de settings são recusadas durante a execução do agente.
Veja `docs/model-selection.md` e `docs/integrations-settings.md`.

## Contexto preventivo e extensões explícitas

O pacote `internal/llm/agent` estima o payload completo antes da rede: prompt de
sistema efetivo, histórico/anexos/continuação, schemas e reserva de saída, com
margem de segurança. Usa tokenizer compatível em famílias reconhecidas e heurística nas demais;
JSON/overhead ainda são aproximações calibradas por uso real. Compactação
preventiva mantém instruções humanas e troca mais recente e substitui lotes
concluídos integralmente por recibos recuperáveis, inclusive no turno atual. Se ainda não couber,
recusa antes de abrir o stream. Títulos e resumos também têm orçamento.
`ContextControl` e `QueueControl` são capacidades opcionais; editar a fila retira
atomicamente o item sob o lock do worker e volta pelo Submit/ACK normal.

Conversas recuperadas e findings com SHA256 são dados históricos, sem evidência
atual de leitura nem autorização. Findings privados usam `internal/localstate`,
separados por workspace/sessão, com limites de 32 registros e 128 KiB; cada fonte
é lida com a política de acesso existente e limite de 16 MiB. Alterações nas
fontes são marcadas como stale. O agente registra apenas metadados de operações
nos logs, sem despejar seus payloads.

`internal/extensions` lê somente diretórios de skills explicitamente configurados
no JSON global (até 64 skills de 16 KiB). `ExtensionControl.InvokeSkill` aceita
invocação explícita do usuário via Submit, sem ferramenta equivalente para o
modelo. Hooks globais before/after-tool reutilizam bash, aprovação e sandbox,
sem interpolar texto do modelo; são bloqueados em Ask/Plan e limitados a 16
execuções de até 30s por turno. Falha ou recusa interrompe o turno, não concede
permissões nem reverte efeitos de comandos. Contratos e exemplos estão em
`docs/context-and-extensions.md`.


## Avaliações reproduzíveis

`internal/evaluation` e `opcode evaluate` validam offline seis fixtures com
contratos limitados; resultados não executados são null, sem nota de modelo.
`--live --provider-config` usa um subprocesso real por caso com HOME/workspace/DB
privados, descartados no final. Execute exige opt-in adicional e grants únicos
somente dentro da fixture; sandbox, bloqueio de rede e credenciais continuam
protegidos. Relatórios contêm resultados por critério, identificação do modelo e
contagem de chamadas, sem respostas/payloads. O contrato e o teste Ollama opt-in
estão em `docs/evaluation.md`.


## Workspace da interface e estado local

O catálogo `internal/tui/commands` alimenta sugestões `/` e a paleta. A página de
chat mantém painéis de fila, contexto, alterações, skills, permissões, ações de
resposta e recuperação. Operações assíncronas têm sequência/sessão de origem;
fechar ou trocar de conversa cancela a operação e descarta resultados atrasados.
Autorizações aparecem acima do painel que as solicitou. Os controles estão em
`docs/workspace-ui.md`.

O transcript agrupa mensagens por turno, com processo recolhível separado da
resposta. Atualizações preservam a posição de leitura e indicam novas mensagens;
o usuário volta ao final explicitamente. Ações copiam texto/código ou abrem uma
prévia protegida de arquivo, sem criar evidência de leitura para o agente.
Rascunhos e histórico de entrada são preservados por conversa; a recuperação
reenvia uma instrução pelo Submit/ACK normal, sem autoaprovar operações.

`session.Catalog` persiste arquivamento e última conversa em documento privado
limitado, separado por raiz do workspace. O editor guarda texto e anexos em
`composer-drafts.json`, inclusive o rascunho anterior à primeira sessão, e não
efetiva troca/saída se a persistência falhar. Limites: 64 rascunhos, 64 KiB de
texto/16 MiB de anexos por entrada e documento de 64 MiB. Conversas e mensagens
continuam em SQLite; exportações Markdown são dados históricos redigidos, sem
autoridade sobre ferramentas ou permissões. `internal/localstate` centraliza
limites, validação de arquivos, locks e gravação atômica dos documentos locais.

## Revisão e undo/redo de arquivos

Commits bem-sucedidos das ferramentas notificam um observador com conteúdo
anterior/posterior e identificador do turno. `review.Store` guarda snapshots
privados por workspace/sessão. Falhar ao registrar causa rollback da edição.
A interface mostra diffs e recibos de verificações, e prepara uma reversão antes
de solicitar aprovação por arquivo. Antes de aplicar, compara o conteúdo atual
com o esperado; alterações externas ou fora de ordem causam conflito.

Aplicar undo/redo reutiliza a transação protegida de arquivos e invalida os
recibos de verificações. Aprovação não elimina a revalidação de conteúdo. Os
locks de revisão não ficam retidos enquanto o usuário decide a permissão.
Arquivos, estado de revisão e SQLite não formam uma transação distribuída nem
uma garantia contra falha de energia. Comandos, serviços externos e modificações
manuais não são desfeitos por esse recurso.

## Ferramentas de código, artefatos e MCP sob demanda

`get_project_map`, outline e leitura de símbolos reutilizam a política de acesso;
leituras parciais não autorizam edição integral. Hierarquia, rename e substituição
LSP usam os clientes existentes, limites de resposta e transações com diffs.
`discover_checks` apenas sugere comandos; somente uma verificação executada para
a revisão/digest atuais produz recibo válido.

Saídas extensas podem gerar artefatos privados, redigidos e vinculados à sessão,
com cotas por página, arquivo, sessão e armazenamento total. O identificador
opaco permite consultar informação, páginas e ocorrências sem repetir comandos.
Artefatos continuam sendo snapshots históricos, sujeitos ao truncamento anterior
do buffer de processo; não concedem permissões nem evidência de leitura atual.

MCP registra despachantes nativos, sem iniciar processos ou rede na abertura.
`list_mcp_tools` consulta uma página de schemas após aprovação;
`call_mcp_tool` executa somente em Executar, também após aprovação. Nomes externos
não podem imitar uma capacidade nativa para mudar sua classificação. Recursos
MCP são páginas/textos não confiáveis, limitados e sujeitos à política de leitura.
Cada chamada captura o cadastro e fecha seu cliente; não há cache de credenciais
ou conexões antigas após uma edição. Uma chamada aprovada em andamento conserva
seu snapshot até concluir ou ser cancelada. Veja `docs/code-tools.md` para cotas.


## Identidade e publicação

O módulo público é `github.com/raulaguila/opcode`, com binário `opcode` e branch
`develop`. README e scripts instalam o código desta origem, sem presumir pacotes
Homebrew/AUR ou releases já existentes. O workflow de build usa permissões de
leitura; uma tag explícita `v*` dispara testes, vet e GoReleaser com o token do
próprio repositório. O script local de release apenas prepara uma tag, sem push.
A licença original e a proveniência são preservadas. `autoCompact` prepara o
contexto local da mesma sessão; a UI não dispara resumo remoto ou sessão nova
por esse limiar.

## Conclusão, calibração e exploração limitada

O worker registra o digest inicial para pedidos explícitos de implementação em
Executar. `workflow.Store` persiste `requires_changes`, `start_digest` e
`no_change_reason` no documento JSON existente, sem nova migração. A conclusão
exige alteração verificável ou justificativa explícita; reparos são limitados a
duas tentativas, com resultados `partial`/`blocked` preservados no evento final.
Nenhuma dessas informações constitui uma autorização.

A calibração pertence ao runtime do agente. `provider.ContextWindowController`
reconstrói uma conexão com janela menor sem realizar chamadas. Somente falhas
reconhecidas antes de conteúdo/chamadas permitem até dois novos pedidos de
geração. O histórico e ferramentas já executadas não são repetidos. A pasta de
dados recebe documento privado limitado com chaves hash e validade de 24 horas;
falha de leitura/gravação gera aviso e o ajuste permanece possível em memória.
A interface opcional de recalibração exige agente ocioso.

`explore_code` e o alias legado `agent` usam outro histórico, sem outro worker,
com lista explícita de ferramentas locais de leitura. Não recebem MCP, comandos,
escritas, hooks ou agentes aninhados. Limites locais de etapas/tempo/resultados
somam-se ao orçamento principal; uso do modelo é contabilizado na sessão pai.
Cancelamento e erros propagam-se pelo mesmo contexto. Não existe serviço extra
nem dependência adicional para essas capacidades.

A manutenção offline de configuração fica em `internal/config/manage.go`; a
versão do documento é aditiva. `workflow.Contract` persiste o resultado esperado
definido pelo usuário, e o runtime filtra suas capacidades na exposição e na
execução. `plan_arguments.go` mantém o binding plano de Ollama separado da
validação do Store. `models.InspectOllama` consulta somente metadados. A página
Git reutiliza ferramentas e permissões existentes; worktrees abrem outro processo
Opcode, preservando o isolamento dos singletons e retornando à TUI anterior.


## Recuperação estrutural e projeções derivadas

`tools` possui descoberta/exclusão compartilhada e índice FTS5. `.opcodeignore`
é aditivo às exclusões Git/fixas; nunca libera credenciais, links, dependências
ou estado privado. O cache de regras é limitado e invalida com metadados do SO.
Listagem fornece snapshot e leitura paginada verifica SHA256 antes de continuar.
O mapa estrutural é bounded e lê fontes/manifests somente sob acesso normal.

Go usa go/parser; nove gramáticas selecionadas de gotreesitter adicionam parsing
local sem CGO. As nove gramáticas têm prazo, cancelamento e teto de memória; todas as fontes estruturais são limitadas a 256 KiB. Falhas mantêm recuperação textual. O inventário distingue
fixtures sintáticas de resolução semântica pelo LSP. LSP registra inicializadores
lazy por mapa de clientes, selecionados por tipos/marcadores/presets; Shutdown
remove o registro, cancela inicialização e fecha clientes/watchers.

`App` é dono do IndexWatcher: debounce de 150 ms, reconciliação a cada 60 s,
atualizações incrementais de até oito arquivos, prazo configurável, até 2.048
observadores de diretório e cancelamento/join em Shutdown. Falha na observação
mantém reconciliação e refresh por busca. Arquivos lidos/recentes têm prioridade.
SQLite mantém a transação anterior em falhas de refresh/rebuild; pause é estado
privado, sem alterar a configuração do usuário. `opcode index` e `/index` não usam inferência.
Limites de cache SQLite/parser não são uma promessa de RSS total do processo.

`memoryindex` é dono de outra projeção privada SQLite/FTS5, por hash do projeto.
`message.Service` projeta texto finalizado/resultados redigidos; `agent` projeta
findings com fontes/hashes. Falha da projeção é diagnosticada sem apagar o dado
original. Buscas de conversa confirmam que a mensagem original existe e findings
revalidam fontes. Não são evidência de leitura atual nem autorização.

Embeddings são opt-in global, por HTTP para IP loopback `/api/embed`, sem proxy,
redirect ou download de pesos; texto é redigido. Respostas, dimensões, tempo,
pool e cache são limitados. RRF combina ranks com BM25 e conserva fallback
lexical. A configuração do projeto não pode habilitar esse fluxo de dados.

Prompts têm versão, identidade única e perfis por janela/modo/protocolo. Guidance
é relido com limites por pedido. O catálogo pagina metadados; schemas opcionais
expiram, MCP/skills têm instruções/schemas lazy. Chamadas explicitamente pedidas
pelo usuário exigem recibos antes de conclusão. Isso não prova critérios semânticos.

O avaliador oferece contrato JSON comum para executáveis adapters explícitos
Opcode/Codaro/Crush. Modelo, contexto manual, modo e hash de fixture precisam
coincidir; métricas externas são identificadas como declaradas. O worker nativo
mede uso do runtime, chamadas, arquivos e recibos. Nenhum código/prompt FSL do
Crush foi incorporado. Novas dependências de produção: gotreesitter (gramáticas
selecionadas), go-gitignore e tokenizer com vocabulários embarcados. Build
CGO_ENABLED=0 continua suportado.


## Evolução comparativa, fontes e decisões

As ferramentas de leitura emitem `SourceReadReceipt` com snapshot/hash/faixa. O
runtime preserva os metadados inclusive no contexto inicial e restringe recibos
ao conteúdo realmente entregue. A guarda de citações revalida fontes locais
regulares sob uma política sem concessões adicionais, antes de concluir. Um
reparo conserva o pedido original; o binding Ollama usa `answer_with_sources`
nesse reparo, com explicação e opções limitadas de intervalos atuais separadas. `SourceCitationController`
clona a conexão por pedido sem I/O, preserva autenticação e inclui o schema na
contagem do payload. O retorno ainda passa pela guarda; nenhuma sugestão de
schema constitui prova ou permissão. A compactação conserva localizadores
históricos limitados, separados de recibos atuais.

`interaction.Questions` mantém uma decisão pendente por sessão; lotes de até
cinco itens têm IDs e validação por tipo. A TUI coleta as respostas em etapas,
preserva o composer e só envia após revisão. O contrato simples segue válido.
Respostas e cancelamento não alteram autorizações.

A descoberta inicial mantém o fluxo de metadados completo. Na TUI, um contexto
optativo solicita apenas o catálogo; a seleção consulta `/api/show` individual,
com debounce, cancelamento e identificadores contra respostas atrasadas. Os
fetchers capturam configuração na thread da UI e não publicam no registro
global. O cache visual dura o diálogo e preserva seleção/último catálogo.

A avaliação compara relatórios limitados por hash/condições/versões, com null
para métricas ausentes. Drivers Codaro/Crush usam workspaces descartáveis e
capacidades declaradas, sem inventar recibos externos. O worker de ranking
reutiliza a ferramenta protegida de busca, mede cache frio/quente e informa
a estratégia efetiva inclusive fallback lexical. Não há instalação automática
nem serviço adicional. A política `parser-v4` invalida chunks de parsers antigos;
26 perfis têm fixtures locais, com fallback e limites preservados.


A recuperação de protocolo reconhece uma resposta final Ollama malformada por
`ErrInvalidFinalAnswer`. Há no máximo uma regeneração por turno, sem texto
parcial/calls publicados e sem reenviar operações concluídas. Parsers, guardas
de citação e permissões continuam exigidos. A avaliação inclui temperatura
observada; o driver Codaro fixa zero apenas no codec público, preservando seu
transporte e execução. Comparação distingue zero de observação ausente.

## Integridade da TUI e processos LSP

O renderer da TUI é o único escritor do terminal durante a conversa. O cliente
LSP drena stderr com buffer fixo de 4 KiB, inclusive linhas enormes, sem reter
ou registrar o conteúdo. `StderrStatus()` contém apenas bytes/estado em memória.
O executável é verificado antes do wrapper de sandbox; avisos de ausência são
localizados e deduplicados por fingerprint da configuração. O fechamento é
idempotente e aguarda o processo, o transporte e o dreno.

Foco do composer depende da página, janela, diálogos, perguntas e terminal.
Atualizações assíncronas continuam sendo processadas com diálogos abertos.
Container, split e quadro raiz limitam dimensões; o rodapé reserva a última
célula em branco para evitar que erase com autowrap pendente apague seu botão.
O input cresce por linhas visuais (3–8 quando há espaço), com prévia de anexos
limitada e rascunho preservado. Busca do catálogo usa seu próprio textinput e
geometria compartilhada entre desenho e mouse. Metadados continuam canceláveis,
sem inferência automática e sem mudar autorizações.


## Responsabilidades nos caminhos de streaming e busca

A geração é dona do `message.StreamBuffer`, que acumula texto e raciocínio sem
copiar o prefixo a cada fragmento. O worker publica snapshots imutáveis em até
50 ms quando recebe novos eventos, ou a cada 4 KiB. Checkpoints duráveis ocorrem
em até 1 s quando há eventos, ou 16 KiB, e nas fronteiras de conclusão, tools,
erro e cancelamento. Não há timer independente que grave em silêncio durante
um provedor parado. O serviço de mensagens mantém o último snapshot publicado
como overlay de `Get/List`, para uma reconciliação não regredir ao checkpoint.
Uma gravação durável bem-sucedida ou exclusão remove o overlay. Se a gravação
final falhar, o progresso continua em memória; isso não é promessa de recuperação
após encerrar o processo. Snapshots não compartilham partes mutáveis com o worker.

O orçamento do provedor mede um `PreparedRequest` imutável, e o mesmo envelope é
enviado. Compactação e mudanças de histórico/ferramentas/opções exigem nova
preparação. Título, resumo e exploração seguem o mesmo contrato. Transportes e
autenticação pertencem à conexão; clones de prompt/contexto/citações preservam
esse proprietário sem misturar conexões, credenciais ou políticas TLS.

A TUI separa projeção, layout e comandos de carregamento. Renderizadores não
consultam o armazenamento: subtarefas chegam por projeções assíncronas limitadas
e protegidas por geração. Caches de mensagens/processos e turnos têm limites de
4 MiB cada; busca normalizada usa até 2 MiB, e subtarefas até 2 MiB/32 tarefas.
Troca de conversa, tema e largura invalidam as projeções correspondentes.
Atualizações estáveis reutilizam o layout; polling de terminal ocioso evita SQL
ou reconstrução de saída sem revisão nova.

O broker mantém uma fila limitada por assinante. Saturação preserva a cauda e
marca `ReconcileRequired`; não oferece entrega ilimitada de todo evento.
Forwarders usam `PublishEvent` para preservar a marca. Mensagens e sessões
recarregam estado autoritativo; permissões consultam pedidos ainda pendentes e
continuam exigindo resposta explícita para o escopo original. O servidor já
consulta snapshots vivos em seu polling. Encerrar o broker também encerra seus
waiters de assinatura, mesmo se o contexto do assinante permanecer ativo.

A busca captura candidatos e escopo sob o semáforo do índice e o libera antes
do HTTP de embeddings. O refresh possui statements preparados por transação.
Snapshots de ignore pertencem a uma operação, validam seus stamps antes do
commit/resultado e rejeitam a operação ao exceder 65.536 políticas observadas.
Caminhos sensíveis, symlinks e autorização continuam verificados por caminho.
Embeddings usam até 16 misses/64 KiB em um lote e podam o cache uma vez por lote;
resultados revalidam política e hash antes de sair. O índice continua navegação,
sem fornecer evidência de leitura para edição ou citação. Redatores preparados
pertencem à operação; a saída de `SecureTool` captura credenciais novamente.

Formulários de conexão têm handlers tipados e módulos próprios para campos,
validação, persistência, navegação e apresentação. Commit/rollback do cofre e
as permissões permanecem nas camadas que executam essas operações.
