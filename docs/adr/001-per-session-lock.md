# ADR 001: Per-Session Lock

**Date**: 2026-06-14
**Status**: accepted

## Decision

AgentLoop uses a per-session `sync.Mutex` to serialize turns within the same session, rather than a per-turn model where every turn gets its own isolated context.

## Rationale

Workflow steps and multi-turn conversations mutate shared state (history, tool results). Allowing concurrent turns within a session would require transactional memory or copy-on-write semantics. A per-session lock is simpler, avoids data races, and preserves FIFO ordering — turns complete in the order they arrived.

The trade-off is that a slow turn blocks subsequent turns for the same session. This is acceptable because session affinity is the common case: users expect sequential processing within a conversation.

## Alternatives considered

- **Per-turn isolation**: Each turn clones the session state. Rejected — adds clone overhead and merge conflicts for every turn.
- **Channel-based serialization**: Single goroutine per session. Rejected — harder to integrate with worker pool and context cancellation.
