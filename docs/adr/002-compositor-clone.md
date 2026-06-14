# ADR 002: Compositor Clone per Worker

**Date**: 2026-06-14
**Status**: accepted

## Decision

Each AgentLoop worker gets its own `Compositor` clone (`c.Clone()`) rather than sharing a single instance.

## Rationale

Stages hold mutable per-turn state (e.g., `MemoryWriteStage.writeIdx`, `ContextBuilderStage.transportCtx`). Sharing a single compositor across workers would create data races on these fields. Cloning gives each worker an isolated copy of the stage pipeline.

The `Clone()` method performs a shallow copy: shared resources (providers, registries, event bus) are copied by pointer — these are concurrency-safe by design. Per-turn counters and context fields are reset to zero values, which is intentional.

## Alternatives considered

- **Mutex per stage**: Wrap every stage method in a lock. Rejected — defeats the purpose of a worker pool; workers would serialize on stage access.
- **Factory pattern**: Create fresh stages per turn. Rejected — needlessly allocates new providers and registries on every turn.
