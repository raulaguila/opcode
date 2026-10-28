# Servidor local, reconexão e OAuth MCP

## Manter uma execução enquanto o cliente desconecta

Compile o binário e configure um segredo de servidor no ambiente dos dois
terminais. Por exemplo, no mesmo shell:

```sh
export OPCODE_SERVER_TOKEN="$(openssl rand -hex 32)"
opcode serve --cwd /caminho/do/projeto --listen 127.0.0.1:4096
```

O segredo tem 32–512 caracteres sem espaços e não é impresso. Em outro terminal,
com a mesma variável configurada:

```sh
opcode attach --url http://127.0.0.1:4096
# Reconectar à sessão cujo ID foi mostrado:
opcode attach ID_DA_SESSAO --url http://127.0.0.1:4096
# Snapshot JSON sem acompanhar a conversa:
opcode attach ID_DA_SESSAO --once
```

O cliente é uma interface de linhas, distinta da TUI completa. Texto envia uma
instrução; `/help` lista controles. `/terminal comando` inicia PTY após aprovação;
`/input ID texto` envia uma linha; `/key ID enter|ctrl-c|tab` envia uma tecla.
`/approve ID`, `/allow-session ID` e `/deny ID` respondem a pedidos pendentes.
A autorização de sessão cobre os mesmos argumentos exibidos, não todos os
comandos do diretório. `/answer ID texto` responde a uma pergunta real.
`/cancel` interrompe o agente/processos e revoga as permissões da sessão.
`/quit` apenas desconecta; não encerra o servidor.

O servidor mantém processos e aprovações em memória até encerramento. Reconectar
obtém snapshots e não reexecuta ferramentas. Encerrar o servidor cancela os seus
pedidos, revoga grants e fecha processos; um novo servidor pode retomar o
histórico da conversa, mas não recupera um PTY encerrado nem aprovações antigas.
Somente sessões com estado de workflow pertencente ao workspace podem ser
anexadas. Não é um servidor multiusuário: os clientes com o mesmo segredo são o
mesmo usuário autorizado.

## API v1

Use `Authorization: Bearer ...` e `Content-Type: application/json` nas escritas.
O servidor recusa origens de navegador, credenciais na URL, bind externo e
clientes sem autenticação. A API não serve páginas web.

| Método e caminho | Operação |
| --- | --- |
| `GET /v1/health` | Estado e workspace |
| `GET /v1/sessions?limit=50&offset=0` | Sessões do workspace, máximo 200 por página |
| `POST /v1/sessions` | Criar com `title` ou anexar com `resume` |
| `GET /v1/sessions/ID` | Últimas 50 mensagens textuais, tarefa, fila, perguntas, permissões e processos |
| `POST /v1/sessions/ID/prompt` | `content` e `request_id` opcional |
| `POST /v1/sessions/ID/cancel` | Cancelar e revogar grants |
| `POST /v1/sessions/ID/mode` | `mode`: ask/plan/execute |
| `POST /v1/sessions/ID/permissions/PERMISSAO` | `decision`: once/session/deny |
| `POST /v1/sessions/ID/answer` | `id` da pergunta e `text` |
| `POST /v1/sessions/ID/processes` | `command`, `pty`, `timeout` em ms, `cols`, `rows` |
| `GET /v1/sessions/ID/processes/PROCESSO` | Saída e tela limitadas/redigidas |
| `POST .../PROCESSO/input` | `data`, sujeito ao modo Execute e dono da sessão |
| `POST .../PROCESSO/resize` | `cols` e `rows` |
| `POST .../PROCESSO/stop` | Encerrar um processo da própria sessão |

Limites: 64 KiB por corpo, 16 requests simultâneos, 64 sessões anexadas e os
limites existentes do ProcessManager (quatro processos ativos, 32 snapshots).
Mensagens acima de 128 KiB são identificadas como truncadas; anexos binários e
continuação opaca do provedor não são exportados. Requests de prompts com o mesmo
`request_id` e conteúdo são deduplicados durante a vida do servidor, até 128 IDs
por sessão; reutilizar um ID com outro conteúdo é recusado. O cliente serializa
o envio de instruções. Não repita automaticamente requests de criação de
processos depois de uma falha de conexão: reconecte e examine o snapshot.

## Autenticar um MCP remoto

O JSON guarda parâmetros públicos e referências a variáveis, nunca os tokens:

```json
{
  "mcpServers": {
    "documentos": {
      "type": "http",
      "url": "https://mcp.exemplo.com/mcp",
      "oauth": {
        "clientID": "cliente-previamente-registrado",
        "scopes": ["read"],
        "redirectPort": 53682
      }
    }
  }
}
```

Omitir `clientID` solicita registro dinâmico quando o servidor o oferece.
`clientSecretEnv` referencia o segredo de um cliente confidencial. Omitir
`redirectPort`, ou usar zero, escolhe uma porta loopback livre; clientes
pré-registrados podem exigir a porta fixa cadastrada no provedor.

```sh
opcode mcp login documentos --cwd /caminho/do/projeto
opcode mcp status documentos --cwd /caminho/do/projeto
opcode mcp logout documentos --cwd /caminho/do/projeto
```

Login mostra uma URL para abrir manualmente e aguarda até cinco minutos. A
resposta usa estado aleatório, PKCE S256 e verificação do emissor antes da troca
do código. O callback aceita apenas o host/porta local previstos. OAuth requer
HTTPS; HTTP só é aceito para IP loopback. Redirects HTTP não são seguidos pelo
cliente que porta o token. Descoberta e refresh usam a biblioteca MCP existente,
com timeout, limite de resposta e identificação do recurso/emissor.

Tokens e credenciais de registro são gravados atomicamente em arquivos 0600
no diretório de configuração do usuário, em `opcode/auth`, separados por hash
dos parâmetros da conexão. Não são serializados no JSON de projeto. Logout
remove a cópia local e invalida refreshes de gerações anteriores. Ele não promete
revogar uma autorização remota: use as configurações da conta do provedor para
isso. Login não conecta ferramentas automaticamente, não inicia servidores
stdio e não substitui as permissões de execução.

Referência de protocolo: [autorização MCP](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization).
