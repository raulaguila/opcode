# Validação da versão inicial do Opcode

Os testes Go determinísticos verificam protocolo de provedores, permissões,
transações, persistência, contexto, extensões e fluxos da interface. As avaliações
com um modelo real medem outra coisa: se esse modelo consegue cumprir os
critérios de uma tarefa usando a aplicação. Um resultado não substitui o outro.

## Ollama real — 28/10/2026

Modelo instalado: `llama3.1:8b`, API nativa do Ollama em loopback, janela
configurada de 16.384 tokens e reserva de resposta de 2.048 tokens. Os testes
usaram workspaces descartáveis; comandos permaneceram no Bubblewrap e passaram
pelas autorizações previstas pelo teste.

O fluxo completo de terminal passou: aprovação na TUI, comando real com PTY,
saída parcial ao vivo, entrada humana e instrução adicional enquanto o comando
aguardava entrada. O modelo seguiu a instrução posterior e a ordem de chamadas,
resultados e mensagens foi preservada no histórico.

Uma execução das seis fixtures do [avaliador](evaluation.md) resultou em:

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
