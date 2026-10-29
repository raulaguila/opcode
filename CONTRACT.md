<!-- bridge:contract-schema 1 -->
# Contrato da evolução do agente

## Status

accepted

Origem: pedido do usuário de implementar todos os itens da comparação de
28/10/2026. O usuário autorizou prosseguir com a implementação.

## Surfaces

- Provedores/perfis: JSON e formulário TUI; catálogo assíncrono cancelável,
  favoritos/recentes, metadados e seleção persistente. Primeiro acesso abre
  configuração; cadastros existentes continuam válidos.
- Configuração em pasta: novos arquivos globais usam `~/.opcode/config.json` e
  overrides de projeto usam `.opcode/config.json`. Sem global compatível, o
  startup normal cria um template privado, sem credenciais/inferência e sem
  sobrescrever arquivos. Diagnóstico offline/configuração explícita não o criam.
  Formatos anteriores permanecem como fallback em seus próprios caminhos; criar
  a pasta de configuração não pode esconder um banco anterior.
- Chat: processo recolhível por turno, resposta separada, copiar/abrir citação,
  notificações de novas mensagens, histórico e fila inspecionável/editável.
- Contexto: contabilizar payload e schemas antes de gerar; compactação preserva
  pares de ferramentas, instruções e pedido atual; status explica estimativas.
- Revisão: visualizar alterações e verificações; undo/redo por turno confere
  conteúdos atuais e pede autorização; efeitos de comandos não são revertidos.
- Sessões: buscar, renomear, arquivar e exportar; erros oferecem recuperação.
- Ferramentas: mapa/outline/símbolo, artefatos paginados, descoberta de checks,
  relatório, hierarquia/renomeação/substituição LSP e recursos MCP.
- Extensões: skills e hooks explícitos, limitados e sem concessão de permissão.
- Avaliações: fixtures determinísticas e opt-in real Ollama sem registrar chaves.

## APIs

Serviços Go/Bubble Tea, sem novo serviço HTTP. Compatibilidade preservada nas
interfaces legadas; capacidades adicionais podem usar interfaces opcionais.

- QueueControl: QueuedInstructions(sessionID) []QueuedInstruction;
  TakeInstruction(sessionID, instructionID) (QueuedInstruction, error).
  QueuedInstruction: ID, Content e Attachments. Retirar é atômico; enviar
  novamente exige o fluxo normal de Submit/ACK.
- Perfis guardam o backend separado do identificador/nome. Modelos podem incluir
  provider_profile; modelos legados continuam usando providers[Provider].
- Ferramentas retornam ToolResponse, IsError e JSON limitado. Novas ferramentas
  passam por SecureTool e a seleção por modo.
- Arquivos: uma observação de commit recebe Before/After e caminhos; falha no
  registro deve fazer rollback. Undo/redo reutiliza a transação protegida.

## Auth

A configuração de segurança global mantém precedência sobre projetos. Variáveis
resolvidas não são serializadas. Hooks não podem autoaprovar ferramentas. MCP
mutável e alterações LSP exigem aprovação em Executar. Dados recuperados nunca
valem como evidência atual de leitura nem concedem permissões.

## Errors

Distinguir configuração, autenticação, indisponibilidade, limite de contexto,
conflito de arquivo, recusa e cancelamento. Resultados atrasados são descartados;
falhas preservam o modelo ativo, rascunho e tarefa quando aplicável.

## Mocks

Mocks apenas nos testes. Testes TUI usam serviços reais quando viável e provedores
controlados; testes HTTP usam transports locais. Runtime de produção não tem
fallback de resposta simulada. Ollama real é opt-in e usa projeto temporário.

## Non-goals

Não copiar implementação/licença de outros projetos. Sem instalar modelos,
ativar integrações não cadastradas, publicar releases, fazer push ou conceder
permissão automaticamente. Busca vetorial não é necessária para o mapa textual.

## Open questions

Nenhuma informação adicional é necessária para iniciar as frentes autorizadas.

## Segunda evolução — comparação e experiência diária

Status: accepted. Origem: pedido explícito “Faça tudo” após a nova comparação.
Esta seção amplia os limites anteriores (inclusive o antigo non-goal de HTTP).

- Conclusão: pedidos de implementação em Executar não podem terminar com sucesso
  sem alteração verificável ou justificativa explícita; trabalho parcial continua
  parcial. Recibos continuam vinculados ao conteúdo validado.
- Recuperação: distinguir autenticação, indisponibilidade, contexto/memória,
  cancelamento e conflitos. Contexto local aprende limites por conexão/modelo,
  com validade e opção de recalibrar, sem repetir ferramentas mutáveis.
- Diagnóstico: `opcode doctor` funciona sem modelo configurado; consulta de
  catálogo e teste real com ferramentas são operações distintas e optativas.
- Interface: lateral automática/opcional, input adaptável, estados e fila claros,
  diff unificado/lado a lado/tela cheia, preferências pesquisáveis, atalhos e
  notificações opcionais. Configuração mostra origem e valor efetivo redigido.
- Ferramentas: índice incremental privado com busca ranqueada; exploração
  separada somente leitura e limitada; formatação e checks por projeto.
- Integrações: OAuth MCP explícito com PKCE/estado, credenciais privadas,
  cancelamento e revogação; nada é conectado ou autenticado automaticamente.
- Workspaces: criação/listagem/remoção segura de worktrees, sem reset/force;
  comandos do agente continuam sujeitos às permissões e ao sandbox.
- Servidor: execução local optativa, autenticação obrigatória, limite de corpo,
  propriedade de sessões/processos e encerramento/cancelamento. Reconexão usa
  snapshots limitados; não reexecuta comandos nem restaura permissões do disco.
- Entrega: binários/checksums geráveis e documentação reproduzível; publicar
  release ou fazer push não é requisito desta etapa.

Mocks continuam exclusivos dos testes. APIs novas precisam de testes de recusa,
limites, cancelamento e preservação de rascunho/estado quando aplicável.

## Identidade e entrega

O produto e binário públicos usam Opcode/opcode; o módulo é
`github.com/raulaguila/opcode` e a branch principal deste fork é `develop`.
Arquivos locais anteriores não são renomeados por uma troca de identidade do
código. A licença original permanece intacta. Métodos de instalação publicados
não devem apontar a owners ou pacotes do projeto anterior. CI testa o fork e
releases são artefatos futuros, disparados somente por tags explícitas.

Contratos explícitos de tarefa são dados definidos pelo usuário, separados de
permissões e de planos gerados pelo modelo. Checks declarados exigem recibos
atuais; alvos/critérios não provam semântica nem liberam caminhos. Bindings
simplificados de planos locais preservam a validação do estado. Uma mudança sem
checks disponíveis deve permanecer parcial. Backups de configuração pertencem
à área privada `.opcode`, inclusive quando o arquivo original é legado.

## Evolução comparativa — 28/10/2026

### Correção UI/UX autorizada — 28/10/2026

Aceite adicional: saída de LSP/subprocessos não escreve diretamente no terminal
enquanto a TUI está ativa; mensagens técnicas são limitadas e não vazam segredos.
Um quadro completo respeita largura/altura, apresenta um único prompt e rodapé,
preserva rascunho/rolagem em resize e tem apenas um foco visível. O composer
reserva três linhas úteis quando há espaço e cresce até oito; telas pequenas,
texto colado, anexos e Unicode mantêm limites e navegação.

Seletor de modelos: busca com cursor, modelo atual distinto do destaque, posição
na lista, nomes legíveis, detalhes sob demanda, cancelamento e catálogo estável.
Modos têm título próprio; idioma configurado é consistente; contraste auxiliar
e mensagens de LSP indisponível são legíveis e acionáveis. Durações usam unidades
corretas. Permissões, sandbox, gates e validação de fontes permanecem obrigatórios.

Mocks comprovam transições e erros determinísticos. A aceitação em PTY real usa
Ollama local com aprovação humana simulada pelos controles normais, comandos,
entrada/novas instruções, resize, seletores e LSP ausente. Comparações capturam
quadros sem credenciais. Aceite final: suíte race, vet, build, evidência live,
documentação atualizada, commit e push normal para develop (sem force).

O usuário autorizou implementar as recomendações, priorizando avaliação, citações e recuperação. Os contratos anteriores permanecem aplicáveis.

### Superfícies e resultados observáveis

- Avaliação: adapters executáveis para Opcode/Codaro/Crush, metadados de versão,
  comparações repetidas com critérios idênticos, relatório de resultados e medidas
  ausentes explícitas. Execução externa opt-in, limitada ao workspace descartável.
- Citações: caminhos e intervalos precisam corresponder a leituras reais atuais;
  referências inválidas recebem reparo limitado. Preferências explícitas do usuário
  continuam respeitadas; referência válida não comprova interpretação semântica.
- Recuperação: preservar instruções, pares de chamadas/resultados e acesso às
  fontes ao compactar; avaliar ranking lexical/híbrido e regressões sem inventar
  ganho de embeddings ou gastar contexto com árvores completas.
- Linguagens: ampliar parsers locais e fixtures, mantendo fallback textual e
  distinguindo parser, LSP instalado/configurado e checks do projeto.
- Perguntas: contratos aditivos para lotes, seleção única/múltipla e texto livre;
  apresentação progressiva no composer preservando rascunho e cancelamento.
- Modelos: busca aproximada, seleção estável e metadados sob demanda com
  cancelamento; erros preservam catálogo utilizável. Sem inferência automática.
- Fluxo diário: modo/modelo/atividade no rodapé; plano, terminal e detalhes sob
  demanda. Diagnóstico orienta configuração de linguagens sem instalar ferramentas.
- Prompts: versão e orçamento observáveis na avaliação; variantes comparadas com
  o mesmo conjunto, sem transferir garantias de segurança para instruções textuais.

### Auth

Permissões, sandbox, hashes e checks por revisão permanecem obrigatórios.
Perguntas, índices, adapters, skills e respostas do modelo não concedem acesso.
Configuração e contratos existentes continuam válidos; novos campos são aditivos.
Cancelamento encerra trabalho pendente sem executar efeitos novamente. Relatórios
não incluem credenciais nem conteúdo privado completo.

### Mocks e validação

Testes determinísticos usam doubles explícitos, separados de avaliações live.
Live usa somente modelos/provedores presentes e autorização opt-in do runner.
Falhas, indisponibilidade e métricas não observadas são registradas como tais.
Critérios originais da suíte permanecem; novos critérios e casos são aditivos.
Aceite: testes direcionados, suíte Go com race, vet e build; fluxos Ollama reais
quando acessíveis. Documentação acompanha comportamento e limitações.

### Fora do escopo

Não copiar código do Crush, instalar toolchains/modelos automaticamente, mudar
permissões padrão, declarar superioridade geral ou publicar resultados fabricados.
Servidor multiworkspace compartilhado fica para uma evolução específica.

### Questões abertas

Nenhuma impede a implementação. A disponibilidade local de Codaro/Crush e Ollama
será verificada antes de selecionar quais comparações live podem ser executadas.


## Evolução de 29/10 — eficiência e conveniência


Existing permissions, citation/completion validation, declared evaluation criteria and credential redaction remain authoritative.

- Usage diagnostics add aggregate phase durations in milliseconds: context_ms, provider_ms, tools_ms, first_token_ms (sum), first_token_samples, and repairs. No prompts, arguments, source contents or credentials in metrics. Existing counters remain compatible. Expose in runtime diagnostics/evaluation where existing usage is shown. Durations are observations, not speedup claims.
- Language inventory expands only with local bundled grammars and meaningful declaration/index fixtures. Unsupported grammars remain text fallback and cannot be advertised as fixture verified. Increment index policy version when structural extraction changes.
- Model reasoning choices come from explicit model metadata, never arbitrary names. Empty metadata offers no editable control. Persist selected effort on the existing agent config; changes apply through normal provider reconstruction, are refused during activity, and never grant permission.
- Clipboard copy uses native backend locally; SSH uses bounded OSC 52 only on explicit copy, reports unconfirmed delivery, and retains manual selection. Never invoke remote clipboard helper or interpolate copied text into shell commands.
- UI matrix verifies actual routed modal interactions and compact geometries, not just isolated appearance. Provider fixtures cover streaming, cancellation, usage and tool calls without real credentials. Real provider testing uses installed local Ollama only unless separately configured by user.
- Theme personalization uses validated literal color data, explicit save, reversible preview, and existing private config paths. Shared-client work must preserve one execution owner and per-request authorization; never share permission approval silently across independent agent processes.

Validation: focused regressions, go test -race ./..., go vet, CGO-disabled build, PTY Ollama acceptance. Comparative benchmarks use unchanged criteria, isolated fixtures, identical requested model/context/reserves; report unavailable applications explicitly. No inferred superiority.
