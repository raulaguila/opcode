# Dependências e migração da interface

Revisão de 30/10/2026, com Go 1.27.3. `go.mod`/`go.sum` fixam as versões.
A atualização cobre as dependências diretas e transitivas usadas pela aplicação
e seus testes. Não exige alterar a configuração pessoal.

| Componente | Antes | Versão adotada |
| --- | --- | --- |
| Bubble Tea | 1.3.5 | 2.1.1 |
| Bubbles | 0.21.0 | 2.2.1 |
| Glamour | 0.9.1 | 2.0.1 |
| Lip Gloss | 1.1.0 | 2.0.7-0.20261009092946-19f011f1cc57 |
| x/ansi | 0.8.0 | 0.11.9 |
| HTML para Markdown | 1.6.0 | 2.5.2 |

Os imports visuais usam `charm.land/*/v2`. A raiz retorna `tea.View`, declarando
tela alternativa, mouse e foco. Componentes internos compõem strings pela
interface `util.Model`. Larguras do Lip Gloss v2 incluem bordas e padding; não
somar apenas o padding ao calcular a largura externa de um diálogo.

O teclado usa eventos nativos, incluindo colagem literal e modificadores.
Não há decodificador privado paralelo de CSI. Colagem em modais não chega ao
composer atrás deles nem responde a permissões. Shift+Enter ainda depende do
terminal; o rodapé informa o atalho disponível. A saída OSC52 é serializada
com a saída do terminal. `theme.AdaptiveColor` preserva o JSON com pares
`Light`/`Dark`. Alterações de fundo/perfil atualizam a apresentação sem gravar
configurações. Os caches distinguem tema, fundo, perfil e largura.
`NO_COLOR` remove as cores, preservando atributos como negrito e seleção em
vídeo reverso. Saída sem terminal (`NoTTY`) remove as sequências de estilo.
O tratamento do fundo preserva também o reset SGR compacto usado pelo Glamour
v2, evitando que o negrito de títulos continue nos parágrafos seguintes.

## Lip Gloss: substituir o commit por uma versão publicada

**Pendência de manutenção:** a versão publicada 2.0.6 escreve um byte por vez
em `WrapWriter.Write`, aumentando objetos alocados na renderização. O código
oficial agrupa essas escritas no commit
[`19f011f1cc573e5d2bf9d7a9c4f14d429647759e`](https://github.com/charmbracelet/lipgloss/blob/19f011f1cc573e5d2bf9d7a9c4f14d429647759e/wrap.go).
Fixamos esse commit pela pseudo-versão da tabela. Não há fork, `replace`,
cópia local nem alteração do cache de módulos necessária ao build.

Quando uma versão publicada contiver essa correção:

1. Conferir que a versão inclui o commit e as escritas agrupadas. Não trocar
   apenas porque o número da versão é maior.
2. Atualizar `charm.land/lipgloss/v2`, rodar `go mod tidy` e `go mod verify`,
   e retirar o comentário temporário de `go.mod`.
3. Rodar `make quality` e benchmarks pareados com as mesmas fixtures, perfil
   de cor, tema, largura, toolchain e entradas. Preservar o limite de 10% para
   bytes e quantidade de alocações.
4. Repetir PTY: Markdown/pergunta, seleção/cópia, revisões de resposta,
   redimensionamento, catálogo, comandos com aprovação e Ollama.
5. Registrar a versão publicada e os novos resultados neste guia/evidências.

A comparação inicial com a **2.0.6 publicada**, contra o checkpoint `5eb3ff5`,
encontrou +173,5% de objetos no Markdown, +185,4% no chat com o último item
invalidado e +247,1% durante streaming. Os bytes caíram 25,8%, 19,3% e 13,6%.
Esses números são anteriores ao commit oficial e não descrevem o resultado
final da dependência fixada. Tempo de renderização não mede velocidade do modelo.
Na matriz final, após as correções, as alocações caíram 47,1% no Markdown,
37,1% no chat atualizado e 35,6% durante streaming. Ganhos, regressões de tempo
e condições completas estão em [Desempenho](performance.md).

## Outras atualizações e compatibilidade

SQLite, Goose, Tree-sitter, tokenizer, Chroma, Cobra/Viper, fsnotify,
autenticação AWS/Azure/Google e `golang.org/x/*` também foram atualizados às
versões compatíveis resolvidas no módulo.

SQLite 0.36.0 separa FTS5 do núcleo. Os bancos de índice de código e memória
registram a extensão em **cada conexão do pool**, inclusive após substituição
de conexão. Sessões não passam a precisar de FTS. A migração não apaga sessões,
não recria índices para contornar erros e não altera schemas.

Há duas escolhas deliberadas de linha principal:

- **MCP 1.5.0:** posterior ao MCP 2.0.0, inclui proteções de transação OAuth e
  validação do emissor de callback ausentes naquela versão 2. Testes rejeitam
  emissor obrigatório ausente, parecido ou duplicado antes de consultar tokens.
  Uma futura migração deve preservar essas proteções. Consulte as releases
  [1.5.0](https://github.com/mark3labs/mcp-go/releases/tag/v1.5.0) e
  [2.0.0](https://github.com/mark3labs/mcp-go/releases/tag/v2.0.0).
- **Goldmark 1.8.7:** Glamour 2 usa a AST Goldmark 1. A aplicação entrega a mesma
  AST ao renderer para preservar referências, listas e código; Goldmark 2 não
  substitui essa interface. Rever quando Glamour migrar.

`go list -m -u all` também mostra dependências de testes e drivers opcionais de
outros módulos, mesmo sem pacote usado pelo Opcode. Não adicionar pins diretos
de drivers sem uso apenas para zerar essa listagem.

Na revisão, `govulncheck` não encontrou vulnerabilidades alcançáveis nem em
pacotes importados. Registrou GO-2026-5932 para `x/crypto/openpgp`, pacote que o
Opcode não importa, sem versão corrigida indicada para esse pacote descontinuado.

Para rollback, usar o checkpoint `5eb3ff5` como revisão completa de código,
`go.mod` e `go.sum`. Downgrade isolado do conjunto visual não é compatível com
a API v2. Os arquivos pessoais de configuração permanecem compatíveis.
