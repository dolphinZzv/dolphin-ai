# ADR 003: Workflow Steps Bypass Session Memory

**Date**: 2026-06-14
**Status**: accepted

## Decision

Workflow Engine steps call the LLM directly via `Provider.CompleteStream()` rather than going through the AgentLoop Compositor pipeline.

## Rationale

Workflow steps are task-specific, single-shot LLM calls. They don't need session history, context building, memory persistence, or permission checks — those are conversational concerns. Running steps through the full pipeline would inject irrelevant system prompts, accumulate history across steps, and add latency from stages that provide no value.

The step executor has its own bounded tool loop (max 5 rounds) for tool-augmented steps, which is sufficient for task execution.

## Alternatives considered

- **Route through AgentLoop**: Rejected — couples workflow to conversational infrastructure, bloats session storage with intermediate step data.
- **Separate LLM client**: Create a second provider instance. Rejected — the workflow engine already holds a reference to the same provider; a second instance adds config complexity for no benefit.
