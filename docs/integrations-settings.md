# Integrações na interface

Abra **Settings** e pressione `I` para ver servidores MCP e de linguagem. `N`
adiciona MCP, `L` adiciona LSP e `Enter` edita a seleção. Os campos de argumentos
usam um array JSON, por exemplo `["--stdio"]`; não é necessário escapar um
comando shell inteiro. `Tab` alterna campos, `Ctrl+S` salva e `Esc` volta.

`Ctrl+T` testa o candidato do formulário, sem salvá-lo. O aplicativo solicita
permissão antes de iniciar o executável ou conectar ao MCP. O teste usa o mesmo
isolamento e ambiente filtrado do runtime; depois da aprovação, tem limite de
8 segundos e fecha seu cliente. `Esc` cancela mantendo o formulário. A aprovação
pertence a um escopo temporário e é revogada ao encerrar o teste.

O resultado MCP mostra uma página limitada de nomes das ferramentas e recursos;
LSP mostra as capacidades declaradas pelo servidor. Um servidor pode oferecer
mais entradas que a página mostrada. O teste não chama ferramentas MCP nem altera
arquivos. Inicializar um servidor pode executar seu processo configurado, por
isso continua sujeito à permissão e ao sandbox.

Salvar configurações não inicia automaticamente uma integração nova. As chamadas
MCP seguintes consultam o cadastro atualizado: `list_mcp_tools` enumera os
schemas sob aprovação e `call_mcp_tool` chama somente uma ferramenta descoberta.
Reinicie o aplicativo para aplicar alterações aos servidores LSP ativos.
O cadastro permite desativar uma integração sem apagar sua configuração.
Servidores LSP desativados não são iniciados.

Para chaves de provedores, o formulário também oferece [cofre nativo opcional](native-vault.md),
chave direta e variável de ambiente. O cofre não altera as permissões das ferramentas.

## Credenciais por ambiente

Para MCP HTTP/SSE, cada entrada de `headersEnv` aponta para uma variável contendo
o valor completo do cabeçalho. Para stdio, `envVars` mapeia o nome recebido pelo
processo filho para a variável de origem do ambiente:

```json
{
  "mcpServers": {
    "documentacao": {
      "type": "http",
      "url": "https://mcp.example/mcp",
      "headersEnv": {"Authorization": "MCP_AUTHORIZATION"}
    },
    "servidor-local": {
      "type": "stdio",
      "command": "meu-servidor-mcp",
      "args": ["--stdio"],
      "envVars": {"SERVICE_API_KEY": "MINHA_CHAVE_SERVICO"},
      "disabled": true
    }
  },
  "lsp": {
    "go": {"command": "gopls", "args": [], "disabled": false}
  }
}
```

Defina `MCP_AUTHORIZATION` com o valor completo esperado, como `Bearer …`, fora
do arquivo. As referências são resolvidas em cópias em memória; seus valores não
são gravados ao editar a integração. URLs com credenciais, query string ou
fragmento são rejeitadas. Os campos legados `headers` e `env` continuam válidos,
mas a interface preserva esses valores privados sem mostrá-los nos novos campos.

Editar uma entrada já presente no projeto salva no mesmo `.opcode/config.json`
(ou no formato anterior que já estava em uso);
entradas novas são salvas no arquivo global em uso. A escrita é atômica,
preserva campos desconhecidos e usa permissão `0600`.

## OAuth MCP explícito

O formulário MCP inclui ativação OAuth, client ID opcional, referência ao segredo
por variável de ambiente, escopos separados por espaços e porta de retorno
(`0` escolhe automaticamente). Salve o cadastro antes de autenticar. OAuth é
aceito em HTTP/SSE; não combine com um cabeçalho Authorization manual.

Na lista de integrações, selecione o MCP e use:

- `Ctrl+A`: iniciar login. Abra a URL mostrada no navegador; `↑↓` rola URLs longas.
- `Ctrl+U`: consultar o estado da autenticação.
- `Ctrl+D`: remover a autenticação local.
- `Esc`: cancelar a espera e voltar preservando a configuração.

Nenhum navegador, login ou conexão é iniciado pela simples abertura da lista.
O fluxo usa PKCE/estado e retorno local com prazo; tokens ficam no armazenamento
privado de autenticação, separados do JSON de configurações. A autenticação não
concede permissão para ferramentas MCP nem restaura aprovações de sessões antigas.
