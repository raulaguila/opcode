# Revisão da janela de adicionar provedor

Revisão de 29/10/2026 do formulário TUI, com inspeção do código e testes de
geometria/foco. O fluxo geral de conversa foi exercitado em PTY com Ollama real;
o TLS foi validado contra servidores HTTPS de teste com certificado autoassinado.
Isso não constitui teste de usabilidade com usuários nem certificação de
acessibilidade. Contraste, leitores de tela e todas as configurações de terminal
não foram medidos.

## Avaliação

| Dimensão | Estado | Próximo ganho |
| --- | --- | --- |
| Hierarquia e legibilidade | Formulário básico separa campos técnicos; nomes e estados ainda se misturam | Agrupar identificação, endereço, autenticação e segurança |
| Descoberta e navegação | Presets e ações visíveis ajudam; atalhos e campos avançados exigem aprendizagem | Instrução contextual por campo e avanço claro para escolher o modelo |
| Feedback e recuperação | Há rascunho, cancelamento, teste e aviso de alterações | Erros junto ao campo e diagnóstico de conexão por etapa |
| Acessibilidade no terminal | Teclado, rolagem do campo focado e booleanos selecionáveis | Eliminar conflito de atalhos e testar terminais/leitores de tela variados |
| Consistência e linguagem | Interface PT/EN e escolhas tipadas existem | Completar tradução de mensagens e ocultar termos de implementação |

## Melhorias incluídas nesta mudança

- Preset **OpenAI compatible** com endereço personalizável, sem exigir chave
  quando o servidor não usa autenticação.
- Controle **TLS inseguro** desligado por padrão, com aviso quando habilitado.
  Só afeta a conexão explicitamente configurada.
- IDs livres para novas conexões do mesmo preset, evitando sobrescrever uma
  conexão anterior ao adicionar outra.
- Exemplos no campo de URL e escolha explícita de autenticação: chave direta
  mascarada, variável de ambiente ou sem autenticação. Apenas o campo escolhido
  é exibido. Chaves diretas usam `apiKey` no JSON privado, sem copiar valores do ambiente.
- Formulário com aviso de TLS continua dentro de 60×20 no teste de geometria.

## Recomendações por prioridade

1. **Alta — estado da credencial de ambiente.** A escolha explícita de chave
   direta/ambiente/nenhuma já foi implementada. O próximo ganho é mostrar o
   estado definido/ausente da variável, sem exibir seu valor. Evidência:
   [`edit` e formulário](../internal/tui/components/dialog/providers.go).
2. **Alta — erro no campo responsável.** Ao salvar uma URL, enum ou número
   inválido, focar e rolar até o campo, mantendo todos os valores preenchidos.
   Hoje `readProfile`, `test` e `save` devolvem o erro em uma nota geral, sem
   direcionar o foco. Evidência: [validação do formulário](../internal/tui/components/dialog/providers.go).
3. **Alta — resultado de teste por etapa.** Distinguir URL inválida, falha TLS,
   autenticação recusada e catálogo vazio/indisponível. Oferecer uma correção
   concreta sem ativar TLS inseguro automaticamente. Separar “catálogo
   acessível” de “modelo suporta ferramentas”; o teste de inferência permanece
   uma ação explícita. Evidência: [`TestProviderProfile`](../internal/config/profiles.go).
4. **Média — configuração em sequência.** Após salvar, destacar “Escolher modelo”
   e mostrar conexão/modelo ativos. Separar claramente Salvar, Consultar catálogo
   e Salvar e ativar, mantendo a etapa de escolha já existente. Ao voltar,
   preservar rascunhos. Evidência: [fluxo de configuração](../internal/tui/components/dialog/experience.go).
5. **Média — vocabulário do usuário.** Mostrar “API compatível com OpenAI” no
   formulário e reservar `local`, backend e protocolo para Mais. `local` também
   atende gateways remotos; esse nome técnico pode induzir a uma escolha errada.
   Preservar os identificadores JSON para compatibilidade.
6. **Média — atalhos durante edição.** `Ctrl+A` abre os campos avançados, mas é
   usado para início/seleção de texto em vários ambientes. Mover “Mais” para um
   atalho sem conflito, mantendo ação visível, e exibir ajuda apenas do campo
   focado. Evidência: [roteamento de teclas](../internal/tui/components/dialog/providers.go).
7. **Média — tradução completa.** Revisar notas de teste, erros de validação e
   termos técnicos, inclusive estados com TLS habilitado. Manter nomes de
   protocolos e variáveis literais, mas traduzir as instruções ao redor.
8. **Baixa — resumo antes de ativar.** Mostrar nome, origem HTTPS/HTTP,
   autenticação e validação TLS em uma linha de resumo. Para URL longa, permitir
   consultar o endereço completo sem aumentar permanentemente o formulário.

Estas recomendações são trabalho futuro. A alteração atual não redesenha os
fluxos de autorização nem altera os critérios de validação de citações.
