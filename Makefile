BINARY ?= dolphin
CONFIG ?= config.yaml

.PHONY: build
build:
	go build -o $(BINARY) ./cmd/dolphin

.PHONY: test
test:
	go test ./... -count=1

.PHONY: test-race
test-race:
	go test ./... -race -count=1

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: playground
playground:
	mkdir -p ../playground
	go build -o ../playground/$(BINARY) ./cmd/dolphin
	cp $(CONFIG) ../playground/

.PHONY: clean
clean:
	rm -f $(BINARY)
	rm -rf .dolphin/

.PHONY: run
run: build
	./$(BINARY) -c $(CONFIG)
