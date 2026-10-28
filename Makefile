.PHONY: test vet build release-local

test:
	go test ./...

vet:
	go vet ./...

build:
	go build -trimpath -o bin/opcode .

# Archives/checksums only; no tags, pushes or publication.
VERSION ?= dev
TARGETS ?= linux/amd64,linux/arm64,darwin/amd64,darwin/arm64
release-local:
	go run ./scripts/build-release -version '$(VERSION)' -targets '$(TARGETS)'
