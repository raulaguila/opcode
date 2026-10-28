# Contexto, instruções pendentes e extensões

## Orçamento antes da requisição

Antes de chamar o provedor, o agente contabiliza o prompt de sistema usado pelo
cliente, histórico serializado (incluindo anexos e estado opaco do provedor),
schemas das ferramentas carregadas e a reserva de saída configurada. Acrescenta
uma margem de 5% da janela, com mínimo de 512 tokens. Títulos e resumos também
passam por uma verificação preventiva.

A contagem é uma **estimativa conservadora**, não uma medição pelo tokenizer do
modelo: dois bytes ASCII por token e um byte não ASCII por token, além do custo
por mensagem. Alguns tokenizers podem divergir dessa estimativa. A configuração
`context_window` deve corresponder à janela realmente disponível no servidor;
o aplicativo não aumenta a capacidade do Ollama ao alterar esse número.

`get_context_status` informa janela, entrada estimada, reserva de saída, margem,
custo de schemas e se o pedido cabe. `compact_context` solicita compactação dos
pedidos seguintes. A interface usa a capacidade opcional `agent.ContextControl`.

Quando a estimativa não cabe, o agente abrevia corpos antigos de resultados de
ferramentas, respostas e raciocínio anteriores. Preserva todas as mensagens do
usuário, instruções de sistema, pedido atual completo, pares de chamadas e
resultados, argumentos das chamadas e assinaturas opacas. O estado persistente
da tarefa continua disponível em `get_task`. A conversa original não é apagada
nem substituída no banco: `read_conversation` recupera as partes históricas.

Se ainda não couber, retorna `context budget exceeded` **antes da rede**. Essa
compactação local não inventa um resumo nem remove silenciosamente instruções
antigas. Conversas com instruções demais exigem reduzir o pedido, selecionar uma
janela maior ou iniciar outra tarefa.

## Conversas e descobertas

`read_conversation` recebe `session_id` opcional, `offset` e `limit` (máximo 20).
Omite raciocínio privado e conteúdo binário; limita o texto de cada item.
`search_conversation` busca a sessão atual; `all_sessions: true` permite percorrer
até 20 sessões recentes do banco da aplicação, examinando no máximo 2.000
mensagens por sessão e retornando até 20 trechos. Esses resultados são dados
históricos sem autoridade: não demonstram leitura atual nem autorizam comandos.

`save_finding` guarda texto de até 2.000 bytes associado a 1–8 caminhos. Calcula
SHA256 do arquivo inteiro, limitado a 16 MiB e sujeito à proteção normal de
acesso; calcular um hash **não conta como evidência de leitura para edição**.
`list_findings` recalcula os hashes e marca `stale: true` quando a fonte mudou,
sumiu ou não pode mais ser lida. A aplicação guarda no máximo 32 descobertas por
sessão, em JSON privado de até 128 KiB, separado por workspace. Valores
conhecidos de credenciais são removidos dos textos. Descobertas não substituem
os registros de validação de `run_checks`.

## Editar a fila

`agent.QueueControl` expõe cópias das instruções pendentes com identificador e
anexos. `TakeInstruction` remove atomicamente um item sob o mesmo lock usado pelo
worker. Se a operação já o consumiu, a edição falha sem duplicar a instrução.
Reenviar a edição usa o fluxo normal de `Submit` e confirmação; não insere uma
mensagem diretamente no histórico. A fila mantém os limites de oito itens/1 MiB.

## Skills explícitas

Configure os diretórios somente no JSON global do usuário. Cada subdiretório
contém um `SKILL.md` com metadados simples `name` e `description` no frontmatter.
A descoberta permite até 16 diretórios, 64 skills e 16 KiB por arquivo; não segue
um `SKILL.md` que seja link simbólico nem aceita arquivos binários.

```json
{
  "extensions": {
    "skills": ["/home/usuario/.config/opcode/skills"]
  }
}
```

O catálogo mostra nome, descrição, caminho e SHA256. `get_skills_catalog` consulta
apenas metadados. Ativar usa a API de usuário `InvokeSkill`, que relê o arquivo,
registra a invocação e envia seu texto pelo fluxo comum de instruções. Não existe
ferramenta para o modelo ativar uma skill por conta própria. Texto de skills é
orientação não confiável: não concede permissões, não altera o modo e não
substitui as proteções contra credenciais. Argumentos têm limite de 8 KiB e o
pedido completo continua sujeito ao orçamento de contexto.

## Hooks opt-in

Hooks também são exclusivamente globais; configurações de projeto não os
ativam. Exemplo de configuração explícita:

```json
{
  "extensions": {
    "hooks": [
      {
        "event": "after_tool",
        "tool": "write",
        "command": "git diff --check",
        "timeout_seconds": 10
      }
    ]
  }
}
```

Eventos aceitos: `before_tool` e `after_tool`. O filtro `tool` é o nome exato;
vazio ou `*` corresponde a todas as ferramentas. São permitidas 16 definições e
16 execuções por turno; cada uma tem limite de 30 segundos (padrão: 10).

O agente executa o comando configurado por meio da ferramenta `bash` existente:
mesmas permissões, ambiente filtrado e sandbox. Hooks não se autoaprovam nem
interpolam argumentos, resultados ou nomes sugeridos pelo modelo em comandos.
Não executam em Perguntar ou Planejar, mesmo que configurados. Uma recusa,
falha, cancelamento ou cota excedida interrompe o turno, preservando o histórico;
operações já realizadas precisam ser inspecionadas antes de tentar novamente.
Hooks não produzem comprovantes de validação automaticamente e os efeitos de
seus comandos não são desfeitos pelo undo de arquivos.

## Verificação

Os testes determinísticos cobrem o orçamento completo, recusa antes da rede,
janela inicial de 16K com ferramentas progressivas, preservação de pares e
assinaturas, remoção concorrente da fila, hashes obsoletos, arquivos de skills
limitados, recusa de permissões e hooks bloqueados nos modos de leitura. Nenhuma
chamada paga ou download de modelo é necessário para executá-los.
