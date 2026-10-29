# Editor de tema

Abra o seletor com `Ctrl+T`, escolha um tema e pressione `Ctrl+E`.
O editor usa a paleta desse tema como base. Não altera o tema original.

É possível ajustar destaque, texto, texto secundário e fundo, cada um com valores
separados para terminal claro e escuro. Outros tokens, como sintaxe, diffs e cores
de erro, continuam herdados da base. O texto e os títulos Markdown acompanham
os tokens básicos alterados.

- `Tab`/`Shift+Tab`, setas ou clique alternam o campo; resize preserva o foco.
- As cores aceitam somente `#RRGGBB`, sem nomes, expressões ou comandos.
- Uma combinação válida é pré-visualizada em memória. Valores incompletos ou
  inválidos conservam a última prévia válida e mostram o motivo.
- `Ctrl+S` ou **Salvar** grava e ativa o tema **custom** explicitamente.
- `Esc` ou **Cancelar** descarta a edição e restaura a prévia anterior no seletor.
  Outro `Esc` no seletor retorna ao tema que estava ativo ao abri-lo.

A seleção de campos tem marcador `›`, inclusive com `NO_COLOR`. Sem cores, os
valores continuam editáveis, mas o terminal não consegue mostrar a cor resultante.
A área de edição mostra apenas os campos que cabem, mantendo o campo atual visível.

## Persistência e limites

A paleta é salva em `~/.opcode/themes/custom.json`, com escrita atômica privada,
limite de 16 KiB e rejeição de arquivos/diretórios simbólicos. Diretórios novos
usam `0700` e o arquivo usa `0600` em Unix. O documento contém apenas o nome da
base e as quatro cores adaptáveis; não contém credenciais, shell ou JavaScript.
A escolha ativa usa a configuração existente `tui.theme: "custom"`.

Na inicialização, o arquivo é carregado antes de aplicar o tema configurado.
Uma paleta inválida é ignorada e não substitui a paleta válida já registrada.
Se a gravação da paleta funcionar, mas o salvamento da seleção ativa falhar, o
editor informa a falha e restaura o documento anterior (ou remove o documento
recém-criado). A seleção em memória é preservada. Se a restauração também falhar,
o erro inclui essa condição; corrija a configuração antes de tentar salvar novamente.

A base precisa ser um tema existente, diferente de `custom`. O editor retém a
base original ao reabrir um tema personalizado, evitando herança circular.
O arquivo pode ser copiado entre instalações que contenham essa base.

Antes de pré-visualizar e salvar, o aplicativo exige contraste calculado de pelo
menos 4,5:1 para destaque, texto e texto secundário em ambos os modos, contra o
fundo editado e o fundo secundário herdado. Isso é uma verificação dos pares
básicos, não uma certificação de todos os tokens herdados ou tecnologias assistivas.
Por esse motivo, um tema antigo com contraste insuficiente pode exigir ajustar
mais de um campo antes de aceitar a prévia.

## Validação

Testes usam HOME e configuração temporários, sem tocar o tema do usuário.
Cobrem prévia/cancelamento, gravação e reload, formato inválido, contraste,
tamanho excessivo, symlink e telas de 36×12, 60×20 e 80×24, incluindo `NO_COLOR`.

```sh
go test -race ./internal/tui/theme ./internal/tui/components/dialog
```
