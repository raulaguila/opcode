# Configurar e trabalhar com o Opcode

## Primeiro acesso

Ao iniciar, o Opcode cria `~/.opcode/config.json` quando ausente: preserva o JSON
global antigo no novo arquivo privado, mantendo o original, ou usa um template
se nenhuma configuração global existir. Não sobrescreve o destino nem baixa modelos. A configuração opcional do projeto fica em
`.opcode/config.json`; os formatos anteriores continuam funcionando no próprio
caminho. `doctor` e os comandos de leitura de configuração não criam o template.

A cada abertura normal, `~/opcode/config.example.json` é criado se estiver
ausente, como referência independente da configuração ativa. Ele contém apenas
exemplos, sem copiar seus ajustes ou suas chaves; alterações que você fizer nele
são preservadas. Para aplicar uma opção, edite `~/.opcode/config.json` ou use
`/settings` — o arquivo de exemplo não é carregado pelo aplicativo.

`opcode --reset` guarda o JSON global anterior em `~/.opcode/config-backups/`,
recria a configuração ativa com os padrões e inicia a aplicação. Funciona mesmo
com JSON inválido. O caminho do backup é informado no terminal; para desfazer,
feche a aplicação e restaure essa cópia em `~/.opcode/config.json`. O backup pode
conter credenciais e tem permissão 0600. Encerre outras instâncias antes do reset.
Configurações de projeto e variáveis de ambiente continuam se aplicando;
histórico, bancos e arquivos de trabalho são preservados. `--help` e `--version`
não fazem o reset, mesmo quando combinados com a flag.

Sem modelo, a interface abre a etapa 1: editar ou adicionar uma conexão. `N`
oferece presets; `Ctrl+T` consulta metadados e `Ctrl+S` salva. A etapa 2 lista os
modelos do provedor para seleção. A etapa 3 destaca “Começar a conversar”.
“Detalhes e testes opcionais” reúne o caminho do JSON e as verificações. `/setup`
reabre esse fluxo. Idioma e preferências ficam em `/settings` → `P`.

No uso diário, o modo e o modelo ficam na barra inferior e abrem seus seletores
ao clicar. O centro mostra comandos ou a atividade atual. `/` oferece as ações
mais usadas; `/commands` e `Ctrl+K` abrem a busca por todas as ações. Detalhes de
modelos usam `Ctrl+D` no seletor, e campos técnicos da conexão usam `Ctrl+A` no
formulário. Processo e lateral começam recolhidos, respeitando preferências
explicitamente salvas. `/plan` mostra o plano quando você precisar consultá-lo;
perguntas, aprovações e erros continuam aparecendo durante o trabalho.

As mensagens públicas mantêm a ordem da conversa entre grupos recolhíveis de
ferramentas. Um diagnóstico continua acessível depois de você responder à
pergunta do modelo; o estado de citação acompanha o texto correspondente.
Perguntas usam painel destacado, aguardam seleção ou texto explícito e não
concedem autorização para ferramentas.

`/quit` salva o rascunho pelo fluxo normal de encerramento. Na próxima abertura,
`opcode --resume` reabre a última conversa disponível do projeto atual;
`opcode --resume ID` escolhe uma conversa específica. Isso não continua ações
automaticamente nem restaura aprovações. Dentro da conversa, `/resume` pede a
continuação da tarefa salva. Sem conversa anterior disponível, a flag informa
isso e abre uma nova. No cliente `attach`, `/quit` apenas desconecta do servidor.

O input começa com três linhas úteis e cresce até oito; `Shift+Enter` insere
uma nova linha quando o terminal transmite o atalho. `Ctrl+J` e `Alt+Enter`
também inserem uma linha. Colagem com múltiplas linhas preserva o texto sem enviar.
Seletores mantêm o rascunho e recebem o foco enquanto abertos. No catálogo,
`✓` marca o modelo ativo, `›` o destaque, e `PgUp`/`PgDn` navegam por páginas;
`Alt+←`/`Alt+→` alternam provedores.

LSP ausente gera um aviso curto com orientação para `/integrations`; não impede
conversar com o modelo. A saída bruta de erro do servidor LSP é descartada para
preservar o terminal e evitar expor conteúdo sensível. Durações ao lado das
respostas usam segundos/minutos e medem aquela resposta, não a tarefa inteira.

Os dois testes opcionais exigem confirmação. O probe faz uma inferência com uma
ferramenta inerte; nenhuma ferramenta é executada. A tarefa de exemplo muda a
sessão para Perguntar e pede uma explicação a partir de arquivos reais. Ambos
podem consumir tokens; nenhum deles certifica a qualidade geral do modelo.

## Resultado esperado da tarefa

`/contract` permite escolher Explicar, Planejar, Alterar ou Verificar, informar
arquivos relativos, critérios de aceite e verificações já configuradas. Use
vírgulas entre arquivos/checks e ponto e vírgula entre critérios. `Tab` muda de
campo, `Ctrl+S` salva e `Esc` preserva o rascunho da conversa.

O contrato é salvo na tarefa e continua ativo até ser removido pelo mesmo painel
ou por `/task new`. `/task` mostra os alvos e critérios. O contrato não concede
permissões nem amplia `/mode`: Alterar/Verificar exigem Execute; salvar plano
exige Plan ou Execute. Explicar restringe as ferramentas à leitura; Verificar
recusa edição, shell livre e integrações opacas, mantendo `run_checks` com suas
aprovações normais. Os checks do contrato exigem recibos da revisão atual.
Arquivos e critérios são orientações, não uma prova automática de correção
semântica nem uma lista adicional de caminhos autorizados.

Na CLI:

```sh
opcode -p 'Corrija a soma em counter.py' --task-kind change \
  --target counter.py --accept 'Preservar a assinatura da função'
opcode -p 'Execute a verificação configurada' --task-kind verify --check test
```

`--target`, `--accept` e `--check` são repetíveis. `--check` recebe um nome de
`checks` no JSON, nunca um comando arbitrário. Aprovações continuam necessárias;
`--task-kind` não equivale a `--auto-approve`.

Uma alteração sem checks configurados é trabalho parcial: os arquivos podem
estar corrigidos, mas não há validação aprovada. A interface informa isso em vez
de declarar conclusão ou exigir que o modelo invente uma verificação.

## Configuração e diagnóstico

```sh
opcode config path
opcode config validate
opcode config validate --file /caminho/config.json
opcode config show --redacted
opcode config migrate --file /caminho/config.json
opcode doctor
opcode doctor --ollama-status
```

`validate` verifica JSON e campos conhecidos sem rede nem credenciais. Erros de
tipo apontam o campo; erros de sintaxe apontam linha/coluna, sem imprimir valores.
Campos desconhecidos são preservados. `show` exporta somente valores permitidos
e suas origens, omitindo chaves, URLs, comandos e campos arbitrários.

`migrate` é explícito, adiciona `schemaVersion: 1` no mesmo arquivo e cria antes
um backup exato, privado, em `.opcode/config-backups` ao lado dele (ou
`config-backups` dentro da pasta `.opcode` existente). O backup pode conter
credenciais antigas: permanece na área privada protegida das ferramentas. Para
reverter, restaure esse arquivo no caminho original. Repetir a migração não cria
outra cópia; configurações sem versão continuam aceitas. Versões futuras são
recusadas em vez de regravadas. Não há migração automática destrutiva.

`/local-status` consulta somente `GET /api/ps` do Ollama: contexto alocado,
CPU/GPU e memória usada pelo modelo. Não informa VRAM livre, não carrega modelos
e não muda sua configuração. Uma alocação diferente pode ser substituída pela
próxima geração; `/context` mostra o orçamento efetivo e `/recalibrate` remove
um limite temporário aprendido. Mais contexto exige mais memória.

## Git e projetos isolados

`/worktrees` reúne projeto atual, branch, arquivos alterados, diferenças Git,
recibos da tarefa e criação de worktree a partir do HEAD commitado. A criação
exige Execute, agente ocioso e aprovação do comando Git exato; Bubblewrap continua
habilitado. Alterações locais não são copiadas para a nova worktree.

Escolher outra worktree pede confirmação e abre uma instância do Opcode no mesmo
terminal. Ao sair dela, você retorna à conversa anterior. A instância tem seus
próprios processos, sessões e autorizações; não recebe as aprovações atuais.
Encerre processos da conversa e aguarde/cancele o agente antes de abrir outra.
Worktrees desaparecidas, não registradas ou com caminhos simbólicos são recusadas.

As diferenças Git incluem alterações manuais; undo/redo continua limitado às
edições capturadas pelas ferramentas. Não há commit, merge, reset ou push
automático nesse painel. Worktrees cujo diretório Git comum fique fora dos
caminhos permitidos pelo sandbox podem ter operações indisponíveis; a proteção
não é desativada para contornar isso.


Veja também a [evolução comparativa](comparative-evolution.md) para perguntas agrupadas, busca aproximada de modelos, citações verificadas e diagnóstico de linguagens.
