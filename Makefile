.PHONY: all build test test-race test-integration generate clean coverage

# Build the server binary
build:
	go build -race -o bin/chick ./cmd/server/

# Run all unit tests (excluding generated code)
test:
	go test -race -count=1 ./internal/auth/ ./internal/config/ ./internal/events/ ./internal/matching/ ./internal/mcp/ ./internal/notifications/ ./internal/repository/gorm/ ./internal/server/ ./internal/service/

# Run all tests including GraphQL
test-all:
	go test -race -count=1 ./internal/...

# Run PostgreSQL integration tests
test-integration:
	go test -tags=integration -race -count=1 ./internal/...

# Generate GraphQL code from schema
generate:
	gqlgen generate

# Show code coverage (excluding generated code)
coverage:
	go test -count=1 -coverprofile=coverage.out ./internal/auth/ ./internal/config/ ./internal/events/ ./internal/matching/ ./internal/mcp/ ./internal/notifications/ ./internal/repository/gorm/ ./internal/server/ ./internal/service/
	go tool cover -func=coverage.out | grep total

# Show coverage in browser (all packages)
coverage-html:
	go test -count=1 -coverprofile=coverage.out ./internal/...
	go tool cover -html=coverage.out

# Clean build artifacts
clean:
	rm -rf bin/ coverage.out
