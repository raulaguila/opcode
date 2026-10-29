# Arquivos de distribuição locais

O build normal é `go build -trimpath -o bin/opcode .`. Para gerar arquivos de
distribuição e SHA256 sem tags, push ou publicação:

```sh
go run ./scripts/build-release -version dev
# Somente a plataforma escolhida, com destino explícito:
go run ./scripts/build-release -version dev -targets linux/amd64 -output dist/local
# Equivalente pelo Makefile:
make release-local VERSION=dev TARGETS=linux/amd64
```

Os alvos de compilação são Linux, macOS e Windows, amd64/arm64. Linux/macOS usam
tar.gz com `opcode`; Windows usa ZIP com `opcode.exe`. Todos incluem somente
`LICENSE`, `README.md` e o binário; `checksums.txt` contém SHA256 ordenados.
O builder usa CGO desativado, `-trimpath`, `-buildvcs=false` e build ID vazio,
normaliza metadados/timestamps do tar/gzip/ZIP e não inclui configurações privadas.
Com os mesmos fontes, dependências, versão e toolchain Go, duas execuções para
o mesmo alvo devem produzir arquivos idênticos. Mudanças em README/LICENSE
também alteram o checksum, como esperado.

O workflow `build` gera os seis arquivos e os guarda como artefatos do CI.
`release.yml` continua separado, disparado somente pelo push explícito de uma
tag `v*`; o builder local não o aciona. `scripts/release` prepara uma tag local
mediante chamada explícita e não é necessário para validar estes arquivos.

Após extrair, valide `sha256sum -c checksums.txt` (ou a ferramenta SHA256 da sua
plataforma) no diretório dos arquivos. Execute `./opcode --version` e
`./opcode doctor --cwd /seu/projeto`. O diagnóstico não instala ferramentas nem
altera a exigência de sandbox. O binário Windows oferece CLI/configuração e
renderização testadas pelo smoke nativo do CI; PTY Windows continua indisponível.

## Versão consolidada

A versão 0.2.0 reúne configuração guiada, contratos de tarefa, diagnóstico do
Ollama e Git/worktrees, além da evolução anterior. O [changelog](../CHANGELOG.md)
e [validação](validation.md) registram comportamento e evidências. Builds de
fontes usam a versão do build info (ou `0.2.0-dev`), com revisão/estado quando disponíveis; arquivos de
release recebem a versão exata via `-ldflags`.

A tag `v0.2.0` aciona o workflow de release: race tests e vet precedem o
GoReleaser. Não acrescente arquivos de configuração, backups, bancos, logs nem
relatórios privados aos artefatos. Os arquivos locais incluem somente binário,
README e licença. macOS/ARM são cross-compilados; isso não equivale a executar
testes manuais em cada equipamento.


## Smoke nativo e limites por plataforma

O workflow `test` mantém `go test -race ./...` e `go vet ./...` no Linux, além de
verificar drift da matriz de linguagens. Um job separado executa uma seleção
explícita no sistema nativo dos runners Linux, macOS e Windows: referências de
cofre com backend falso, permissões/recusa, composer, seleção Unicode, busca,
geometria de modais, negação de sandbox indisponível e arquivos reproduzíveis.
No Unix, uma fixture inerte verifica a primitiva PTY; isso não altera a política
de execução da aplicação. No Windows o smoke exige recusa clara do PTY.

O binário nativo também passa por `--help`, `--version`, `config path`, `config
validate` e `config show --redacted`; o startup com prompt e sem modelo precisa
criar JSON/template e parar **antes de inferência**. O segundo startup restaura
o exemplo sem sobrescrever a configuração-sentinela do usuário. HOME,
USERPROFILE, APPDATA, LOCALAPPDATA e XDG são isolados; o runner não herda chaves,
URLs de descoberta ou endereços de sessão de cofre.

Para reproduzir o smoke do binário:

```sh
go build -trimpath -o bin/opcode .
go run ./scripts/platform-smoke -binary ./bin/opcode
# Windows: compile para bin/opcode.exe e passe esse caminho em -binary.
go test ./internal/llm/tools/shell -run '^TestNativeSmoke'
```

O smoke não usa Ollama, acessa cofre real, instala ferramentas ou publica nada.
A avaliação Ollama/PTY completa permanece opt-in e separada. Execuções macOS e
Windows precisam ocorrer nos respectivos runners; compilar esses alvos em Linux
não equivale a executá-los. O CI testa a arquitetura do runner, não todos os
pares amd64/arm64 dos arquivos de distribuição.

O executor ainda usa shell POSIX (`/bin/bash` por padrão) e Bubblewrap Linux.
Fora do Linux, o sandbox padrão recusa comandos com explicação; não é desativado
automaticamente. Execução sem sandbox exige configuração global explícita e um
shell compatível instalado; não prometemos PowerShell/ConPTY nem testes completos
Windows. Fixtures POSIX de processos, fake executáveis sem `.exe` e alguns testes
de symlinks ainda precisam ser portados antes de uma suíte integral nativa nesses
sistemas. Não há skips genéricos que convertam essa limitação em suporte.

ZIP no GoReleaser usa o [override oficial por sistema](https://goreleaser.com/customization/package/archives/).
A configuração de release continua restrita a tags explícitas; esta evolução
não cria tags nem publica releases.
