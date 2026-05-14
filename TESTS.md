# Tests

Verifier: **root** | Date: **2026-05-14 09:46**

---

## Unit Tests

Run: `make test` (`go test -race -count=1 ./...`)

| Package | Tests | Time | Coverage | Verified | File |
|---------|-------|------|----------|----------|------|
| agent | AgentDef, AgentPool, ChannelIO, Compressor, Config, Coordinator, Loop, OpenAI, E2E, Summary, Turn | 5.244s | 52.2% | root 2026-05-14 | [agent/](internal/agent/) |
| command | Parse, Manager, Load, MultiDir, Usage, Reload | 1.101s | 96.9% | root 2026-05-14 | [command/](internal/command/) |
| config | ConfigGen, Career, EnvOverrides, ProjectDetect, RepoFetcher, SSH, Merge | 1.513s | 46.4% | root 2026-05-14 | [config/](internal/config/) |
| context | Builder, LoadFiles, Cache | 1.060s | 81.5% | root 2026-05-14 | [context/](internal/context/) |
| diary | Sync, Prune, Lock, AtomicWrite, Summary | 2.291s | 78.2% | root 2026-05-14 | [diary/](internal/diary/) |
| event | Emit, Subscribe, Webhook, LogWriter | 2.463s | 84.9% | root 2026-05-14 | [event/](internal/event/) |
| hook | Registry, Priority, Abort, Rewrite | 1.088s | 100.0% | root 2026-05-14 | [hook/](internal/hook/) |
| mcp | Shell, CDP, Email, Webhook, Registry, SSE, Client | 6.613s | 51.1% | root 2026-05-14 | [mcp/](internal/mcp/) |
| metrics | Counter, Gauge, Histogram, Timer, Render | 1.051s | 81.6% | root 2026-05-14 | [metrics/](internal/metrics/) |
| plugin | Manager, LoadScripts, HookScript | 1.070s | 75.3% | root 2026-05-14 | [plugin/](internal/plugin/) |
| scheduler | Parse, AddTask, RemoveTask, Cron, Due, Persist | 1.474s | 88.2% | root 2026-05-14 | [scheduler/](internal/scheduler/) |
| session | Manager, LogMessage, Summary, ReadEvents | 1.166s | 68.1% | root 2026-05-14 | [session/](internal/session/) |
| skill | Parse, Manager, Search, TopSkills | 1.050s | 67.2% | root 2026-05-14 | [skill/](internal/skill/) |
| transport | Stdio, SSH, MQTT, Email, EmbeddedBroker | 1.681s | 45.6% | root 2026-05-14 | [transport/](internal/transport/) |
| **Total** | **~270 tests** | **~29s** | **52.2%** | root 2026-05-14 | |

## Smoke Tests

### LLM Smoke Test

| Scenario | Time | Result | Verified | File |
|----------|------|--------|----------|------|
| Valid key — send "abc 第一个字是什么？只回答一个字", verify LLM returns "a" | ~3.6s | ✅ PASS | root 2026-05-14 | [scripts/llm-smoke.sh](scripts/llm-smoke.sh) |
| Invalid key — send request with bad API key, verify auth error message | ~5s | ✅ PASS | root 2026-05-14 | [scripts/llm-smoke.sh](scripts/llm-smoke.sh) |

### Docs Smoke Test

| Check | Time | Result | Verified | File |
|------|------|--------|----------|------|
| Config examples are valid YAML | ~0.1s | ✅ PASS | root 2026-05-14 | [scripts/docs-smoke.sh](scripts/docs-smoke.sh) |
| DeepSeek config consistency (zh docs match actual config) | ~0.1s | ✅ PASS | root 2026-05-14 | [scripts/docs-smoke.sh](scripts/docs-smoke.sh) |
| Internal .md links are not broken | ~0.2s | ✅ PASS | root 2026-05-14 | [scripts/docs-smoke.sh](scripts/docs-smoke.sh) |
| README has required sections (install, config) | ~0.1s | ✅ PASS | root 2026-05-14 | [scripts/docs-smoke.sh](scripts/docs-smoke.sh) |

### Docs without automated validation

The following documentation files exist but have no automated testing. They should be reviewed manually for accuracy.

| Doc | Reason | Verified |
|-----|--------|----------|
| [design/](design/) — all design docs | Visual architecture docs, need human review | root 2026-05-14 |
| [workflow/](workflow/) — process docs | Process definitions, not code-configurable | root 2026-05-14 |
| [AGENTS.md](AGENTS.md) | Development guidelines, changes require human review | root 2026-05-14 |
| [CLAUDE.md](CLAUDE.md) | Project instructions for AI, validated by usage | root 2026-05-14 |
| [README.md](README.md), [README.zh.md](README.zh.md) | User-facing docs, partially validated (sections exist) | root 2026-05-14 |
| [docs/en/INSTALL.md](docs/en/INSTALL.md), [docs/zh/INSTALL.zh.md](docs/zh/INSTALL.zh.md) | Install guides, partially validated (deepseek config) | root 2026-05-14 |

## CI Workflows

| Workflow | Trigger | Steps | Verified | File |
|----------|---------|-------|----------|------|
| CI | push (main/tags), PR | fmt → vet → build → test → coverage → docker release | root 2026-05-14 | [ci.yml](.github/workflows/ci.yml) |
| LLM Smoke Test | push (main/tags), PR, manual | build → valid key test → invalid key test | root 2026-05-14 | [llm-smoke.yml](.github/workflows/llm-smoke.yml) |
| Docs Smoke Test | push (main), PR on docs | install deps → config validation → link check → README check | root 2026-05-14 | [docs-smoke.yml](.github/workflows/docs-smoke.yml) |
