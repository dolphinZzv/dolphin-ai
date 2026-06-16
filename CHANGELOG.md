# Changelog

All notable changes to Dolphin will be documented in this file.

## [Unreleased]
- Add version.go for build-time version injection
- /context list uses 1-based numbered format (1) base, 2) soul, ...)
- Add /session stop and /session continue to pause and resume turn processing
- Add speedtest workflow

### Changed

- Merged create/update/delete into upsert for scheduler, subscriptions, commands, scripts, skills to reduce tool count and per-turn context.
- TUI status bar now displays git hash version alongside agent name (e.g., `🐬 Dolphin abc1234`).
- TUI queue display now properly renders after boot; SetAgentIO send deferred until program ready.

### Added

- TUI transport with adaptive terminal theming, markdown rendering, and permission dialogs.
- Ctrl+Enter in TUI sends message as priority turn, jumping to the front of the agent queue.
- Priority queue channel in AgentIO and AgentLoop for front-of-queue turn dispatch.
- TUI status bar shows session usage (tokens, rounds) and limit caps with percentage.
- TUI live queue display showing active and pending turns above the input area.
- `config init` command to generate a default config.yaml file.

### Fixed

- Turn timeout now covers the entire turn (init + loop stages), not just each loop round.
- LLM HTTP client now derives timeout from context deadline, preventing hung connections.
- SubscriptionEngine data race on `running` field (switched to atomic.Bool).
- DroppingMemory no longer orphans tool_result messages when truncating the message window.
- Nil pointer panic in status command when no active session exists.
- Per-model `stream`, `temperature`, `top_p` configuration in provider model lists.
- Non-streaming LLM path (`CompleteOpenAI`) for models that don't support SSE.
- LLM request hooks (`internal/llm/models/`) for per-model request rewriting.
- DeepSeek V4 Pro preset: default `reasoning_effort=high`.
- `/status` command shows current model's `temperature` and `top_p`.
- Python test scripts for OpenAI and Anthropic API validation (`scripts/`).
- Sticky floating indicator at top showing current user message with pending/success/error state.
- GitHub Issue auto-review system: poller script (`scripts/github-issue-poll.sh`) cross-repo, deduped via `.dolphin/github-issues-state.json`, cron every 5min, subscription `review-assigned-issues` triggers LLM analysis + auto-label + reply draft on `file.update` of `.dolphin/issue-changes.json`.

### Changed

- TUI theme system replaced with terminal-native adaptive colors via lipgloss.AdaptiveColor.
- Status bar stays single-line, dropping low-priority parts when content exceeds terminal width.
- Status bar req/tok simplified to percentage only; added tool call count display.
- TUI `show_tools`, `show_thinking`, `workmode` now read from config; config values take priority over persisted prefs.
- TUI status bar shows `pool_size` and `tool_parallelism` when above defaults.
- Thinking continuation lines padded to align with content text.
- gofmt formatting fixes across changed files.
- Fix agentloop LLMRequest missing `Stream: true`, causing non-streaming path and DeepSeek API 400 error.
- Per-round turn timeout: each agent loop round gets a fresh timeout so long-running tools don't starve subsequent LLM calls.
- Tool parallelism config (`agent.tool_parallelism`) for concurrent tool execution.
- Workflow agent-driven e2e tests.

### Changed

- User input text color changed to green in both TUI themes.
- Markdown rendering preserves ANSI color codes for syntax highlighting.
- Various lint and formatting fixes for the TUI transport.
- `os.Exit` made replaceable via package-level `osExit` variable for testability.
- Transport test coverage: `stdio.go` improved from 67.8% to 92.1% with pipe-backed readline tests.
- TUI test coverage: all changed files (`model.go`, `tui.go`, `renderer.go`, `theme.go`, `perm_dialog.go`) now above 80%.
- TUI viewport uses incremental rendering: only re-renders the changed message tail instead of rebuilding all content on every append.
- Replace range loop with variadic append for block offsets in incremental renderer.
- TUI messages capped at 500 entries with front-trim to prevent memory exhaustion in long conversations.
- TUI streaming text bypasses glamour markdown rendering to avoid O(n²) re-render cost; block is finalized on next non-text event.
- TUI e2e tests: 8 integration tests covering streaming conversation, permission flow, multi-turn memory, theme switch, and dirty block lifecycle.
- TUI benchmarks: 10 benchmark suites measuring render performance, streaming vs non-streaming paths, and markdown rendering cost.
- TUI `Flush()` now sends `flushMsg` to program to finalize dirty text blocks; previously dirty blocks persisted until the next non-text event.

### Breaking Changes

- **Stage.Clone()** is now a required method on the `Stage` interface. External `Stage` implementations must add a `Clone() Stage` method. See `docs/adr/002-compositor-clone.md` for rationale.
- **Engine.NewEngine** signature changed: removed `brain *brain.Brain` parameter (was stored but never used).
- **AgentIO.SetProcessing** removed. This method was a no-op kept for backward compatibility. Use `ActiveSnapshot()` or `QueueSnapshot()` to inspect worker state.

### Added

- Worker pool support: `agent.pool_size` config (default 1) enables multi-worker turn processing.
- Chaos tests: context cancellation under session lock, compositor panic recovery, 100 concurrent same-session turns.
- Fuzz tests: `FuzzValidSessionID`, `FuzzSessionLock`, `FuzzCompileTemplate`.
- Benchmarks: `BenchmarkAgentLoopSingleTurn`, `BenchmarkAgentLoopMultiWorker`, `BenchmarkCompileTemplate`, `BenchmarkRenderPrompt`.
- Architecture Decision Records: `docs/adr/001-per-session-lock.md`, `docs/adr/002-compositor-clone.md`, `docs/adr/003-workflow-no-session-memory.md`.
- Workflow context section expanded with complete YAML format reference, template syntax, foreach, checkpoint, and file lifecycle instructions for the LLM.

### Changed

- Session lock GC interval is now configurable via `agent.session_gc_interval` (default 300s).
- `SetActive`/`ClearActive` moved after session lock acquisition — elapsed time in `/queue` no longer includes lock wait.
- Agentloop tests replace `time.Sleep` with channel/waitgroup synchronization — test suite runtime reduced ~40%.

### Removed

- `workflow.max_steps` config key (was never read).
- `Engine.brain` field (was never referenced).
- `AgentIO.SetProcessing` method (was a no-op).

### Fixed

- Worker panic recovery: exponential backoff (1s, 2s, 4s...) capped at 30s, worker exits after 5 consecutive panics. Publishes `EventWorkerPanic` event on every panic including the final one.
- `truncateInput`/`truncateForMarkdown` duplicate functions merged.
- `sortedWorkerIDs` replaced bubble sort with `sort.Strings`.
- Error wrapping: all `fmt.Errorf` calls that pass an error use `%w`, enabling `errors.Is`/`errors.As` throughout the codebase.
- Data races in `-race` CI: fixed races in `command.Execute` (lock entire body), `ExecuteWithTimeout` (pre-check ctx.Err), `weWork.sendAndWait` test, `watcher` test. Skipped readline-based stdio tests under `-race` (third-party race in `github.com/chzyer/readline`).
- golangci-lint: fixed gofmt, exhaustive, gosec, and staticcheck issues in transport, workflow, limit, and llm packages.
- Workflow path resolution: `run_workflow` now resolves file paths relative to the brain directory, fixing a mismatch where `brain_write` wrote to the brain dir but `run_workflow` read from CWD.
- Workflow step timeout: `run_workflow` handlers now use `context.WithoutCancel` to strip the tool pipeline's 30s timeout. Workflow steps can now be configured to never timeout by setting `workflow.step_timeout: "0s"` or per-step `timeout: "0s"`.
	- Root OpenAI/Anthropic providers now route to non-streaming HTTP path when `Stream: false`, matching the volcengine provider pattern. Adds `CompleteAnthropic` for non-streaming Anthropic API calls.
	- gofmt formatting fixes across changed files.
	- Add test coverage for temperature and topP display in `/status` command.
	- Add test coverage for LLM request hook registration and dispatch.
	- Add test coverage for non-streaming OpenAI and Anthropic HTTP completion paths.
	- Fix data race in TUI TestRequestPermission_ReplyPaths: protect permCh access with mutex.
