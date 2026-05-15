# Change Flow

**Principle: Design first, code follows design.**

```text
┌─────────────────────────────────────────────────┐
│ 1. User submits requirement, issue, or bug       │
│    ├─ First create a Chick issue for triage      │
│    ├─ Then archive the number in todo/ or feature/│
│    ↓                                            │
│ 2. Agent self-review requirements (round 1)     │
│    ├─ Unclear → ask user for clarification       │
│    ↓                                            │
│   Agent self-review requirements (round 2)      │
│    ├─ Still unclear → continue asking            │
│    ├─ Pass                                       │
│    ↓                                            │
│ 3. Design — output design doc to design/ or      │
│    write a clear solution                        │
│    ↑←───────────────┐                          │
│    ↓                 │                          │
│ 4. Agent self-review design (round 1)            │
│    ├─ Issues → ──────┘ revise design             │
│    ↓                                            │
│   Agent self-review design (round 2)            │
│    ├─ Still issues → revise design → back to r1 │
│    ├─ Pass                                       │
│    ↓                                            │
│ 5. Create feature/bugfix branch per function,     │
│    code strictly per design                      │
│    ├─ User feedback → sync update design doc     │
│    ↓                                            │
│ 6. Unit tests — all new code must have tests     │
│    ├─ go test -race ./internal/... -count=1 100% │
│    ├─ Fail → back to step 5 to fix code          │
│    ↓                                            │
│ 7. Agent self-review code (round 1)              │
│    ├─ Check incomplete requirements line by line │
│    ├─ Check edge cases, error handling, concurrency│
│    ├─ Issues → back to step 5 → re-review        │
│    ├─ Design issues → back to step 3 to fix      │
│    ↓                                            │
│   Agent self-review code (round 2)               │
│    ├─ Still issues → back to step 5 → round 1   │
│    ├─ Pass                                       │
│    ↓                                            │
│ ► Commit code (git commit)                       │
│    ↓                                            │
│ 8. Agent self-evaluate — impact scope, rollback, │
│    compatibility                                 │
│    ↓                                            │
│ 9. Ask user: improve or merge                   │
│    ├─ Merge → Agent creates PR, requests merge    │
│    ├─ Improve → back to step 1, restart flow     │
└─────────────────────────────────────────────────┘
```

## Key Rules

| # | Rule |
|---|------|
| 1 | Bug/Feature/Docs → first create Chick issue for triage, then archive in `todo/` or `feature/`. See `workflow/issue-flow.md` |
| 2 | Requirements must pass two rounds of agent self-review; ask if unclear |
| 3 | Design must output a doc to `design/`; coding only after two rounds of self-review |
| 4 | Create feature/bugfix branch per function; code strictly follows design |
| 5 | User feedback must sync-update the design doc |
| 6 | Unit tests `go test -race ./internal/... -count=1` must pass 100% |
| 7 | Code must pass two rounds of agent self-review: verify incomplete requirements + edge cases, error handling, concurrency safety |
| 8 | Self-evaluate impact scope, rollback plan, and compatibility before commit |
| 9 | Finally ask user: merge or improve |
