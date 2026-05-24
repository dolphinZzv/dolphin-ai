---
title: 上下文压缩
description: 使用上下文压缩策略管理长对话
weight: 25
---

Dolphin 通过压缩长对话来自动管理上下文窗口。这确保 LLM 不会因 token 限制而拒绝请求。

## 工作原理

当对话接近 `max_context_tokens` 的 70% 时，Dolphin 使用以下策略之一压缩消息历史。

所有策略共享**通用前导逻辑**：
1. 估算总 token 数（中文感知：CJK 字符约 1 token/字，ASCII 字节/3.5）
2. 低于 70% 阈值 → 跳过压缩
3. 消息数 ≤6 → 跳过（太小不值得压缩）
4. 找到 `keepStart` — 最后一个用户消息及其后所有内容
5. 如果 `keepStart` 前没有消息 → 跳过

## 压缩策略

### drop（默认）

最简单的策略：从前面删除完整的用户+助手轮次组。

- 无摘要，无 LLM 调用
- 快速，零成本，零延迟
- 适用场景：短会话、交互式使用

```mermaid
graph LR
    A["消息<br/>[U1 A1 U2 A2 U3 A3 U4 A4 U5 A5]"] --> B["compressPreamble<br/>估算 token → 70% 阈值?"]
    B -->|"是"| C["查找 keepStart<br/>最后一个用户消息"]
    C --> D["从前面删除轮次组<br/>直到低于阈值"]
    D --> E["结果<br/>[U4 A4 U5 A5]"]
    B -->|"否"| F["跳过<br/>return nil, nil"]
    E --> G["CompressReport<br/>dropped=N tokensSaved=M"]
    style A fill:#e1f5fe
    style E fill:#c8e6c9
    style F fill:#ffebee
```

### segment

创建多级金字塔摘要。每次压缩轮次从丢弃的消息生成 L1 片段。当任何级别的片段数超过 `segment_merge_limit`（默认 100）时，片段合并到下一级别。

- 使用拼接方式生成摘要（计划集成 LLM）
- 适用场景：非常长的会话，可预测的增长

```mermaid
graph TB
    subgraph Compression["单次压缩轮次"]
        A["原始消息<br/>[U1 A1 ... U10 A10]"] --> B["keepStart = 最后一个用户消息索引"]
        B --> C["丢弃的原始消息<br/>[U1 A1 ... U5 A5]"]
        B --> D["保留<br/>[U10 A10]"]
        C --> E["summarizeRawMessages()<br/>拼接方式（无 LLM）"]
        E --> F["[L1 摘要, 覆盖 10 组]<br/>segment_1"]
    end

    subgraph Merge["级别合并（当数量 > MergeLimit）"]
        G["L1 片段<br/>[seg_1, seg_2, ... seg_100]"]
        G -->|">100"| H["第1级合并"]
        H --> I["[L2 摘要, 覆盖 200 组]<br/>merged_segment"]
    end

    subgraph Pyramid["多级金字塔"]
        J["L3+（很旧）"] --- K["L2（远历史）"]
        K --- L["L1（中历史）"]
        L --- M["原始（最近）"]
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

三层缓存结构：L2（远历史）→ L1（中历史）→ 原始（最近 N 对）。

- 保留最后 3 对用户+助手对话为原始文本
- 使用 LLM 为 L2 和 L1 生成摘要
- 适用场景：需要详细近期上下文但可以摘要旧历史的会话

```mermaid
graph TB
    subgraph Input["keepStart 前的消息"]
        M["[最旧] ... [中间] ... [最近 keepStart]"]
    end

    subgraph Tiered["三层结构"]
        L2["[L2 摘要]<br/>远历史（LLM）"]
        L1["[L1 摘要]<br/>中历史（LLM）"]
        RAW["[原始对话]<br/>最近3对（文本）"]
    end

    Input -->|拆分| Tiered

    subgraph SplitLogic["TieredCompressor.Split"]
        S1["从 keepStart 往前计数原始对话对"]
        S1 --> S2{"rawPairs < rawKeep?"}
        S2 -->|yes| S3["rawStart = 此用户消息"]
        S2 -->|no| S4["继续往前"]
        S3 --> S5["midStart = rawStart + 一半"]
        S4 --> S5
    end

    M --> SplitLogic

    SplitLogic -->|最旧 → L2| L2
    SplitLogic -->|中间 → L1| L1
    SplitLogic -->|最近3对| RAW

    L2 --- L1 --- RAW

    style L2 fill:#ffcdd2
    style L1 fill:#fff9c4
    style RAW fill:#c8e6c9
```

### incremental

单一运行摘要，每 N 轮（默认 5）增量更新一次。

- 新消息通过 LLM 调用合并到现有摘要中
- 线程安全（互斥锁保护状态）
- 漂移风险：低质量摘要会随时间累积
- 适用场景：需要维护连贯运行叙事的会话

```mermaid
sequenceDiagram
    participant M as 消息
    participant IC as IncrementalCompressor
    participant LLM as LLM 提供商

    Note over IC: 状态: runningSummary, turnsSinceUpdate, coveredCount

    M->>IC: Compress(messages, maxTokens)

    IC->>IC: compressPreamble() → keepStart

    rect rgb(230, 245, 230)
        Note over IC: turnsSinceUpdate++

        loop 统计 keepStart 前的用户轮次
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

    IC->>IC: 构建结果: runningSummary + messages[keepStart:]

    IC-->>M: result, CompressReport

    Note over IC: 互斥锁围绕状态变化
```

```mermaid
graph LR
    subgraph State["IncrementalCompressor 状态"]
        S["runningSummary: ''"]
        T["turnsSinceUpdate: 0"]
        C["coveredCount: 0"]
    end

    subgraph Timeline["多轮演进"]
        R1["第1-4轮:<br/>累积轮次"]
        R5["第5轮:<br/>通过 LLM 合并"]
        R6["第6-9轮:<br/>累积"]
        R10["第10轮:<br/>再次合并"]
    end

    R1 -->|"turnsSinceUpdate = 4"| R5
    R5 -->|"重置为 0"| R6
    R6 -->|"turnsSinceUpdate = 4"| R10

    S -->|"'' → '第1-5轮摘要'"| T
    T -->|"'第1-5轮摘要' → '第1-10轮摘要'"| C

    style R1 fill:#e1f5fe
    style R5 fill:#c8e6c9
    style R10 fill:#c8e6c9
```

### topic

按主题边界对消息进行分区（用户消息长度 > 2x 平均值 → 新主题）。

- 每个已完成的主题组独立摘要
- 保留主题元数据：`[L1 摘要, topic N, 覆盖 M 组]`
- 适用场景：自然地在不同主题之间切换的会话

```mermaid
graph TB
    subgraph Partition["partitionTopics() — 启发式检测"]
        P1["收集用户消息长度"]
        P1 --> P2["计算平均长度"]
        P2 --> P3{"用户消息长度 > 2x 平均?"}
        P3 -->|yes| P4["新主题边界"]
        P3 -->|no| P5["继续当前主题"]
        P4 --> P5
        P5 --> P3
    end

    subgraph Groups["主题组"]
        G1["[主题 1]<br/>U1 A1 U2 A2"]
        G2["[主题 2]<br/>U3 A3"]
        G3["[主题 3 - 当前]<br/>U4 A4 U5 A5"]
    end

    Partition --> Groups

    G1 -->|"summarizeTopic()"| S1["[L1 摘要, topic 1, 覆盖 2 组]"]
    G2 -->|"summarizeTopic()"| S2["[L1 摘要, topic 2, 覆盖 1 组]"]
    G3 -->|"保留原始-ish<br/>(当前主题)"| R3["[原始]<br/>U4 A4 U5 A5"]

    style G1 fill:#e1f5fe
    style G2 fill:#b3e5fc
    style G3 fill:#c8e6c9
    style S1 fill:#fff9c4
    style S2 fill:#fff9c4
    style R3 fill:#c8e6c9
```

```mermaid
sequenceDiagram
    participant M as 消息
    participant TC as TopicCompressor
    participant LLM as LLM 提供商

    M->>TC: Compress(messages, maxTokens)
    TC->>TC: compressPreamble() → keepStart

    TC->>TC: partitionTopics(messages[:keepStart])

    rect rgb(255, 243, 224)
        Note over TC: 检测到多个主题组

        loop 对每个已完成的主题
            TC->>LLM: summarizeTopic(group.messages)
            LLM-->>TC: summary
            TC->>TC: 格式化: [L1 摘要, topic N, 覆盖 M 组] summary
        end
    end

    Note over TC: 当前主题（最后一组）保留原始-ish

    TC-->>M: 结果: [摘要...] + [当前原始...] + [keepStart...]
```

## 配置

通过 `config.yaml` 中的 `llm.compress_mode` 配置：

| 值 | 策略 |
|------|------|
| `drop`（默认） | DropCompressor — 最简单，无 LLM |
| `segment` | SegmentCompressor — 多级金字塔 |
| `tiered` | TieredCompressor — 三层缓存 |
| `incremental` | IncrementalCompressor — 运行摘要 |
| `topic` | TopicCompressor — 主题感知分割 |

对于 `segment` 模式，还可配置：
- `llm.segment_merge_limit` — 合并前的片段数量阈值（默认：100）
- `llm.compress_timeout_seconds` — LLM 调用超时（默认：15秒）

## 压缩报告

每次压缩返回 `CompressReport`：

| 字段 | 说明 |
|------|------|
| `dropped_count` | 删除的消息数 |
| `tokens_saved` | 估算释放的 token 数 |
| `new_level` | 生成的摘要级别（0 = 纯删除） |

查看压缩事件日志：
```
zap.Info("compression", zap.Int("dropped", r.DroppedCount), ...)
```