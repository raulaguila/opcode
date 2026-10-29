# Raciocínio do modelo e clipboard

## Esforço de raciocínio

No seletor de modelos, `Ctrl+E` abre os níveis declarados para o **modelo ativo**.
Setas escolhem; `Enter` salva explicitamente e `Esc` volta. A troca exige agente
ocioso e revalida o modelo, o protocolo e as capacidades antes de reconstruir o
provedor. A escolha não é aplicada ao navegar pela lista.

Os níveis não são inferidos pelo nome do modelo. O cadastro JSON pode declarar
`"can_reason": true` e `"reasoning_levels": ["low", "medium", "high"]` no objeto
do modelo, somente quando o endpoint documentar esses valores. O schema admite
`none`, `minimal`, `low`, `medium`, `high` e `xhigh`; a lista precisa corresponder
ao modelo. Consulte o [schema](../opcode-schema.json) para os demais campos.

Os controles atendem ao parâmetro de esforço dos adaptadores OpenAI compatíveis
com Chat Completions ou Responses. Não presumem orçamento de pensamento para
Anthropic/Gemini nem opções equivalentes para Ollama. Modelo sem capacidades
declaradas não ganha uma escolha fictícia. O provedor ainda pode recusar uma
declaração manual incorreta. Mais esforço pode aumentar tempo e consumo.

## Copiar respostas e código

`Ctrl+Y` abre as ações da resposta. A cópia nativa usa um executável da plataforma
com o texto enviado literalmente pela entrada padrão, sem shell, com prazo de
três segundos. No macOS usa `pbcopy`; Windows/WSL, `clip.exe`; Linux prefere
`wl-copy` em Wayland e depois `xclip`/`xsel`. Ferramenta ausente ou falha deixa a
seleção manual como alternativa.

Em SSH, a aplicação não chama o clipboard do servidor. A ação explícita de
copiar envia OSC 52 pela mesma saída serializada da TUI. O terminal local precisa
permitir esse protocolo; a interface informa **recebimento não confirmado**,
pois não há confirmação da área de transferência. Multiplexadores ou políticas
do terminal podem bloquear a entrega. O limite é 64 KiB por cópia.

`Ctrl+V` em SSH não consulta o clipboard do servidor. Cole pelo recurso do seu
terminal; o texto recebido segue o fluxo normal de colagem. Quando o foco está
no PTY de um processo, os controles continuam destinados àquele processo.

Os testes nativos usam executáveis descartáveis, nunca o clipboard real do
usuário. Os testes SSH verificam o conteúdo codificado e o limite; o aceite com
PTY confere a sequência OSC 52 na saída real da aplicação, sem afirmar que o
terminal de outra máquina a tenha aceitado.
