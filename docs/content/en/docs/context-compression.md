---
title: Context Compression
description: Manage long conversations with context compression strategies
weight: 25
---

Dolphin automatically manages context windows by compressing long conversations. This ensures the LLM never rejects requests due to token limits.

## How It Works

When your conversation approaches 70% of `max_context_tokens`, Dolphin compresses the message history using one of the strategies below.

All strategies share a **common preamble**:
1. Estimate total tokens (CJK-aware: ~1 token per CJK char, bytes/3.5 for ASCII)
2. If under 70% threshold → skip compression
3. If ≤6 messages → skip (too small to compress)
4. Find `keepStart` — the last user message + everything after it
5. If no messages before `keepStart` → skip

## Compression Strategies

### drop (Default)

The simplest strategy: drops complete user+assistant turn groups from the front.

- No summarization, no LLM calls
- Fast, zero cost, no latency
- Best for: short sessions, interactive use

```mermaid
graph LR
    A["Messages<br/>[U1 A1 U2 A2 U3 A3 U4 A4 U5 A5]"] --> B["compressPreamble<br/>estimateTokens → 70% threshold?"]
    B -->|"Yes"| C["Find keepStart<br/>last user message"]
    C --> D["Drop turn groups<br/>from front until under threshold"]
    D --> E["Result<br/>[U4 A4 U5 A5]"]
    B -->|"No"| F["Skip<br/>return nil, nil"]
    E --> G["CompressReport<br/>dropped=N tokensSaved=M"]
    style A fill:#e1f5fe
    style E fill:#c8e6c9
    style F fill:#ffebee
```

### segment

Creates a multi-level pyramid of summaries. Each compression round generates an L1 segment from dropped messages. When any level exceeds `segment_merge_limit` (default 100), segments merge into the next level.

- Uses concatenation for summaries (LLM integration planned)
- Best for: very long sessions with predictable growth

```mermaid
graph TB
    subgraph Compression["Single Compression Round"]
        A["Raw Messages<br/>[U1 A1 ... U10 A10]"] --> B["keepStart = last user message index"]
        B --> C["Dropped Raw<br/>[U1 A1 ... U5 A5]"]
        B --> D["Kept<br/>[U10 A10]"]
        C --> E["summarizeRawMessages()<br/>concatenation (no LLM)"]
        E --> F["[L1 摘要, 覆盖 10 组]<br/>segment_1"]
    end

    subgraph Merge["Level Merge (when count > MergeLimit)"]
        G["L1 Segments<br/>[seg_1, seg_2, ... seg_100]"]
        G -->|">100"| H["Merge at level 1"]
        H --> I["[L2 摘要, 覆盖 200 组]<br/>merged_segment"]
    end

    subgraph Pyramid["Multi-Level Pyramid"]
        J["L3+ (very old)"] --- K["L2 (far history)"]
        K --- L["L1 (mid history)"]
        L --- M["Raw (recent)"]
        style J fill:#ffcdd2
        style K fill:#fff9c4
        style L fill:#e1f5fe
        style M fill:#c8e6c9
    end

    F --> L
    I --> K
    D --> M

    style G fill:#e1f5fe
    style H fill:#fff9c4
    style I fill:#ffcdd2
```

### tiered

Three-tier cache structure: L2 (far history) → L1 (mid history) → raw (recent N pairs).

- Keeps last 3 user+assistant pairs as raw text
- Uses LLM to generate summaries for L2 and L1
- Best for: sessions needing detailed recent context with summarized old history

```mermaid
graph TB
    subgraph Input["Messages before keepStart"]
        M["[oldest] ... [mid] ... [recent keepStart]"]
    end

    subgraph Tiered["Three-Tier Structure"]
        L2["[L2 摘要]<br/>far history (LLM)"]
        L1["[L1 摘要]<br/>mid history (LLM)"]
        RAW["[raw pairs]<br/>recent 3 pairs (text)"]
    end

    Input -->|Split| Tiered

    subgraph SplitLogic["TieredCompressor.Split"]
        S1["Count raw pairs from keepStart"]
        S1 --> S2{"rawPairs < rawKeep?"}
        S2 -->|yes| S3["rawStart = this user msg"]
        S2 -->|no| S4["Continue walking back"]
        S3 --> S5["midStart = rawStart + half"]
        S4 --> S5
    end

    M --> SplitLogic

    SplitLogic -->|oldest → L2| L2
    SplitLogic -->|mid → L1| L1
    SplitLogic -->|recent 3 pairs| RAW

    L2 --- L1 --- RAW

    style L2 fill:#ffcdd2
    style L1 fill:#fff9c4
    style RAW fill:#c8e6c9
```

### incremental

Single running summary, incrementally updated every N turns (default 5).

- New messages merge into the existing summary via LLM call
- Thread-safe (mutex-protected state)
- Drift risk: low-quality summaries compound over time
- Best for: sessions where maintaining a coherent running narrative matters

```mermaid
sequenceDiagram
    participant M as Messages
    participant IC as IncrementalCompressor
    participant LLM as LLM Provider

    Note over IC: State: runningSummary, turnsSinceUpdate, coveredCount

    M->>IC: Compress(messages, maxTokens)

    IC->>IC: compressPreamble() → keepStart

    rect rgb(230, 245, 230)
        Note over IC: turnsSinceUpdate++

        loop count user turns before keepStart
            IC->>IC: newTurns++
        end

        alt turnsSinceUpdate >= updateInterval (5) AND provider != nil
            IC->>LLM: mergeIntoSummary(messages[:keepStart], newTurns)
            LLM-->>IC: newSummary
            IC->>IC: runningSummary = newSummary
            IC->>IC: coveredCount += covered
            IC->>IC: turnsSinceUpdate = 0
        end
    end

    IC->>IC: Build result: runningSummary + messages[keepStart:]

    IC-->>M: result, CompressReport

    Note over IC: Mutex lock/unlock around state mutations
```

```mermaid
graph LR
    subgraph State["IncrementalCompressor State"]
        S["runningSummary: ''"]
        T["turnsSinceUpdate: 0"]
        C["coveredCount: 0"]
    end

    subgraph Timeline["Multi-Round Evolution"]
        R1["Round 1-4:<br/>accumulate turns"]
        R5["Round 5:<br/>merge via LLM"]
        R6["Round 6-9:<br/>accumulate"]
        R10["Round 10:<br/>merge again"]
    end

    R1 -->|"turnsSinceUpdate = 4"| R5
    R5 -->|"reset to 0"| R6
    R6 -->|"turnsSinceUpdate = 4"| R10

    S -->|"'' → 'summary of rounds 1-5'"| T
    T -->|"'summary of rounds 1-5' → 'summary of rounds 1-10'"| C

    style R1 fill:#e1f5fe
    style R5 fill:#c8e6c9
    style R10 fill:#c8e6c9
```

### topic

Partitions messages by topic boundaries (user message length >2x average → new topic).

- Each completed topic group is independently summarized
- Topic metadata preserved: `[L1 摘要, topic N, 覆盖 M 组]`
- Best for: sessions that naturally switch between distinct topics

```mermaid
graph TB
    subgraph Partition["partitionTopics() — Heuristic Detection"]
        P1["Collect user message lengths"]
        P1 --> P2["Compute average length"]
        P2 --> P3{"User msg length > 2x avg?"}
        P3 -->|yes| P4["New topic boundary"]
        P3 -->|no| P5["Continue current topic"]
        P4 --> P5
        P5 --> P3
    end

    subgraph Groups["Topic Groups"]
        G1["[Topic 1]<br/>U1 A1 U2 A2"]
        G2["[Topic 2]<br/>U3 A3"]
        G3["[Topic 3 - Current]<br/>U4 A4 U5 A5"]
    end

    Partition --> Groups

    G1 -->|"summarizeTopic()"| S1["[L1 摘要, topic 1, 覆盖 2 组]"]
    G2 -->|"summarizeTopic()"| S2["[L1 摘要, topic 2, 覆盖 1 组]"]
    G3 -->|"kept raw-ish<br/>(current topic)"| R3["[raw]<br/>U4 A4 U5 A5"]

    style G1 fill:#e1f5fe
    style G2 fill:#b3e5fc
    style G3 fill:#c8e6c9
    style S1 fill:#fff9c4
    style S2 fill:#fff9c4
    style R3 fill:#c8e6c9
```

```mermaid
sequenceDiagram
    participant M as Messages
    participant TC as TopicCompressor
    participant LLM as LLM Provider

    M->>TC: Compress(messages, maxTokens)
    TC->>TC: compressPreamble() → keepStart

    TC->>TC: partitionTopics(messages[:keepStart])

    rect rgb(255, 243, 224)
        Note over TC: Multiple topic groups detected

        loop For each completed topic
            TC->>LLM: summarizeTopic(group.messages)
            LLM-->>TC: summary
            TC->>TC: Format: [L1 摘要, topic N, 覆盖 M 组] summary
        end
    end

    Note over TC: Current topic (last group) kept raw-ish

    TC-->>M: result: [summaries...] + [current raw...] + [keepStart...]
```

## Configuration

Configure via `llm.compress_mode` in config.yaml:

| Value | Strategy |
|-------|----------|
| `drop` (default) | DropCompressor — simplest, no LLM |
| `segment` | SegmentCompressor — multi-level pyramid |
| `tiered` | TieredCompressor — three-tier cache |
| `incremental` | IncrementalCompressor — running summary |
| `topic` | TopicCompressor — topic-aware segmentation |

For `segment` mode, also configure:
- `llm.segment_merge_limit` — segment count before merging (default: 100)
- `llm.compress_timeout_seconds` — LLM call timeout (default: 15s)

## Compression Report

Each compression returns a `CompressReport`:

| Field | Description |
|-------|-------------|
| `dropped_count` | Number of messages dropped |
| `tokens_saved` | Estimated tokens freed |
| `new_level` | Summary level generated (0 = pure drop) |

Check logs for compression events:
```
zap.Info("compression", zap.Int("dropped", r.DroppedCount), ...)
```