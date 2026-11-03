# Terminal e app

O opcode tem duas interfaces sobre o mesmo agente, as mesmas conversas, as
mesmas permissões e a mesma configuração. Uma conversa começada numa continua
na outra.

```sh
opcode       # no terminal; funciona também via SSH
opcode app   # numa janela própria, fora do terminal
```

As duas seguem a mesma proposta visual: transcrição com a sua mensagem numa
barra violeta e a resposta numa barra azul, passos das ferramentas agrupados
por resultado, campo de mensagem com a cor do modo (Perguntar azul, Planejar
âmbar, Executar laranja) e um rodapé com a política de aprovação, a atividade,
o uso do contexto e o modelo.

Os protótipos das duas estão em [docs/prototipos](prototipos/README.md).

A interface anterior do terminal continua disponível por um tempo com
`opcode --ui classico`; o guia dela é [Trabalhar pela interface](workspace-ui.md).

## Qual escolher

| | Terminal (`opcode`) | App (`opcode app`) |
|---|---|---|
| Onde roda | qualquer terminal, inclusive via SSH | janela própria na máquina local |
| Memória | ~50 MB | ~50 MB do processo + a web view do sistema |
| Abrir | instantâneo | cerca de meio segundo |
| Tema | escuro | escuro, claro ou o do sistema |
| Mouse | rodapé e campo de mensagem | em todas as telas |

O modelo e as ferramentas rodam igual nas duas; o que muda é o custo da janela.

## Primeiro uso

Sem modelo configurado, as duas abrem em **Nova conexão**: escolha um ponto de
partida (Ollama, LM Studio, OpenAI, Anthropic, Gemini, OpenRouter ou compatível
com OpenAI), revise, teste o catálogo com `Ctrl+T` e salve com `Ctrl+S`. Depois
escolha o modelo com `Ctrl+O`.

## Telas

- **Conversa**: passos agrupados (`✓ view store.go · main.go`,
  `✗ edit store.go ×3 · 3 falhas`, `edit main.go · +12 −0`), o card
  **Parado** com Retomar, Detalhes e Trocar modelo quando uma execução falha, e
  a faixa do plano acima do campo de mensagem.
- **Aprovação**: arquivo e `+N −M` no topo, prévia do diff com números de linha
  e três escolhas: permitir uma vez, permitir este escopo na sessão ou recusar.
- **Pergunta do agente**: opções numeradas, com uma nota curta ao lado quando o
  agente a envia; a última opção é a sua própria resposta, escrita na linha.
- **Plano**: barra de progresso, etapas com `✓ ● ○` e o resultado esperado.
- **Comandos** (`Ctrl+K`): busca, grupos e o atalho de cada comando.
- **Escolher modelo** (`Ctrl+O`): abas por conexão, grupos Locais e Nuvem;
  `Ctrl+D` abre os detalhes (contexto, saída, onde o modelo está carregado,
  velocidade da última resposta).
- **Conversas** (`Ctrl+S`): por dia, com estado (`● ✓ ◐ ■ ○`) e prévia;
  renomear, exportar em Markdown e arquivar.
- **Alterações** (`/changes`): as interações que editaram arquivos, a mais
  recente primeiro, com o diff que `Enter` aplicaria. `Enter` desfaz uma
  interação aplicada ou refaz uma desfeita; `/undo` e `/redo` agem direto na
  última. Cada arquivo pede aprovação no card de sempre; se o arquivo mudou
  depois da interação, nada é escrito. Comandos (`bash`), serviços externos e
  edições suas fora do opcode não são desfeitos: o git continua sendo a rede de
  segurança.
- **Configurações**: menu lateral com Conexões, Interface, Trabalho, Aprovação,
  Atalhos, Integrações e Avançado. Conexões aparecem como cards com estado
  (em uso, chave ausente, desativada), endereço, autenticação e o resultado do
  último teste.

## Teclas

| Tecla | Terminal | App |
|---|---|---|
| `Enter` | envia | envia |
| `Alt+Enter`, `Shift+Enter` | nova linha (`Ctrl+J` também) | nova linha |
| `Shift+Tab` | alterna o modo | alterna o modo |
| `↑` `↓` no campo | mensagens que você enviou nesta conversa; depois da mais recente, volta o rascunho | idem |
| `Ctrl+A` no seletor de modelos | modelo de tarefas, títulos ou resumos | idem |
| `Ctrl+K`, `/` com o campo vazio | comandos | comandos |
| `Ctrl+O` | escolher modelo | escolher modelo |
| `Ctrl+S` | conversas | conversas |
| `Ctrl+N` | nova conversa | nova conversa |
| `Esc` | cancela a execução | cancela a execução |
| `Ctrl+C` | pede confirmação para sair; `Enter` ou outro `Ctrl+C` sai | — |
| `1` `2` `3` | responde a aprovação | responde a aprovação |
| `T` | mostra o diff inteiro na aprovação | mostra o diff inteiro na aprovação |
| `@` | lista os arquivos do projeto para citar | lista os arquivos do projeto para citar |
| `C` `R` nos detalhes do modelo em uso | contexto · raciocínio | seletores |
| `PgUp` `PgDn` / `Alt+↑↓` | rolam a conversa | rolam a conversa |
| `Ctrl+R` `Ctrl+E` no card Parado | retomar · detalhes | botões do card |

Nas janelas, `↑↓` andam pelos itens e `Enter` escolhe. Em Configurações, `↑↓`
escolhem a seção, `→` entra nela e `←` volta ao menu; dentro, `→` ou `Espaço`
mudam o valor. Em Conversas, `→` vai da lista para as ações e `←` volta. No
app, botões lado a lado aceitam `←→` e `Enter`.

Na pergunta do agente, os números marcam, `Espaço` alterna numa pergunta de
várias respostas e `Enter` responde. `Esc` guarda a pergunta para depois: o
agente continua esperando e o rodapé mostra *Pergunta pendente*, que `Enter`
com o campo vazio reabre.

Nas duas:

- **@ arquivos**: digite `@` e parte do nome; a lista respeita o `.gitignore`.
- **Contexto e Raciocínio** nos detalhes do modelo em uso. Contexto é salvo com
  o modelo (ou *automático*); Raciocínio aparece quando o provedor aceita o
  ajuste. Com Ollama, os detalhes mostram se o modelo está carregado e onde.
- **Largura de leitura** limita a largura das mensagens e **Mostrar processo**
  lista cada ferramenta numa linha.

## Só no app

- **Tema** claro, escuro (o da proposta) ou o do sistema, em Configurações ›
  Interface. A escolha fica só no navegador desta janela.

## Janela nativa ou do navegador

O binário comum não depende de bibliotecas gráficas, para rodar também em
servidores. Nele, `opcode app` abre uma janela de app do Chrome, Chromium, Edge
ou Brave, com um perfil próprio, ou o navegador padrão.

Compilado com a tag `desktop`, o app abre numa janela nativa (Wails) que usa a
web view do sistema:

```sh
make build-desktop            # Linux; precisa de libgtk-3-dev e libwebkit2gtk-4.1-dev
make app                      # compila e abre
sh ./install --desktop        # instala com a janela nativa
```

No macOS e no Windows use `DESKTOP_TAGS=desktop,production`. As releases trazem
os dois tipos de arquivo: `opcode-<versão>-…` (comum) e `opcode-desktop-<versão>-…`
(com a janela nativa).

## Segurança do app

A janela e o processo do opcode conversam por HTTP local, e só eles:

- o processo escuta em `127.0.0.1`, numa porta escolhida ao abrir;
- cada abertura gera uma chave nova; a janela a recebe no fragmento do endereço
  (que o navegador nunca envia) e a manda num cabeçalho em cada pedido;
- pedidos com outro `Host` (proteção contra DNS rebinding) ou de outra origem
  são recusados;
- a página não carrega nada da rede: as fontes vêm embutidas e a política de
  conteúdo só admite os próprios arquivos;
- respostas do modelo são renderizadas em Markdown sem HTML cru;
- chaves de API nunca saem do processo: as telas só sabem como cada chave está
  guardada; uma chave deixada em branco no formulário mantém a já guardada;
- mudanças de configuração esperam a tarefa em andamento terminar.

## Onde está no código

- `internal/ui`: a interface do terminal (Bubble Tea), sobre os mesmos serviços
  do aplicativo.
- `internal/desktop`: o servidor local do app e a página (`web/`), embutida no
  binário.
- `cmd/app.go`: o comando `opcode app`; `cmd/app_native.go` (tag `desktop`)
  abre a janela Wails e `cmd/app_browser.go` o substituto sem a tag.
- `internal/tui`: a interface clássica.
