<!-- bridge:product-schema 1 -->
# Opcode — evolução do fork

Agente de desenvolvimento em Go, usado no terminal para perguntar, planejar e
executar trabalho em repositórios locais. O usuário utiliza Ollama com
`llama3.1:8b`; provedores remotos também são suportados.

O pedido aceito é implementar as oito frentes da comparação com Codaro/Crush:
configuração acessível, chat organizado e fila editável, contexto preventivo,
revisão/undo/redo, sessões e recuperação, ferramentas avançadas, skills/hooks e
avaliações repetíveis. A implementação deve reutilizar a arquitetura existente,
preservar permissões e isolamento, e manter a suíte de testes passando.

A segunda evolução adiciona conclusão verificável, diagnóstico sem inferência,
contexto adaptativo, configurações com origem, interface compacta, diff adaptável,
índice incremental e exploração limitada. Formatação, worktrees e OAuth MCP
continuam explícitos e autorizados. Um servidor loopback optativo mantém sessões
e PTYs vivos entre conexões de clientes autenticados; permissões não são
persistidas, e encerrar o servidor encerra seus processos.

As novas configurações globais usam `~/.opcode/config.json`; o override de
projeto é `.opcode/config.json`. Sem arquivo global compatível, a primeira
inicialização normal cria um template de provedores/preferências, sem rede,
credenciais ou modelo escolhido. Arquivos antigos continuam sendo usados no
próprio caminho e nunca são sobrescritos pelo template.

Credenciais vêm de variáveis de ambiente ou configuração privada. Descobrir
modelos não executa inferência nem baixa pesos. Perguntar/Planejar não recebem
capacidades mutáveis. Executar mantém aprovação e sandbox. Aprovações não são
restauradas do disco nem concedidas por skills, hooks, MCP ou texto do modelo.

A interface deve funcionar por teclado e mouse, preservar rascunhos, manter
leitura durante streaming e comunicar carregamento, vazio, falha e recuperação.
Não há metas de latência ou paridade de qualidade entre modelos estabelecidas.

A conversa mantém a ordem das mensagens públicas e intercala grupos consecutivos
de ferramentas recolhíveis. Diagnósticos permanecem visíveis após perguntas,
respostas humanas e reparos internos, com estado de citação associado ao texto.
Uma falha de citação tem categoria própria e não indica contexto esgotado.
Correções diretas de referências são versões da mesma resposta. O original
continua visível durante a revisão; somente uma correção validada vira principal,
com a versão anterior recolhida e acessível por clique ou `Alt+V`. Falhas mantêm
o original e seu aviso. Perguntas, mudanças de tarefa e mensagens independentes
não são agrupadas como revisões por proximidade ou conteúdo.
Perguntas usam painel com borda, superfície distinta, estado de espera e seleção
explícita, preservando a distinção entre responder e autorizar uma ferramenta.
O painel tem borda reta e divisória acima dos controles; o estado de espera
permanece no rodapé. O chat renderiza títulos sem marcadores Markdown, com
ênfases neutras no tema Opcode e linhas de listas alinhadas depois do marcador.
A resposta livre é a última opção selecionável; o foco indica se será enviada
uma sugestão ou o texto, sem um rascunho inativo substituir a escolha. Após o
envio, pergunta e resposta ficam juntas, com rótulo e pipe na cor de destaque,
inclusive ao reabrir a conversa e sem depender somente de cores.
`/quit` usa o encerramento protegido; `--resume` sem ID reabre a última conversa
disponível do projeto, sem continuar ações ou restaurar aprovações. A sintaxe com
ID e o comando `/resume` para continuar tarefas permanecem disponíveis.

A estabilidade visual inclui um único composer/rodapé, input de três a oito
linhas conforme o espaço, foco exclusivo dos diálogos e catálogo com busca,
modelo ativo e paginação visíveis. Redimensionar ou consultar seletores preserva
o rascunho. Erros de LSP são avisos curtos e acionáveis; stderr bruto não chega
ao terminal. Durações de respostas usam os timestamps em segundos armazenados.

Identidade pública: **Opcode**, módulo `github.com/raulaguila/opcode`, branch
`develop`. A instalação documentada usa o checkout ou `go install ...@develop`.
Distribuições e releases só são anunciadas quando publicadas. A licença original
e a referência de proveniência ao OpenCode original são preservadas.

O fluxo diário inclui `/setup`, `/contract`, `/local-status` e `/worktrees`,
com configuração offline pela CLI. Resultado parcial é distinto de conclusão.
A validação de fixtures mede seus critérios declarados, não uma taxa geral de
qualidade do modelo. Ver [guia diário](docs/daily-workflow.md).


A evolução de recuperação/contexto inclui prompts por modo/protocolo/janela,
compactação de lotes concluídos, estimativas calibradas e origem de contexto,
parsers locais em 26 perfis de linguagens, mapa estrutural, leitura/hash/páginas estáveis,
memória pesquisável por projeto, índice administrável e descoberta compartilhada.
Embeddings locais são opcionais e não baixam modelos; o LSP inicia sob demanda.
Controles avançados ficam em painéis `/index`, `/runtime` e `/context` para manter
o chat compacto. Recibos validam ações explícitas e patches podem ser pré-visualizados.
A avaliação compartilhada diferencia medidas nativas de declarações de adapters;
não há conclusão de que o Opcode supera Codaro/Crush em qualidade geral.

A evolução comparativa acrescenta drivers de avaliação Codaro/Crush, comparação
de condições e métricas ausentes explícitas, além de medição do ranking sem
inferência. Citações são conferidas contra caminho, intervalo entregue e hash
atual; leituras iniciais preservam seus recibos. Isso não comprova a interpretação
semântica da resposta. Perguntas agrupadas aceitam seleção múltipla, texto livre
e revisão antes de enviar. O seletor usa busca aproximada, preserva seleção e
catálogo e consulta metadados individuais com cancelamento, sem inferência.
`doctor --languages` orienta configuração de LSP sem instalar/iniciar servidores.

A revisão de usabilidade separa reparos da aplicação de mensagens humanas,
expõe parâmetros em permissões genéricas, reserva diagnósticos técnicos para
`/error` e separa atalhos de edição e navegação. O catálogo prioriza atual,
favoritos e recentes; a paleta usa o estado da tarefa. Formulários oferecem
opções tipadas e argumentos por linha. `/task` reúne o trabalho e `/context`
oferece acesso ao orçamento, índice, alocação e diagnóstico. A apresentação tem
prosa limitada, ações visíveis, tempo decorrido, prévia de temas e notificações
optativas. Essas mudanças não concedem capacidades nem alteram os gates de
citação, conclusão ou permissão. Ver [interface](docs/workspace-ui.md).

As aprovações começam em Recusar e aceitam Esc ou clique no botão; o escopo
continua exato. O editor externo devolve um rascunho. Conexões mostram seu estado
ativo, e catálogos não são apresentados como testes de qualidade. Planos podem
ser ajustados ou continuados após confirmação explícita de modo. Painéis têm
navegação de volta, contexto começa pelo resumo e sessões mostram recência e
prévia humana. Código e tabelas usam a largura disponível sem alargar os blocos
de prosa da mesma resposta. Temas claro/escuro e NO_COLOR mantêm marcadores de
seleção visíveis. Ver os limites e evidências em docs/evidence.

Os diagnósticos avançados registram preparação, reutilização de fontes e latência
até o primeiro conteúdo sem guardar os textos. Conversas com o mesmo modelo local
para título e resposta recebem um título curto da primeira mensagem, evitando uma
inferência auxiliar. O usuário pode selecionar um modelo separado para títulos.
