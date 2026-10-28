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

Os alvos suportados são Linux e macOS, amd64/arm64. Cada tar.gz contém `opcode`,
`LICENSE` e `README.md`; `checksums.txt` contém SHA256 ordenados por arquivo.
O builder usa CGO desativado, `-trimpath`, `-buildvcs=false` e build ID vazio,
normaliza metadados/timestamps do tar/gzip e não inclui configurações privadas.
Com os mesmos fontes, dependências, versão e toolchain Go, duas execuções para
o mesmo alvo devem produzir arquivos idênticos. Mudanças em README/LICENSE
também alteram o checksum, como esperado.

O workflow `build` gera os quatro arquivos e os guarda como artefatos do CI.
`release.yml` continua separado, disparado somente pelo push explícito de uma
tag `v*`; o builder local não o aciona. `scripts/release` prepara uma tag local
mediante chamada explícita e não é necessário para validar estes arquivos.

Após extrair, valide `sha256sum -c checksums.txt` (ou a ferramenta SHA256 da sua
plataforma) no diretório dos arquivos. Execute `./opcode --version` e
`./opcode doctor --cwd /seu/projeto`. O diagnóstico não instala ferramentas nem
altera a exigência de sandbox. PTY para Windows ainda não faz parte dos alvos
de distribuição.

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
