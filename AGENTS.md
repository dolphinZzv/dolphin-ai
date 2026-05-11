# AGENTS.md — DolphinzZ Development Guidelines

## Project Scope

DolphinzZ is an AI agent that connects to terminal, email, chat, and SSH — providing a unified agent experience across all transport layers. The agent runs shell commands, controls browsers, delegates to sub-agents, and follows user-defined schedules.

**Core principles:**
- Work on all channels equally (terminal, email, chat, SSH)
- Session state is preserved regardless of transport
- Safe by default: timeouts, allowlists, and explicit permission gates

## Code Quality

- Code must be stable, recoverable, observable, and testable
- All configuration via `config.yaml` or environment variables; no hardcoding
- Error messages must be clear and actionable; never swallow critical errors
- Use zap structured logging; INFO level on all critical paths

## Testing

**Coverage requirements:**
- Minimum 60% overall test coverage; 80%+ for critical paths
- Build and test with race detection: `go test -race ./...`

**Test boundaries:**
- Unit tests: isolated, fast, no external dependencies (mock everything)
- Integration tests: real components, local resources only (in-memory DB, tmp files)
- E2E tests: full transport layer (terminal, HTTP), real external dependencies

**Conventions:**
- Table-driven tests for both happy paths and edge cases
- Test files live next to the code they test: `foo.go` → `foo_test.go`
- Fixtures via `testdata/` directory or inline factories
- Test naming: `TestUnitOfWork_StateUnderTest_ExpectedBehavior`
- Every PR must preserve existing functionality; new code requires new tests

## Git Workflow

**Branch strategy (Gitflow):**
```
main          ← production-ready, always deployable
├── develop   ← integration point for features
├── feature/* ← topic branches, off develop
├── bugfix/*  ← hotfix branches off main, merge back to main + develop
└── release/* ← preparation for a release
```

**Working with branches:**
- Feature branches off `develop`: `feature/add-session-timeout`
- Bugfix branches off `main`: `bugfix/fix-browser-race-condition`
- Never commit directly to `main` or `develop`

**Commit rules:**
- One commit per independent logical change
- Commit message format:
  ```
  <type>(<scope>): <subject>

  <optional body>

  <optional footer>
  ```
  Types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`
- Subject: imperative mood, no period, max 72 chars
- Body: explain *what* and *why*, not *how*

**Pull Request process:**
1. Create PR against `develop` (features/bugfixes) or `main` (hotfixes)
2. Fill in PR template (description, testing done, breaking changes, screenshots)
3. At least 1 reviewer approval required
4. All CI checks must pass
5. Squash merge to maintain clean history

**Code review guidelines:**
- Reviewer: focus on correctness, security, design, and test coverage
- Author: respond to all comments, don't dismiss without explanation
- Changes requested → address or explain → re-request review
- No force push to main; never skip hooks

**Revert process:**
- If a commit causes issues, revert first, fix second
- `git revert <commit>` → PR → review → merge

## Security

**Secrets management:**
- NEVER hardcode credentials, tokens, keys, or secrets in code
- Use environment variables or `config.yaml` with secret references
- Secrets in config must be marked with `[SECRET]` suffix and never logged

**Dependency management:**
- Run `go mod verify` on every PR
- Dependency vulnerabilities: scan with `govulncheck ./...` in CI

**Input validation:**
- Validate all external input at boundary (user, API, file, environment)
- Use typed inputs; avoid `interface{}` when possible
- Log all rejected input with reason (INFO level, no sensitive data)

**Allowed operations:**
- Shell tool requires explicit allowlist for dangerous commands (`rm -rf`, `shutdown`, etc.)
- Browser tool: sandboxed where possible, screenshot on every action for audit

## Onboarding

**Local setup:**
1. Clone repo and read `README.md`
2. Set required env vars: `DZ_LLM_API_KEY`, `DZ_LLM_MODEL`
3. Run `go build ./...` to verify compilation
4. Run `go test -race ./...` to verify test suite
5. Start the agent with `./dolphinzZ` (first run walks you through setup)

**Common issues:**
- Build fails → check Go version matches `go.mod` (minimum 1.22)
- Tests fail with "connection refused" → ensure mock services are running
- Config parsing errors → verify YAML syntax and required fields

**Getting help:**
- Architecture overview: see `design/DESIGN.md`
- Proposed changes: submit to `proposals/`
- Questions: open an issue with `question` label

## Observability

**Structured logging (zap):**
- INFO: entering/leaving critical functions, key decisions, session events
- WARN: degraded functionality, retries, non-fatal errors
- ERROR: exceptions, failures that require attention

**Required log fields for critical operations:**
```go
zap.String("session_id", sessionID)
zap.String("tool", toolName)
zap.Duration("duration", elapsed)
```

**MCP tool calls:**
- Log call count per session in metrics
- Log latency histogram per tool

**Metrics endpoint:**
- Prometheus format, enabled by default at `/metrics`
- Include: request count, latency percentiles, error rate, active sessions

**Alerts (production):**
- ERROR log spike → page on-call
- Latency p99 > 1s → alert
- Error rate > 1% → alert

## Design Docs

**When to write a design doc:**
- Adding a new transport layer (email, chat, SSH)
- Changing session state architecture
- Adding a new tool integration (browser, shell)
- Any change that affects cross-component contracts

**What to include:**
1. Context and problem statement
2. Goals and non-goals
3. Proposed solution with architecture diagram
4. Alternatives considered and why they were rejected
5. Decision log (who decided, when, based on what)
6. Implementation plan and rollback plan

**Location:**
- Architecture decisions: `design/DESIGN.md` (update in place)
- Feature proposals: `proposals/<feature-name>.md`
- Reviews: PR description links to relevant design doc

## References

- panda client changes: see `./app/panda/AGENTS.md`