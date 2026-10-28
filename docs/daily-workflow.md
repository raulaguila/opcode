# Configurar e trabalhar com o Opcode

## Primeiro acesso

Ao iniciar sem configuração global compatível, o Opcode cria um template privado
em `~/.opcode/config.json`. Não sobrescreve configurações existentes, não copia
chaves antigas e não baixa modelos. A configuração opcional do projeto fica em
`.opcode/config.json`; os formatos anteriores continuam funcionando no próprio
caminho. `doctor` e os comandos de leitura de configuração não criam o template.

Sem modelo, a interface abre a etapa 1: editar ou adicionar uma conexão. `N`
oferece presets; `Ctrl+T` consulta metadados e `Ctrl+S` salva. A etapa 2 lista os
modelos do provedor para seleção. A etapa 3 mostra o modelo, o caminho do JSON e
opções para revisar a configuração, testar ou começar a conversar. `/setup`
reabre esse fluxo. Idioma e preferências ficam em `/settings` → `P`.

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
