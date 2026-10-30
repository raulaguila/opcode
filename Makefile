.PHONY: test test-integration vet build release-local complexity staticcheck quality

test:
	go test ./...

test-integration:
	OPCODE_LIVE_TESTS= OPCODE_LIVE_OLLAMA= go test -race -p 1 -tags integration ./...

vet:
	go vet ./...
	go vet -tags integration ./...

complexity:
	gocognit -over 15 .

staticcheck:
	staticcheck ./...
	staticcheck -tags integration ./...

quality: complexity staticcheck vet test test-integration

build:
	go build -trimpath -o bin/opcode .

# Archives/checksums only; no tags, pushes or publication.
VERSION ?= dev
TARGETS ?= linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64,windows/arm64
release-local:
	go run ./scripts/build-release -version '$(VERSION)' -targets '$(TARGETS)'
