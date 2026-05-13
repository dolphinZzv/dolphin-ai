# CI/CD Pipeline

## CI (`.github/workflows/ci.yml`)

| Step | Action |
|------|--------|
| Checkout | Fetch full history |
| Setup Go | Go 1.26 |
| Verify | `go mod verify` |
| Vet | `go vet ./...` |
| Test | `go test -race ./... -coverprofile=coverage.out` |
| Coverage | Upload to Codecov |
| Build | `go build ./...` |

## Release (GoReleaser)

Tag → GitHub Release with binaries + Docker multi-arch image.

## Docker

Multi-stage build, distroless base, `/dolphin` entrypoint.
