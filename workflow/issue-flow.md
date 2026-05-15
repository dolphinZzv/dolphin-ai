# Issue Flow

**Integrated with the Chick task management system, linked with change-flow.**

## State Machine

```
open → in_progress → review → closed_completed ←─┐
                   → blocked ──→ open (unblocked) │
                   → closed_not_planned            │
                                         closed_completed → open (regression)
```

## Flow Chart

```text
┌────────────────────────────────────────────────────┐
│ 1. Bug / Feature / Docs issue discovered            │
│    ├─ Agent self-discovers → create issue directly │
│    ├─ User reports → create issue + link convo     │
│    ↓                                               │
│ 2. Triage & prioritize                              │
│    ├─ Type: bug / feature / docs / chore            │
│    ├─ Priority: critical / high / medium / low      │
│    │  ├─ critical: core broken, data security       │
│    │  ├─ high: major feature broken, severe UX      │
│    │  ├─ medium: minor feature, doc mismatch        │
│    │  └─ low: typos, minor improvement              │
│    ↓                                               │
│ 3. Assign (Chick_assign_issue)                      │
│    ├─ First use Chick_list_agents to get agent IDs │
│    ├─ Assign to a specific agent (agentId)          │
│    ├─ No assignee → leave unassigned                │
│    ↓                                               │
│ 4. Confirm reproduction / clarity                  │
│    ├─ Cannot reproduce → comment + closed_not_plan │
│    ├─ Needs clarification → comment → blocked       │
│    ↓                                               │
│ 5. Enter development flow (change-flow)             │
│    ├─ Write issue number into todo/ or feature/     │
│    ├─ Strictly follow change-flow.md 9 steps       │
│    ├─ Commit message references: "fix(#168): ..."  │
│    └─ Design doc notes "related to issue #xxx"     │
│    ↓                                               │
│ 6. State transitions (Chick_transition_issue)       │
│    ├─ Starting fix → in_progress                    │
│    ├─ Waiting for user / external dep → blocked     │
│    ├─ Code review passed → review                   │
│    ↓                                               │
│ 7. Verification & close                             │
│    ├─ User confirms fix → closed_completed          │
│    ├─ Not a bug / won't fix → closed_not_planned    │
│    └─ Closing comment: reason + key commit hash     │
└────────────────────────────────────────────────────┘
```

## Key Rules

| # | Rule |
|---|------|
| 1 | Every issue MUST have a **type** and **priority** |
| 2 | State transitions MUST use `Chick_transition_issue`, never close directly |
| 3 | Code changes MUST reference the issue: commit format `fix(#168): msg` / `feat(#167): msg` |
| 4 | Unreproducible bugs → comment explanation then `closed_not_planned` |
| 5 | Blocked state MUST include the blocking reason and expected unblock condition |
| 6 | Related issues found in the same session MUST cross-reference each other in comments |
| 7 | Regression of a `closed_completed` issue → `Chick_transition_issue` back to `open`, do NOT create a new issue |
| 8 | Only `docs/`, `internal/i18n/` allow non-English. Everything else: **English only**. Issue title/desc/comments use **Chinese**, commit messages use **English**. Docs-related issues MUST tag `[zh]`/`[en]`/`[zh/en]` |

## State Definitions

| State | Meaning | Operator |
|-------|---------|----------|
| `open` | Created, pending triage | Agent / User |
| `in_progress` | Being worked on | Agent |
| `blocked` | Waiting for external input | Agent |
| `review` | Under code review | Agent (self-review) |
| `closed_completed` | Fixed and closed. Reopen `→ open` on regression | Agent / User |
| `closed_not_planned` | Won't fix | Agent / User |

## Comment Guidelines

When adding a comment (`Chick_add_comment`), include:

- **Bug**: reproduction steps + environment info + sanitized config/logs
- **Feature**: use case + expected behavior + reference design (if any)
- **Docs**: file location + current content + expected content
- **Closing comment**: reason + key commit hash + impact scope

## issue → change-flow Linkage

```
                                 change-flow
issue (open) ──────────────────→ 1. todo/feature archive (ref issue #)
↓                                 2. Agent self-review requirements
│  Chick_transition_issue         3. Output design doc
│  → in_progress                  4. Agent self-review design
│                                 5. Create branch, write code
│                                 6. Unit tests
│                                 7. Agent self-review code
│  Commit: "fix(#168): ..."       8. Commit code
│  Chick_transition_issue
│  → review                      
│  Code review passed             
↓                                 9. User confirms merge
issue → closed_completed ←──────  (merge → close)
```

Mapping:

| issue State | change-flow Steps | Trigger |
|------------|-------------------|---------|
| `open` | 1-4 (archive → design pass) | Issue created |
| `in_progress` | 5-8 (coding → commit) | Start coding |
| `review` | 7-8 (review → commit) | Code self-review passed, ready to commit |
| `blocked` | any step | Waiting for external input |
| `closed_completed` | 9 (merge) | User confirms merge |
| `closed_not_planned` | — | Confirmed won't fix |

## Multilingual Standards

### Code and Documentation Language Boundaries

| Area | Allowed Languages | Notes |
|------|------------------|-------|
| `docs/` | **Chinese / English** | Bilingual content, separate files |
| `internal/i18n/` | **Chinese / English** | Backend i18n resource files |
| Code comments | **English only** | Comments, variable names, log strings must be English |
| `AGENTS.md`, `workflow/` | **English only** | Project guidelines and workflow docs |
| `design/` | **English only** | Technical design documents |
| Other `.md` files | **English only** | README etc. in project root |
| Navigation links | **Chinese OK** | Links pointing to `docs/zh/` may use Chinese |

**Exceptions**: Only `docs/content/zh/`, `docs/zh/`, `internal/i18n/` may contain Chinese.

### Issues and Comments

| Scenario | Language | Notes |
|----------|----------|-------|
| Issue title | **Chinese** | Primary project communication language |
| Issue description | **Chinese** | Detailed description in Chinese |
| Comments | **Chinese** | Match the issue language |
| Commit message | **English** | Git convention: `fix(#168): msg` |
| Docs-related issues | **Tag language** | Prefix with `[zh]` / `[en]` / `[zh/en]` |

## Notification Handling

Use `Chick_check_notifications` to periodically check notifications:

- New issue notification → evaluate type and priority
- State change notification → track related issue progress
- Comment notification → reply promptly
