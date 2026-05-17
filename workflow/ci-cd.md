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

## Windows CI (`ci.yml` → `windows` job)

In addition to Go vet/build/test, the Windows job also builds the C# WebHost:

| Step | Action |
|------|--------|
| Setup .NET SDK | 5.0.x via `setup-dotnet` |
| NuGet cache | Cache `~/.nuget/packages` |
| Build WebHost | `dotnet build deps/win/webhost/src/WebHost/WebHost.csproj -c Release` |

## Release (GoReleaser)

Tag → GitHub Release with binaries + Docker multi-arch image.

## Docker

Multi-stage build, distroless base, `/dolphin` entrypoint.
