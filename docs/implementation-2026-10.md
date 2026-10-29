# Registro da implementação — recuperação e contexto

Escopo: as 44 recomendações da comparação Codaro/Opcode/Crush. Permissões,
modos, sandbox, configuração e histórico originais permanecem como invariantes.
Os comportamentos de referência foram implementados independentemente; nenhum
código ou prompt FSL do Crush foi incorporado.

| Itens | Entrega concreta | Evidência |
| --- | --- | --- |
| 1–3 | Identidade Opcode, estilo útil e prompt por modo/protocolo/capacidades | prompt/prompt_test.go |
| 4–5 | Perfis full/reduced/minimal e orientação baseada nos schemas reais | prompt.Compose, plan_arguments.go, testes de protocolo |
| 6–7 | Orientação limitada relida por pedido; versão e regressões de prompt | TestProjectGuidanceReloadsWithoutCrossProjectCache, prompt.Version |
| 8–10 | Calibração por uso real; tokenizer apenas compatível; origem da janela | TestUsageCalibrationIncludesCachedInputAndKeepsUncertainty, TestTokenizerOnlyForCompatibleProviderFamily |
| 11–13 | Orçamento real completo, compactação preventiva de lotes concluídos e recibos de continuidade | TestClosedBatchContinuityPreservesConstraintsAndNewestEvidence, TestActiveTurnBudgetCompactionCanResumeWithoutLosingUserConstraints |
| 14–16 | Projeção pequena da tarefa, componentes/reservas e cache contado na entrada | taskProjection, ContextStatus, UsageMetrics |
| 17–19 | Go + nove parsers locais; chunks por declaração/escopo e inventário separado de LSP | TestLocalLanguageDeclarations: dez fixtures |
| 20–22 | Mapa estrutural de arquivos/manifests, referência textual explícita, leitura com hash e continuação de linha | TestProjectMapOutlineAndSymbolPage, TestPagedReadLongUnicodeLinesAndExternalEdit |
| 23 | Projeção FTS5 de conversas/achados por projeto, fontes e staleness | TestProjectIsolationUpdatesAndDeletion, testes de findings existentes |
| 24–25 | Embeddings locais opt-in com fallback/RRF e intent de busca | TestEmbeddingBoundsRedactionAndFallback, TestHybridRetrievalRecoversAControlledSemanticOnlyMatch |
| 26–27 | .opcodeignore e regras compartilhadas para descoberta/listagem/mapa/índice | TestDiscoveryIgnoreSnapshotAndSharedMap, TestIgnoreSymlinkCannotExposeFiles |
| 28–29 | Watcher administrado e controles CLI/TUI sem modelo | TestIndexWatcherStopsAndReconciles, TestIndexControlPauseRebuildAndBounds |
| 30–33 | Limites configuráveis, explicação de exclusão, snapshot de páginas e prioridade de arquivos | config.IndexConfig, ControlCodeIndex, TestDiscoveryIgnoreSnapshotAndSharedMap |
| 34–35 | Catálogo pesquisável/paginado e descarregamento/expiração de schemas | TestCatalogFilteringPaginationAndSchemaLease |
| 36–37 | LSP lazy por tipo/marcadores; diagnóstico sanitizado /runtime | TestLazyServerMatchingNeedsKnownTypeAndRootMarker, RuntimeDiagnostic |
| 38 | Prévia de patch, conflitos e recibos/hashes sobre transação existente | TestPatchPreviewReceiptRejectsSourceChanges |
| 39–40 | Descoberta lazy de skills/MCP e exploração com fontes/recibos e orçamento completo | TestMCPLazyCatalogDenialCancelAndApprovedPage, TestSkillCannotBeInvokedByModel, TestExploreIsSeparateReadOnlyAndUsesParentBudgets |
| 41–42 | Contrato comum de adapter Opcode/Codaro/Crush e métricas de uso/chamadas/tempo/recall/MRR | TestAdapterContractAndRetrievalMetrics; docs/evaluation.md |
| 43–44 | Regressões longas/retomada/edição externa e checagem de chamadas explícitas antes de concluir | TestActiveTurnBudgetCompactionCanResumeWithoutLosingUserConstraints, TestRequirementCountsUseOnlyCurrentRunSuccessfulReceipts, TestExplicitRequirementsNeverTurnNegationIntoAnAction, teste real composto |

## Validação observada

- Suíte comum completa com `go test -race -p 1 ./...`: passou. Ajustes finais de
  contexto/retomada passaram novamente nos pacotes afetados.
- `go vet ./...`, `git diff --check`, schema e avaliação offline: passaram.
- Build Linux amd64 `CGO_ENABLED=0 go build -trimpath`: passou.
- Ollama real `llama3.1:8b`: primeira pergunta com contexto automático 131.072 e
  manual 16.384, quatro turnos retomados e um pedido composto: passaram.
- PTY real: passou após corrigir a visibilidade da prévia durante execução.
  Aprovou pela TUI, mostrou saída ao vivo, aceitou entrada humana e instrução
  concorrente, manteve a ordem no histórico e encerrou com exit 0.
- CLI offline em diretório escolhido: update/status/pause/resume/rebuild/clear
  preservaram fonte e JSON, sem provedor/modelo selecionado.
- As seis tarefas gerais foram repetidas com os critérios originais: **3/6**
  passaram nesta execução. O relatório sanitizado está em
  [evidência](evidence/2026-10-28-runtime-roadmap.json).

## Limites da conclusão

Dez fixtures não equivalem à cobertura de toda sintaxe de uma linguagem;
Codaro mantém mais gramáticas. Busca semântica foi validada com serviço HTTP e
vetores controlados (recall@1 de 0 para 1 nessa fixture), sem medir qualidade de
um modelo real de embeddings ou baixar pesos. Schemas não conferem aprovação.

O benchmark tem contrato/normalização para drivers explícitos das versões
instaladas dos três aplicativos. Não instala outros aplicativos, não altera
seus repositórios e não certifica a política declarada por um driver externo.
Não foi realizada comparação real de qualidade entre os três. Requisitos de
chamadas explícitas são verificáveis; condições semânticas gerais continuam
exigindo critérios, checks e revisão humana. Esses limites não são escondidos
por um estado de conclusão automático.

Detalhes operacionais: [Índice e recuperação](retrieval-runtime.md),
[Contexto e extensões](context-and-extensions.md) e [Avaliação](evaluation.md).
