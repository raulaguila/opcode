# Evolução comparativa — 28/10/2026

## Referências e continuidade

`view`, `read_lines` e `read_symbol` produzem recibos de caminho, SHA-256 e linhas
entregues. As leituras iniciais passam pelo mesmo controle de acesso e preservam
esses metadados no histórico. Conteúdo truncado limita o intervalo aceito.

Nas explicações/localizações reconhecidas pelo runtime, uma referência precisa
corresponder a um recibo bem-sucedido do turno atual, às linhas efetivamente
entregues e ao hash atual. Recibos de erro, busca, resumo, turno anterior ou
arquivo alterado não bastam. Páginas adjacentes do mesmo hash podem formar um
intervalo; lacunas não são preenchidas. A recusa explícita de citações pelo
usuário continua respeitada.

Há um reparo de citação, sem repetir alterações ou comandos. Ele preserva o pedido original
para evitar que o modelo responda ao aviso de validação. No Ollama, o reparo usa a resposta
interna `answer_with_sources`, com explicação e fonte separadas; seu schema
oferece até oito intervalos de fontes atuais. As referências vêm do modelo e
passam novamente pelo mesmo validador: a aplicação não fabrica uma citação para
aprovar uma resposta. Isso valida procedência/linhas/hash, não interpretação.

A compactação guarda localizadores históricos limitados com mensagem, caminho,
hash e linhas. Eles ajudam a reencontrar a fonte via `read_conversation` ou nova
leitura, mas não viram evidência atual nem concedem autorização. Fontes externas,
sensíveis ou symlinks não são certificadas automaticamente. A revalidação tem
prazo de três segundos e limita arquivos a 16 MiB.

Uma resposta final privada malformada pode ter uma regeneração adicional, apenas
sem texto parcial ou chamadas publicadas. O parser continua estrito e operações
concluídas permanecem no histórico, sem reexecução. Reincidência encerra com erro.

## Perguntas e modelos

Perguntas simples continuam aceitando opções e texto livre. Lotes têm até cinco
perguntas, com tipos `single_choice`, `multi_choice`, `yes_no` e `free_text`.
O schema avançado `ask_user_questions` é carregado com `request_tools`; a pergunta
simples fica no conjunto inicial, evitando custo permanente no contexto.
Cada pergunta tem até cinco opções. IDs, opções repetidas/desconhecidas, respostas
ausentes e formatos inválidos são recusados. Respostas nunca aprovam ferramentas.

No lote, ↑/↓ navega, Espaço marca opções, e digitar oferece uma alternativa livre.
Enter/Tab avança; Shift+Tab retorna preservando respostas. A última etapa mostra
a revisão; Enter envia o conjunto. F2 abre detalhes com rolagem. Esc cancela a
atividade. O rascunho normal da conversa é preservado.

O seletor aceita busca aproximada (`llm8b` encontra `llama3.1:8b`), priorizando
correspondências diretas. Atualizar mantém a seleção e o catálogo anterior até
chegar um resultado utilizável; falhas não esvaziam a lista. Favoritos/recentes
continuam disponíveis. Os detalhes ficam em Ctrl+D.

A listagem da TUI não consulta `/api/show` para todos os modelos. O modelo em
destaque é consultado após 200 ms; mudar seleção/fechar cancela a consulta e
descarta respostas atrasadas. Se a seleção ocorrer antes da descoberta, a mesma
consulta individual resolve o contexto antes de efetivar a escolha. Falhas
preservam os valores anteriores identificados por sua origem. Overrides manuais
continuam valendo. Não há geração, carregamento de pesos ou download.

## Linguagens e configuração assistida

Os perfis locais atuais estão no [inventário gerado do runtime](language-support.md),
com fixtures de declarações/índice e teste de drift. Vue/Svelte usam scripts JS/TS
e linhas originais. A contagem de perfis não comprova suporte semântico.
Veja a [matriz de linguagens e limites](retrieval-runtime.md). O índice invalida
os chunks da política anterior mesmo quando o arquivo não mudou.

`opcode doctor --languages --json` mostra linguagens detectadas, parsers,
executáveis LSP, configuração/desativação e checks. A ferramenta
`get_language_support` oferece páginas e filtros. Sugestões indicam comandos,
argumentos e marcadores; nada é instalado/iniciado automaticamente. Arquivos
sem parser continuam pesquisáveis por texto, e semântica exige LSP apropriado.

## Avaliar

`evaluate compare` confere hash da suíte e condições antes de comparar relatórios.
`evaluate retrieval` mede ranking real com cache frio/quente, sem inferência.
Perfis/versões de prompt são observados; o rótulo de experimento não muda prompts
nem demonstra causalidade. Drivers externos têm limites e capacidades explícitos.
Uma aplicação ausente ou um critério não observável não recebe aprovação.
Veja [comandos, condições e adapters](evaluation.md).

A [comparação de 29/10 após as nove melhorias](comparison-2026-10-29.md) registra
novos controles, métodos e limites de observação.
