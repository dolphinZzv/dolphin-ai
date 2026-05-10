# AGENTS.md — DolphinzZ Development Guidelines

## Code Quality

- Code must be stable, recoverable, observable, and testable
- All configuration via config.yaml or environment variables; no hardcoding
- Error messages must be clear and actionable; never swallow critical errors
- Use zap structured logging; INFO level on all critical paths

## Testing

- Every module requires thorough unit tests
- Minimum 60% test coverage; 80%+ for critical paths
- Build and test with race detection: `go test -race ./...`
- Every change must include unit tests ensuring existing functionality is preserved
- Use table-driven tests covering both happy paths and edge cases

## Git Workflow

- Gitflow workflow (feature/bugfix branches → main)
- One commit per independent change; commit message describes intent
- Pre-commit checks: `go build ./...` / `go test -race ./...` / `go vet ./...`
- No force push to main; never skip hooks

## Design Docs

- Code changes must sync with `design/DESIGN.md`
- Architecture decisions recorded in `design/` directory
- New feature proposals go in `proposals/` directory

## Observability

- Structured logging for critical operations (session_id, tool, duration)
- MCP tool calls must log call count and latency
- WARN or ERROR level for all exception paths
- Metrics endpoint enabled by default (Prometheus format)

## References

- panda client changes: see `./app/panda/AGENTS.md`
