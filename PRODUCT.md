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

Credenciais vêm de variáveis de ambiente ou configuração privada. Descobrir
modelos não executa inferência nem baixa pesos. Perguntar/Planejar não recebem
capacidades mutáveis. Executar mantém aprovação e sandbox. Aprovações não são
restauradas do disco nem concedidas por skills, hooks, MCP ou texto do modelo.

A interface deve funcionar por teclado e mouse, preservar rascunhos, manter
leitura durante streaming e comunicar carregamento, vazio, falha e recuperação.
Não há metas de latência ou paridade de qualidade entre modelos estabelecidas.

Identidade pública: **Opcode**, módulo `github.com/raulaguila/opcode`, branch
`develop`. A instalação documentada usa o checkout ou `go install ...@develop`.
Distribuições e releases só são anunciadas quando publicadas. A licença original
e a referência de proveniência ao OpenCode original são preservadas.
