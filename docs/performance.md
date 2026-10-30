# Medir desempenho e alocações

As fixtures de componente não consultam APIs reais. Execute com a mesma toolchain,
mesmas entradas e sem testes, builds ou inferência concorrentes durante a medição:

```sh
make bench-performance > /tmp/opcode-after.log
make bench-compare BEFORE=/tmp/opcode-before.log AFTER=/tmp/opcode-after.log
```

O alvo de medição executa os pacotes sequencialmente (`-p 1`). O segundo
comando emite JSON e retorna 1 se a mediana de B/op ou allocs/op de
qualquer caso do baseline crescer mais de 10%. Retorna 2 para logs vazios,
métricas inválidas, caso removido do candidato ou menos de três amostras por
caso nos dois lados. Tempo é informado e revisado, sem um limite fixo entre
máquinas. Casos novos sem baseline não entram nessa comparação: meça-os
separadamente e preserve suas condições antes de atribuir um ganho.

Use exatamente as mesmas fixtures nos dois checkouts; um benchmark adicionado
nesta revisão pode precisar ser copiado para a revisão anterior, desde que use
as mesmas APIs/entradas. Não substitua o trabalho da versão anterior por uma
operação menor. O benchmark de troca de conversa, por exemplo, executa os
comandos retornados e mede a primeira apresentação concluída, incluindo a
versão assíncrona. Preserve os logs brutos e a identificação dos commits.

Fixtures disponíveis:

- Hash do workspace com 200 e 2.000 arquivos pequenos.
- Busca aquecida com watcher ativo, mais edição de um arquivo, alteração de
  política e reconciliação periódica. Abertura do banco e preparação ficam fora
  da região medida nesses casos; os resultados não representam a ferramenta
  completa com verificação de hits.
- Revalidação de citação de um arquivo de 1 MiB, com hash e contagem de linhas.
- Preparação real do contexto com histórico de 160 kB e imagem PNG sem
  compressão de aproximadamente 1 MiB, sem envio ao provedor.
- Projeção repetida de mensagens com redação e confirmação do último registro
  gravado; escrita do histórico autoritativo fica fora dos dois lados.
- Eventos do chat, resposta crescente, histórico maior que os dois caches,
  troca de conversa e Markdown com múltiplos blocos e larguras.

`B/op` é o total de bytes alocados por operação; não é a memória residente do
processo. Caches estáveis, atualizações reais, construção inicial e inferência
são cargas diferentes. As fixtures de crescimento usam ciclos limitados com
entradas iguais nos dois lados para não comparar tamanhos diferentes após a
calibração do runner. O caso de overflow é propositalmente grande; uma melhora
nele não é uma promessa de ganho igual em conversas pequenas.

As regressões funcionais continuam sendo o gate de integridade. Incluem
expiração entre lotes, falha de watcher, rotação de credenciais, quotas,
rollback, cancelamento, substituição do armazenamento privado, limites de
citação, paginação visual, seleção, busca, posições dos links e paridade do
Markdown. Reduzir alocações não autoriza remover essas verificações.
