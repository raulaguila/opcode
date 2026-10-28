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

## Identidade e entrega

O produto e binário públicos usam Opcode/opcode; o módulo é
`github.com/raulaguila/opcode` e a branch principal deste fork é `develop`.
Arquivos locais anteriores não são renomeados por uma troca de identidade do
código. A licença original permanece intacta. Métodos de instalação publicados
não devem apontar a owners ou pacotes do projeto anterior. CI testa o fork e
releases são artefatos futuros, disparados somente por tags explícitas.
