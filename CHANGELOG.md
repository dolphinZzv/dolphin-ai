# Changelog

All notable changes to Dolphin will be documented in this file.

## [Unreleased]

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
