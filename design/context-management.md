# Context Management Design

## Overview

Dolphin's context manage has two layers: **system prompt construction** (static, layered) and **runtime compression** (dynamic, strategy-pluggable). Together they keep the LLM context within token limits while preserving key information.

## System Prompt Construction

### Layered Context Files

The system prompt is built from context files with cascading fallback: **agent dir > project dir (.dolphin/) > user dir (~/.dolphin/) > system dir (/etc/dolphin/)**.

| Priority | Directory | Purpose |
|----------|-----------|---------|
| 1 | `.dolphin/agents/<name>/` | Agent-specific overrides |
| 2 | `.dolphin/` | Per-project settings |
| 3 | `~/.dolphin/` | User-wide settings |
| 4 | `/etc/dolphin/` | System-wide defaults |

### Assembly Order

```
1. PREFACE (embedded)       — agent identity, capabilities, safety rules
2. BUILTIN SKILLS (embedded) — always-available instructions
3. AGENTS.md                — multi-agent definitions and routing
4. RULES.md                 — behavioral constraints (project coding standards, etc.)
5. USER.md                  — user profile, preferences, background
6. SYSTEM.md                — auto-generated once, injected every session (OS, tools, paths)
```

Files are stat-cached: re-read only when mtime changes. Missing files are silently skipped.

### Transport Context

Each transport appends its own context line (e.g. email latency warnings, SSH session metadata) at Agent start time. This is injected after the static system prompt.

---

## Runtime Compression

### Problem

LLM context windows are finite. Long conversations accumulate messages until the provider rejects the request. Compression must:
- Drop old messages without losing critical information
- Be transparent to the user (no interruption)
- Handle CJK content correctly in token estimation

### Compression Interface

```go
type Compressor interface {
    Compress(messages []Message, maxTokens int) ([]Message, *CompressReport)
}
```

Returns `(nil, nil)` when no compression is needed.

### Shared Preamble

All compressors share `compressPreamble`:
1. Estimate total tokens (CJK-aware: ~1 token per CJK char, bytes/3.5 for ASCII)
2. If under 70% of `maxTokens` → skip
3. If ≤6 messages → skip (too small to compress)
4. Find `keepStart` — the last user message + everything after it
5. If no messages before `keepStart` → skip

### Strategies

#### 0. DropCompressor (default)
- Simplest: drops complete user+assistant turn groups from the front
- No summarization, no LLM calls
- Fast, zero cost, no latency
- Best for: short sessions, interactive use

#### A. SegmentCompressor
- Each compression creates an L1 segment from dropped raw messages
- When any level exceeds `SegmentMergeLimit` (default 100), segments at that level merge into L+1
- Segments are marked: `[L1 摘要, 覆盖 N 组] content`
- **Current limitation**: summarization uses concatenation, not LLM. When LLM integration is added, `summarizeRawMessages` will call the provider.
- Best for: very long sessions with predictable growth

#### B. TieredCompressor
- Three-tier cache: L2 (far history) → L1 (mid) → raw (recent N pairs)
- Keeps last 3 user+assistant pairs as raw text
- Uses LLM to generate summaries, with concatenation fallback on failure
- Splits old messages in half: oldest → L2, middle → L1
- Best for: sessions that need detailed recent context but can summarize old history

#### C. IncrementalCompressor
- Single running summary, incrementally updated every N turns (default 5)
- New messages are merged into the existing summary via LLM call
- Thread-safe (mutex-protected state)
- Drift risk: if LLM summaries are low quality, errors compound over time
- Best for: sessions where maintaining a coherent running narrative matters

#### D. TopicCompressor
- Partitions messages by topic boundaries using heuristics (user message length >2x average → new topic)
- Each completed topic group is independently summarized
- Topic metadata preserved: `[L1 摘要, topic N, 覆盖 M 组]`
- Best for: sessions that naturally switch between distinct topics

### Strategy Selection

Configured via `llm.compress_mode` in config.yaml:

| Value | Strategy |
|-------|----------|
| `drop` (default) | DropCompressor |
| `segment` | SegmentCompressor |
| `tiered` | TieredCompressor |
| `incremental` | IncrementalCompressor |
| `topic` | TopicCompressor |

---

## Message Structure

Messages use JSON blocks for multi-modal content:

```json
[
  {"type": "thinking", "thinking": "...", "signature": "..."},
  {"type": "text", "text": "..."},
  {"type": "tool_use", "id": "...", "name": "...", "input": {...}},
  {"type": "tool_result", "tool_use_id": "...", "content": [...]}
]
```

This block-based format allows:
- Thinking blocks to be separated from visible text
- Tool calls and results to be structured
- Compressors to extract text via `extractText()` for summarization

---

## Token Estimation

Custom `estimateTokens()` accounts for mixed ASCII+CJK content:
- CJK characters (U+2E80–9FFF, U+F900–FAFF, U+FE30–FE4F): ~1 token each
- Non-CJK: bytes / 3.5
- Assistant messages: +20 token overhead

This is more accurate than `len(bytes)/4` for Chinese-language conversations.

---

## Compression Flow

```
Run() → runTurn()
  → compressHistory()
    → compressor.Compress(messages, MaxContextTokens)
      → compressPreamble()     // shared: estimate, threshold, keepStart
      → strategy-specific logic // drop / segment / tiered / incremental / topic
      → return (compressed, report)
    → update state.Messages
    → log compression (session diary)
    → emit Compression event
  → continue with compressed messages
```

Compression happens before each LLM call (`runTurn`), triggered when estimated tokens exceed 70% of `MaxContextTokens`.

---

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Interface over config flags | New compressors can be added without changing call sites |
| Shared preamble | DRY — every compressor needs the same threshold/keepStart logic |
| 70% threshold | Compression takes one turn to run; triggering too late risks rejection |
| Min 6 messages | Compressing tiny histories wastes tokens on summary overhead |
| CJK-aware estimation | Primary user base is Chinese-language; generic estimators undershoot |
| Stat-cached context files | Context files are read once per session, hot-reloaded on mtime change |
| LLM fallback to concatenation | Compression must never fail — if provider is down, degrade gracefully |
