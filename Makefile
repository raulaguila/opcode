.PHONY: test test-integration vet vet-desktop build build-desktop app release-local complexity staticcheck quality bench-performance bench-compile bench-compare

test:
	go test ./...

test-integration:
	OPCODE_LIVE_TESTS= OPCODE_LIVE_OLLAMA= go test -race -p 1 -tags integration ./...

vet:
	go vet ./...
	go vet -tags integration ./...

complexity:
	gocognit -over 30 .

staticcheck:
	staticcheck ./...
	staticcheck -tags integration ./...

quality: complexity staticcheck vet test test-integration

# Identical offline fixtures/toolchain are required for paired comparisons.
BENCH_PACKAGES := ./internal/llm/tools ./internal/llm/agent ./internal/llm/provider ./internal/message ./internal/tui/components/chat ./internal/tui/styles
BENCH_PATTERN := Benchmark(WorkspaceDigest|WarmCodeIndexSearch|FullWarmCodeIndexSearch|CitationCurrent|IndexRefreshChanges|ContextPreparation|PreparedImageBudget|AuditKnownTokenizer|ConversationProjection|AuditChat|SQLiteChatSessionLoad|TranscriptRender|AuditMarkdown)

bench-compile:
	OPCODE_LIVE_TESTS= OPCODE_LIVE_OLLAMA= go test -p 1 -run '^$$' $(BENCH_PACKAGES)

bench-performance:
	OPCODE_LIVE_TESTS= OPCODE_LIVE_OLLAMA= go test -p 1 -run '^$$' -bench '$(BENCH_PATTERN)' -benchmem -benchtime=100x -count=3 $(BENCH_PACKAGES)

bench-compare:
	go run ./scripts/bench-compare -before '$(BEFORE)' -after '$(AFTER)'

build:
	go build -trimpath -o bin/opcode .

# The native app window (Wails). Linux needs GTK 3 and WebKitGTK 4.1:
#   sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
# With WebKitGTK 4.0 drop webkit2_41; on macOS and Windows use DESKTOP_TAGS=desktop,production.
DESKTOP_TAGS ?= desktop,production,webkit2_41

build-desktop:
	go build -trimpath -tags '$(DESKTOP_TAGS)' -o bin/opcode-desktop .

vet-desktop:
	go vet -tags '$(DESKTOP_TAGS)' ./cmd/...

# Opens the app in its native window, on the current workspace.
app: build-desktop
	./bin/opcode-desktop app

# Archives/checksums only; no tags, pushes or publication.
VERSION ?= dev
TARGETS ?= linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64,windows/arm64
release-local:
	go run ./scripts/build-release -version '$(VERSION)' -targets '$(TARGETS)'
