BINARY ?= dolphin
CONFIG ?= config.yaml

.PHONY: build
build:
	mkdir -p bin && go build -o bin/$(BINARY) ./cmd/dolphin

.PHONY: build-mail
build-mail:
	mkdir -p bin && go build -o bin/mail ./cli/mail

.PHONY: build-all
build-all: build build-mail

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
