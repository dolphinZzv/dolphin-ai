# AGENTS.md — Dolphin Development Guidelines

> **All changes MUST strictly follow the 9-step process defined in `workflow/change-flow.md`. Skipping any step is prohibited.**

## Change Flow (Summary)

```
1. Requirement/Bug → archive in todo/ or feature/ with issue number
2. Agent self-review requirements (round 1) — ask if unclear
3. Output design doc to design/ or write a clear solution
4. Agent self-review design (round 2) — revise if problematic
5. Create feature/bugfix branch, code strictly per design
6. Unit tests: go test -race ./internal/... -count=1 100% pass
7. Agent self-review code (round 2) — verify requirements + edge cases + concurrency safety
8. Commit code → self-evaluate impact scope, rollback plan, compatibility
9. Ask user: merge or improve
```

## Project Scope

Dolphin is a cross-terminal/email/chat/SSH AI agent that runs shell commands, controls browsers, dispatches sub-agents, and follows scheduled tasks.

## Core Constraints

- All configuration via `config.yaml` or environment variables, no hardcoding
- Use zap structured logging, INFO level for critical paths
- Code must be stable, recoverable, observable, and testable
- Test coverage: 60%+ overall, 80%+ for critical paths
- Tests with race detection: `go test -race ./...`
- Publishing to GitHub requires 100% CI pass rate — never merge while CI is failing

## Design Docs

- Architecture design in `design/` directory
- New features/cross-component changes must update design docs first, then code
- Design doc path: `design/modules/<module-name>.md`
- Every document must include a last-modified timestamp at the end: `<!-- last-modified: YYYY-MM-DD -->`

## Security

- No hardcoded credentials or keys; use environment variables or `config.yaml` secret references
- Shell tool requires explicit allowlist for dangerous commands
- All external input must be validated; log rejection reasons

## References

- Change Flow: `workflow/change-flow.md`
- Issue Flow: `workflow/issue-flow.md`
- Git Branching: `workflow/git-branching.md`
- CI/CD: `workflow/ci-cd.md`
- Architecture: `design/README.md`
- Panda client: `./app/panda/AGENTS.md`

<!-- last-modified: 2026-05-18 -->
