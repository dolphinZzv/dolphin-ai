BINARY ?= dolphin
CONFIG ?= config.yaml
GIT_HASH ?= $(shell git rev-parse --short HEAD)
LDFLAGS ?= -X dolphin/internal/common.Version=$(GIT_HASH)

.PHONY: build
build:
	mkdir -p bin && go build -race -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/dolphin

.PHONY: build-mail
build-mail:
	mkdir -p bin && go build -o bin/mail ./cli/mail

.PHONY: build-all
build-all: build build-mail

.PHONY: browser
browser:
	@bash mcp/browser/build.sh

.PHONY: browser-run
browser-run: browser
	open bin/BrowserMCP.app

.PHONY: browser-test
browser-test:
	cd mcp/browser && swift test --filter "BrowserMCPTests" 2>&1

.PHONY: browser-test-all
browser-test-all:
	cd mcp/browser && swift test --filter "MCPProtocolTests|BrowserMCPIntegrationTests" 2>&1

.PHONY: test
test:
	go test ./... -race -count=1

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: playground
playground: build
	cp bin/$(BINARY) ../playground/
	cp $(CONFIG) ../playground/

.PHONY: docs
docs:
	cd docs && hugo server -D

.PHONY: push
push: setup
	git push

.PHONY: setup
setup:
	git config core.hooksPath .githooks

.PHONY: clean
clean:
	rm -rf bin/
	rm -rf .dolphin/

.PHONY: run
run: build
	./bin/$(BINARY) -c $(CONFIG)
