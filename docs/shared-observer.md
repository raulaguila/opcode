# Observar uma sessão em outra janela

`opcode watch` lê um banco existente sem criar agente, carregar configuração,
iniciar provedor, LSP/MCP, executar ferramentas ou migrar o banco. A instância
original continua sendo a dona da execução e das permissões.

```sh
# Listar uma página das conversas recentes:
opcode watch --database /caminho/privado/opcode.db

# Consultar mensagens salvas de uma conversa:
opcode watch --database /caminho/privado/opcode.db --session ID

# Acompanhar mudanças; Ctrl+C encerra somente o observador:
opcode watch --database /caminho/privado/opcode.db --session ID --follow

# Snapshots em JSON por linha, adequados para outro cliente:
opcode watch --database /caminho/privado/opcode.db --session ID --follow --format json
```

O caminho é explícito; consulte a configuração da instância dona para localizar
seu banco. Arquivo ausente, diretório ou link simbólico são recusados. SQLite é
aberto com `mode=ro` e `query_only=1`; nenhuma inicialização ou migração ocorre.
Um banco sem o schema esperado informa erro, em vez de ser alterado para tentar
corrigi-lo.

A página começa pelas entradas mais recentes. `--limit` aceita 1–50 (padrão 20)
e `--offset` aceita 0–10000. O campo `next_offset` no JSON e a indicação `More` no
texto permitem consultar outra página. O cursor é um offset do snapshot: se a
instância dona inserir mensagens entre consultas, as posições podem mudar.
Cada snapshot usa uma transação de leitura para manter suas consultas coerentes.
Em `--follow`, a página escolhida é consultada a cada segundo; somente mudanças
no conteúdo exibido publicam um novo snapshot. A conexão fecha ao terminar ou
cancelar, e cada consulta tem prazo de três segundos.

Somente texto de mensagens humanas e de mensagens de assistente já encerradas é
exibido. Não são exibidos raciocínio separado, ferramentas, argumentos,
resultados, anexos ou estado privado do provedor. Mensagens de reparo com origem
`application` são omitidas. O observador não promete streaming token a token nem
mostra a fila de autorizações. Os rótulos de modo/estado da tarefa são consultados
quando o schema correspondente existe; não são prova de conclusão ou validação.

O texto é literal: sequências ANSI são removidas e outros controles são escapados.
Credenciais conhecidas do processo (variáveis de ambiente ou configuração já
carregada) são redigidas pelo mesmo mecanismo da aplicação. Isso não detecta todo
segredo arbitrário que possa ter sido digitado na conversa; trate o banco e a saída
como dados privados e autorize o acesso de quem acompanha a janela.

Limites por snapshot: até 50 mensagens, 16 KiB de texto por mensagem e 128 KiB de
texto no conjunto. Partes serializadas acima de 256 KiB não são carregadas; o item
indica a omissão. Truncamento/página seguinte são explícitos. Mensagens omitidas
por origem ou tipo não ganham conteúdo substituto como se fossem texto humano.

Esse modo fornece acompanhamento local somente leitura. Não é um backend
colaborativo como o workspace compartilhado do Crush: não envia prompts, não
responde perguntas, não aprova ferramentas, não controla processos nem assume a
execução depois que a instância original fecha. Abrir outro observador não
transfere nem amplia autorizações.

Os testes `cmd/watch_test.go` cobrem arquivo ausente sem criação, SQL de escrita
recusado, projeção/redação/controles, paginação, partes grandes, publicação somente
após mudança, cancelamento e execução sem inicializar configuração.
