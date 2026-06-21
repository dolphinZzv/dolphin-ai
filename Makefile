BINARY ?= dolphin
CONFIG ?= config.yaml
GIT_HASH ?= $(shell git rev-parse --short HEAD)
LDFLAGS ?= -X dolphin/internal/common.Version=$(GIT_HASH)

RACE ?= 0

# go-build appends the race flag only when RACE=1, keeping daily builds fast.
define go-build
	mkdir -p bin && go build $(if $(filter 1,$(RACE)),-race) -ldflags "$(LDFLAGS)" -o bin/$(1) $(2)
endef

.PHONY: build
build:
	$(call go-build,$(BINARY),./cmd/dolphin)

# build-race enables the race detector for diagnosing concurrency issues.
.PHONY: build-race
build-race: RACE = 1
build-race:
	$(call go-build,$(BINARY),./cmd/dolphin)

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
push:
	git push

.PHONY: push-gitea
push-gitea:
	git push gitea

.PHONY: push-origin
push-origin:
	git push origin

.PHONY: push-fast
push-fast:
	git push --no-verify

.PHONY: init
init: setup

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
