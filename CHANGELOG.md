# Changelog

All notable changes to Dolphin will be documented in this file.

## [Unreleased]
- Permission dialog: `a (always)` requires double-press to confirm (safety guard against accidental permanent grants). Dialog remains 3-choice: once/always/deny. Reverted abort/yolo additions.
- Slash-command Tab-completion: `/` prefix triggers autocomplete from cobra registry + TUI-only commands (`/tools`, `/thinking`, `/windows`, `/exit`). Completions popup renders between queue and input. Tab cycles matches.
- Mouse wheel scrolling enabled in TUI viewport via `tea.WithMouseCellMotion()`.
- Input history: up/down arrow navigates previously submitted messages (last 100, dedup consecutive). Bubble Tea textarea provides built-in readline keybindings (Ctrl+A/E/W/U/K, Alt+B/F, etc.).
- **Permission system overhaul**: `always` now grants per-tool access (glob match) instead of exact-parameter matching, so approving `shell` once covers all future shell commands. Deny rules checked first and take priority over allow/safe-command auto-approval. Built-in safe commands (`ls`, `pwd`, `cat`, `grep`, etc.) auto-allow without prompting when no rule matches. Config-driven deny defaults (`permission.deny`) act as a safety net blocking dangerous operations like `rm *`, `sudo *`, `dd *` regardless of runtime grants.
- **Compaction**: auto-compaction now has a dedicated `compaction` config section (schema + defaults). `/session compaction` and `/compaction` commands for on-demand manual compaction — always compacts regardless of token threshold, with dynamic keepRounds adjustment to ensure at least one round gets summarized. `Compact()` exported from `CompactionStage`.
- **`/session dump` and `/dump`**: write the last turn's LLM request/response + tool calls to `<session.dump_dir>/<session_id>.json`. `dump` package with `Recorder` + `TurnDump` struct; recorded per-turn in `AgentLoop.processTurn` on success.
- **MCP SSE fixes**: removed `http.Client{Timeout: 10s}` that broke long-lived SSE connections; added `Accept: application/json, text/event-stream` header; removed `httpReq.Close = true`; `readSSE` became a `Client` method with `SetOnProgress` callback for intermediate progress events; `scanner.Err()` check added; buffer increased to 1MB.
- **TUI sidebar improvements**: `/windows` toggle for right-side status panel (persisted). Status bar always shows `turn:N` and `tok:N/60k(X%)` (live compaction progress). Session ID prefixed `session:`, git hash shown as `git:abc1234`. Side panel now includes `reasoning` and `thinking` rows when configured per-model. Separator changed to dashed line `╌`.
- **LLM `thinking` + `reasoning_effort`**: per-model `thinking` (bool) and `reasoning_effort` (string high/max) in model config. OpenAI format: `{"reasoning_effort":"high","thinking":{"type":"enabled"}}`. Anthropic format: `{"output_config":{"effort":"high"},"thinking":{"type":"enabled","budget_tokens":4096}}`.
- **Human-readable limits**: `1k`, `2.5m`, `1b` suffixes in `limitValue` config and `/limit` command display, parsed by `parseHumanCount` in `config.GetInt`.
- **AgentLoop**: `State.ModelName` populated before LLM call. `SetDumpRecorder` + `SetSessionGcInterval` setter methods.

- Add session idle-expiry: when a new turn arrives more than `session.expire_after` (default `1h`, set to `0` to disable) after the active session's last activity, AgentIO asks the user via their transport whether to rotate to a fresh session. "Yes" creates a new session and binds the turn to it; "no" reuses the current session and refreshes its `UpdatedAt`. Background turns without a transport context silently continue the active session.
- `transport.IO` gains `Confirm(ctx, prompt) (bool, error)` — a simple yes/no helper used by the expiry prompt and any future confirmations. Interactive transports (TUI/stdio) delegate to `RequestPermission` (Once/Always → yes, Denied → no); non-interactive transports return `false` with an error. `Session` now persists `UpdatedAt`; `session.Manager` exposes `Touch(id)` for AgentIO to refresh it per turn. `lifecycle.Builder` now captures the first bootstrap error via a `boot()` helper and panics in `Build()` instead of silently dropping it (was 13 unchecked `Bootstrap()` calls); `Limiter.RecordLLM` routes counter increments through an `incr()` helper that logs store errors without breaking the LLM path; `MCP.Execute`/`StdioClient.Execute` return argument-parse errors; `skills enable/disable` and `skill load` propagate save errors to the user; `main` exits non-zero on cobra errors; HTTP response encoders, multipart writes, best-effort index/migration, and advisory deadline sets are marked with explicit `_, _ =` / `_ =` and rationale where the error is genuinely not actionable. tighten `os.WriteFile` permissions to `0o600` for user data/config/session/permission/skill/scheduler files (G306, 14 sites); add `ReadHeaderTimeout` to HTTP servers in a2a and prometheus to mitigate Slowloris (G112); fix off-by-one slice indexing in `i18n.Register` that could read past the end of an odd-length dicts slice (G602, real bug); validate SMTP recipients against CRLF injection at the `sendSMTP` entry (G707); suppress G204/G117/G118/G401/G501 false positives (shell tool, login credential field, WeWork md5 digest, detached panic-event publish) with `//nolint:gosec` + rationale. most are event subscriber switches that intentionally handle a subset of an open `event.Type` enum (new event types must not force updates to every subscriber); others have a `default` that legitimately absorbs the remaining cases (e.g. `PermissionDenied`, `WebhookHTTP`, non-terminal step states). Annotated each with `//nolint:exhaustive` and a one-line rationale rather than listing every case redundantly. `WriteString(fmt.Sprintf(...))`→`fmt.Fprintf` (QF1012), `if/else if` on status→tagged switch (QF1003), nil context→`context.TODO` (SA1012, with `//nolint` where nil is the behavior under test), drop empty `else` branch (SA9003), `//nolint` on `Lock/Unlock` pairs whose Lock side-effect registers a session entry (SA2001) `errPermissionDenied` sentinel, `limitStore` interface, `FileStore.save` method, `mcpServerConfig` struct, `_removed` test stub, unused `models`/`ctx`/`cancel`/`mu` fields
- Apply `gocritic` cleanups: `assignOp` (`prefix +=`), `unlambda` (pass `appctx.NewRegistry` directly), `sloppyTypeAssert` (drop redundant type assertions), `singleCaseSwitch` (type switch → if), `appendAssign` (don't alias slice headers across append); suppress `ifElseChain` false positives on compound-predicate branches with `//nolint:gocritic` `sh -c "sleep N"` left the `sleep` grandchild alive holding the stdout pipe open after `sh` was killed, so the reader goroutine blocked until the grandchild exited naturally (watchdog appeared not to fire). The shell handler now puts the child in its own process group (`Setpgid`) and kills the whole group on context cancellation, and closes the pipe write end via `context.AfterFunc` so the reader unblocks immediately even if a grandchild lingers. Fixes the CI `TestWatchdog_ShellToolFiresOnSilentStall` failure (elapsed was the full `sleep 2`, now ~50ms).
- Fix `TestPanda_HandleFrame_SendAck_FlushesPending` race: replace a fixed `time.Sleep(100ms)` waiting for the async ack flush with a poll-until-flushed loop (3s deadline), so the assertion no longer races the readLoop on slow CI runners under `-race`.
- Loosen panda timeline frame-wait timeouts 1s→3s throughout panda_test.go for CI runner headroom under `-race`.
- Fix CI lint (`golangci-lint run ./... --new`): `skill.NewFileStore` now returns `(*FileStore, error)` instead of silently swallowing `os.MkdirAll` failures (mirrors `limit.NewFileStore`); `ToolsBootstrapper` propagates the error, test callers fail loudly via a `mustNewStore` helper
- Tighten `.golangci.yml`: add `gci` formatter (import grouping std / 3rd-party / `dolphin` prefix), exclude `errcheck`/`gosec`/`noctx` in test files, declare default presets explicitly (v2 disables them when an `exclusions` block is present), raise timeout to 5m
- Add high-value linters (`bodyclose`, `errorlint`, `noctx`, `unconvert`, `usestdlibvars`, `wastedassign`) and fix all their findings at net-zero issue count: close WebSocket dial response bodies; use `errors.Is` for sentinel comparisons; thread a real `context.Context` through the model-discovery chain (`Bootstrap` → `createProvider` → `discoverProviderModels` → `DiscoverModels` → `DiscoverAnthropic/OpenAIModels` → deepseek); use `ListenConfig`/`tls.Dialer.DialContext` for a2a/email; bound the Prometheus remote-write push with a 10s timeout context
- Makefile gates `-race` behind `RACE=1` / `make build-race`; the default `make build` no longer links ThreadSanitizer, cutting idle CPU from ~8% to ~0
- Fix TUI side-panel overflow: `renderSideStatus` passed inner dimensions to lipgloss `Width`/`Height` (content area), but the border adds 2 rows/cols outside it, so the panel overshot by 2 rows and scrolled the top of the view off-screen (hiding the Status header and the top of the message viewport, including the user's own echo)
- Add context compaction: when the estimated token count exceeds `compaction.max_tokens`, the oldest messages are summarized into a single `IsSummary` message kept at the head of Messages, while the most recent `compaction.keep_rounds` rounds are preserved verbatim; the compacted history is persisted via a new `Memory.Replace` so subsequent turns (and restarts) use the trimmed context without re-summarizing
- `Memory` interface gains `Replace`; `types.Message` gains `IsSummary`; new `EventCompaction` event; `compaction.enabled` switches the feature off
- Compaction summarizer switch marked `//nolint:exhaustive` (system messages never flow through Messages); drop an unused test helper
- Agent loop: catch panics raised by compositor stages — convert to a TurnResult with Done=true and re-panic so runWorker's recovery still drives backoff and publishes EventWorkerPanic; the turn is no longer silently dropped on a stage panic
- TUI: permission dialog colors switched from hardcoded literals to adaptive theme colors (light/dark terminal support)

- /context list uses 1-based numbered format (1) base, 2) soul, ...)
- Add /session stop and /session continue to pause and resume turn processing
- Add test coverage for pause/resume paths in LLMStage, ToolStage serial, and processParallel
- Add tea.WithInputTTY() and error logging to TUI transport for diagnostics
- Add speedtest workflow
- Add idle-timeout watchdog (llm_idle_timeout) with throttled per-chunk/per-line feed (feed_min_interval) so stalled LLM/tool calls fail fast instead of hanging the turn
- Add checkpoint recovery: on recoverable failures the unflushed memory tail is written with IsPartial markers, enabling future /continue resume
- Add internal/progress package: nil-safe context-attached feed point that breaks the agentloop<->tool import cycle
- Shell tool now streams stdout line-by-line and feeds the watchdog per line, so long builds/commands aren't misjudged as stalls
- `config init` now also writes config.schema.json (embedded in binary) alongside config.yaml; default config.yaml references it via $schema for editor validation
- TUI gains a right-hand side status panel (~20% width, min 16 cols) showing model, temperature, pool size, workmode, turn/req/tok/tools usage with k/m/b/t suffixes; long values truncate with ellipsis instead of wrapping
- Side panel uses dashed open-bottom border (left/right `┊` extend to the queue separator); viewport and side panel resize dynamically with textarea/queue/statusbar heights so borders stay aligned
- Narrow terminals fall back to a full bottom status bar; wide terminals keep a compact bottom bar (identity + model + /exit) alongside the side panel
- gofmt fix for side panel border struct alignment
- TUI: bottom status bar shows a spinner + elapsed time while a turn is pending, so there is live feedback even with tool/thinking output hidden
- TUI: scrolling away from the bottom shows a scroll-position indicator (`↑ 42%`) and `Ctrl+G` jumps back to the latest output
- TUI: tool errors (`❌`) now always render even when tool-call display is off, using the previously-unused error color; failed tool calls are no longer silently invisible
- TUI: queue shows `+N queued`/`+N done`/`+N more active` indicators instead of silently dropping items; pending list shows the head (next to run) rather than the tail
- TUI: side panel renames the tool-call-count row to `calls` so it no longer collides with the `tools` on/off toggle
- TUI: permission dialog is now a true modal — captures all keys (no stray textarea typing), supports `y/Y a/A n/N`, arrow-key + Enter navigation, and is centered on screen
- TUI: fix permission dialog being unresolvable — the response channel is now delivered with the prompt message instead of a stale nil copy
- Agent loop: feeds the idle watchdog while a permission prompt waits for user input, so a slow reader no longer trips `llm_idle_timeout` and cancels the turn mid-prompt
- TUI: drop the redundant `/exit` hint from the wide-mode bottom status bar
- Fix cross-test data race: `requestPermissionFeeding`'s detached goroutine read the test-mutable global `permFeedInterval` directly, racing with the next test's write to it under `-race`; the value is now read once on the caller's synchronous path and captured in the closure so no detached goroutine touches the global

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

  (format fix)
