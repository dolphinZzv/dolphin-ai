# Dream — 离线 self-edit 系统

## 概述

Dream 在用户停止输入一段时间后，通过 git 分支编辑 brain 已有内容。brain 越 dream 越精。

**三个核心决策：**

1. **宁可不动，不要乱动。** 编辑门槛高——只有证据充分且修复方向明确才动。
2. **自动化是默认。** 用户审核是安全阀，不是必经流程。
3. **影响力决定投入。** 高频引用文件的纠正 > 冷门文件的合并，token 预算按影响力分配。

---

## 架构

```
用户空闲 20m (/exit 后加速到 2m)
    │
    ▼
Phase 0: GATE  — 新 session 够吗？有新材料吗？连续空跑几次了？
    │ 通过
    ▼
Phase 1: SCAN  — 纯规则。session 信号 × brain 现状 × effectivenes 数据
    │            → 加权 EditProposal 列表（合并同文件冲突，排序）
    ▼
Phase 2: EDIT  — 单次 LLM 批量调用。所有 proposal 一次 prompt 处理
    │            输出后过因果验证 + 质量约束
    ▼
Phase 3: APPLY — git clone brain → /tmp 临时工作区 → dream/N 逐 commit
    │            → git fetch 回 brain → auto_apply? ff-merge / 留分支
    │            → rm -rf 临时工作区
    ▼
Phase 4: TIDY  — 更新 state。自校准阈值。
```

---

## 运行时状态

**不在 brain git 里。** state 是操作元数据，存在 `.dolphin/dream.json`（gitignored）。

```json
{
  "last_dream_at": "2026-06-24T11:00:00Z",
  "last_dream_id": 12,
  "consecutive_empty": 0,
  "open_branch": null,
  "totals": {
    "files_improved": 8,
    "files_deprecated": 3,
    "files_merged": 2,
    "files_created": 1
  },
  "usage": {
    "files": {"commands/deploy.md": {"refs": 30, "last": "2026-06-24T10:00:00Z"}}
  },
  "file_cooldowns": {               // 同文件编辑冷却
    "commands/deploy.md": {"last_edited_dream": 12, "cooldown_until_dream": 17}
  },
  "calibration": {
    "thresholds": {"correction": 0.85, "preference": 0.95, "refinement": 0.65, "repetition": 0.80},
    "window": [{"dream_id": 10, "adopted": 3, "total": 4}, {"dream_id": 11, "adopted": 1, "total": 2}]
  },
  "last_applied_edits": [],
  "edit_feedback": {}
}
```

**引用追踪：** `brain.Read` 被 agent loop 调用来读取任意 brain 文件时，发出 `EventBrainFileReferenced`。Pipeline 的 metrics goroutine 消费此事件并更新 `usage.files`。

---

## 配置

```yaml
dream:
  enabled: true
  idle_minutes: 20
  exit_idle_minutes: 2            # /exit 后的快速窗口
  auto_apply: true
  min_sessions: 2
  min_user_messages: 8
  max_consecutive_empty: 3
  min_impact_threshold: 0.5       # Impact < 此值的 edit 不进 Phase 2 LLM
  file_cooldown_dreams: 5         # 同一文件在 N 次 dream 内不可重复编辑
  max_edits_per_dream: 10         # 单次 dream 最大编辑数
  reflect_model: ""
  max_reflect_tokens: 2048
  calibration_window: 10          # Phase 4 校准的滑动窗口大小
  calibration_min_step: 0.05      # 最小阈值调整步长
  calibration_confidence_floor: 0.3
  calibration_confidence_ceiling: 0.95
```

---

## Phase 0: GATE

```go
func (d *Dream) shouldRun(sessions []*session.Session) (bool, string) {
    // 1. 至少 2 个新 session
    if len(sessions) < d.minSessions {
        return false, "insufficient sessions"
    }

    // 2. 至少 8 条用户消息
    if countUserMessages(sessions) < d.minUserMessages {
        return false, "insufficient user messages"
    }

    // 3. 连续空跑 ≥2 次 → 除非有 ≥5 个新 session
    if d.state.ConsecutiveEmpty >= 2 && len(sessions) < 5 {
        return false, "recent dreams produced no edits"
    }

    // 4. 距上次 dream < (idle_minutes × 3) 且有 ≥1 个 session 与上次重叠
    if d.sessionsOverlapWithLastDream(sessions) {
        return false, "too soon, overlapping sessions"
    }

    return true, ""
}
```

**`/exit` 加速：** 用户发送 `/exit` 时，timer 从 `idle_minutes` 缩到 `exit_idle_minutes`（默认 2m）。信号更强——用户说"我走了"。

---

## Phase 1: SCAN

### 1.1 信号分层（按可验证性）

Brain 下所有 `.md` 文件都是可编辑目标。不做类型限制。

| 层级 | 信号类型 | 置信度 |
|------|---------|--------|
| L1 | 显式偏好声明（"以后用 X 不要 Y"） | 0.95 |
| L1 | 文件 30 天未被引用 | 0.90 |
| L2 | Teachable moment（纠正后行为改变） | 0.85 |
| L2 | 重复模式 ≥5 次跨 session | 0.80 |
| L3 | 多文件内容重复 >70% | 0.70 |
| L3 | 精炼信号（"顺便也…""再加一个…"） | 0.65 |

### 1.2 Teachable moment 三层级联

```
Level 1 — 硬信号
  用户消息含 "以后|记住|别忘了|不对|应该是" + 同一轮 assistant tool_call 变化
  → confidence 0.85+

Level 2 — 行为验证
  对比纠正前后的 tool_call name + args。参数确实变了 → 确认。没变 → 丢弃。

Level 3 — 跨 session 强化
  同类型纠正 ≥2 个 session → confidence +0.05

反问否定（"你觉得这样做对吗？"）
  → 标记为 rhetorical_candidate。Phase 2 中只与 L1/L2 信号捆绑处理。
  → prompt 里的捆绑指令：
    "For any proposal marked rhetorical_candidate: only edit if a paired L1/L2
     proposal confirms the same corrective intent. Otherwise discard silently."
```

### 1.3 过滤 Compaction 回声

Phase 1 读取 session memory 时跳过 `IsSummary: true` 的消息。Dream 只分析原始对话，不分析 Compaction 的二次摘要。

### 1.4 影响力度量

不区分文件类型。所有 brain 文件用同一套度量。

```go
func (d *Dream) computeImpact(signal EditSignal, target BrainFile) float64 {
    w := 1.0

    // 使用频率：被引用的次数（来自 state.usage）
    if target.ReferencedCount > 10 {
        w *= 2.0
    } else if target.ReferencedCount > 0 {
        w *= 1.2
    }
    // 未被引用 → w 保持 1.0（不做惩罚——可能是新文件或冷门但正确的文件）

    // 信号类型修正
    switch signal.Type {
    case "correction":  w *= 1.5
    case "refinement":  w *= 0.8
    case "obsolescence": w *= 0.3
    }

    // 新鲜度：7 天半衰期（下界 0.1）
    days := time.Since(signal.FirstSeen).Hours() / 24
    w *= max(0.1, math.Pow(0.5, days/7))

    return w
}
// Impact 值域约 [0.03, 3.0]。上限由 max_edits_per_dream 截断。
```

**引用追踪：** `brain.Read` 被调用读取任意文件时发 `EventBrainFileReferenced`。与 command 使用追踪共用同一条事件管道，消费到 `dream.json` 的 `usage.files`。

### 1.5 同文件合并

Phase 1 输出前检测同一 Target 的多条 EditProposal：

```
for each target with >1 proposal:
    keep ← Impact 最高的那条
    merged_reason ← join all reasons
    丢弃其余
```

最终输出的 EditProposal 列表中**每个 Target 最多一条**。

### 1.6 输出

```go
type EditProposal struct {
    ID         string      // "p1", "p2", ...
    Target     string      // brain 文件路径
    Action     string      // improve|merge|deprecate|create|split
    Target     string      // brain 文件路径（如 commands/deploy.md、knowledge/k8s.md、rules/code-style.md）
    Before     string      // 当前内容
    Reason     string      // 为什么
    Evidence   []string    // session_id:msg_index
    Confidence float64     // 规则计算
    Impact     float64     // 影响力权重
    NeedsLLM   bool        // Impact ≥ min_impact_threshold
    IsRhetorical bool      // 反问否定信号，需 L1/L2 捆绑
}

// 排序：Impact 降序。截断到 max_edits_per_dream。
```

---

## Phase 2: EDIT

### 2.1 批量调用

**一次 LLM 调用处理所有提案。** 单次 prompt 内共享 few-shot 示例前缀（只出现一次），所有 proposal 串行输出。

| 提案数 | 策略 | input tokens | output tokens |
|--------|------|-------------|--------------|
| 1–3 | 全量 few-shot（每种 action 1 示例） | ~1500 | ~600 |
| 4–10 | L1/L2 的 2 示例，L3 走规则 | ~2500 | ~1500 |

**LLM 不处理 NeedsLLM=false 的提案。** 这些直接在 Phase 3 走纯规则（deprecate 标记、简单 merge）。

### 2.2 Few-shot 示例

```
## Example 1 — improve (correction-driven)
INPUT:  proposal p1, action improve, target commands/deploy.md
        before: "1. Run: docker compose up -d\n2. Verify: curl localhost:8080/health"
        reason: 用户纠正：用 kubectl 不是 docker compose
OUTPUT: after: "1. Apply: kubectl apply -f deploy.yaml\n2. Verify: kubectl rollout status deployment/app"
        reasoning: 将 docker compose→kubectl，curl→rollout status（更可靠）

## Example 2 — create (preference)
INPUT:  proposal p2, action create, target knowledge/go-pref.md
        reason: 用户说"以后都用 Go 不要 Python"，跨 3 个 session 验证
OUTPUT: after: "用户偏好 Go。后端/CLI 工具用 Go，不用 Python。已跨 session 验证。"
        reasoning: 显式偏好声明 + 跨 session 验证

## Example 3 — merge (dedup)
INPUT:  proposal p3, action merge, target rules/deploy.md
        reason: rules/deploy-k8s.md + rules/deploy-docker.md 内容重叠 75%
OUTPUT: after: "部署方式：k8s (kubectl apply) 是标准方案。Docker Compose 曾用但已废弃。"
        reasoning: 合并消除重复，保留最新信息

## Example 4 — deprecate
INPUT:  proposal p4, action deprecate, target scripts/old-script.md
        reason: 40 天未被引用
OUTPUT: after: null
        reasoning: null
```

### 2.3 Prompt 规则（发给 LLM 的指令清单）

```
1. Output ONLY valid JSON array of edit objects. No markdown wrapping.
2. Each edit: {"proposal_id":"...", "action":"...", "target":"...", "after":"...", "reasoning":"..."}
3. improve/merge/create: after must be non-empty. deprecate: after must be null.
4. Every non-deprecate edit MUST have a non-empty reasoning.
5. After length ≤ before length for improve/merge (shorter is better).
6. Do not create new files if an existing brain file already covers the topic.
7. For any proposal marked rhetorical: only produce an edit if a paired
   non-rhetorical L1/L2 proposal in the same batch confirms the same
   corrective intent. If no such pairing exists, discard the rhetorical
   proposal silently (omit it from the output array).
```

规则 7 是 Phase 1 → Phase 2 的反问否定捆绑指令。

### 2.4 质量约束

| 约束 | 检查 | 不通过行为 |
|------|------|-----------|
| After 可解析 | YAML frontmatter parse 成功（有 frontmatter 的文件适用） | 丢弃 |
| After 非空 | TrimSpace != ""（deprecate 除外） | 丢弃 |
| 实质变化 | Jaccard(Before, After) < 0.95 | 丢弃 |
| reasoning 有 | len > 0（deprecate 除外） | 丢弃 |
| 编辑方向正确 | After 长度 ≤ Before 长度（improve/merge 类型） | 丢弃（仅 soft constraint；语义改进优先于字符数） |

### 2.5 因果验证

```go
func (d *Dream) verifyCausality(edit LLMEdit, proposals []EditProposal) bool {
    // 1. proposal_id 存在于 Phase 1 输出中
    p := findProposal(edit.ProposalID, proposals)
    if p == nil { return false }

    // 2. create_* → 有 ≥1 条 L1/L2 证据
    if strings.HasPrefix(edit.Action, "create_") {
        return p.Confidence >= 0.85 && len(p.Evidence) >= 1
    }

    // 3. improve_* → Before 内容能找到 Phase 1 提的错误模式
    if strings.HasPrefix(edit.Action, "improve_") {
        for _, ev := range p.Evidence {
            if strings.Contains(p.Before, extractPattern(ev)) {
                return true
            }
        }
        return false
    }

    // 4. merge_* → 源文件确实有 Jaccard > 0.7
    if edit.Action == "merge" {
        return p.Confidence >= 0.7
    }

    // 5. rhetorical_candidate 必须与一个 L1/L2 proposal 配对
    if p.IsRhetorical {
        for _, other := range proposals {
            if !other.IsRhetorical && other.Confidence >= 0.85 && shareEvidence(p, other) {
                return true
            }
        }
        return false
    }

    return true
}
```

---

## Phase 3: APPLY

### 3.1 临时工作区

Dream 不直接在 brain 目录上工作。创建临时 git 工作区，装 brain 的镜像，在上面完成所有编辑，最后合并回去。

```
/tmp/dolphin-dream-12/          ← 临时工作区
  ├── .git/                     ← git clone --mirror → 轻量副本
  ├── commands/                 ← brain 文件的镜像
  ├── knowledge/
  └── ...

工作流程:
1. git clone --mirror .dolphin/brain → /tmp/dolphin-dream-12/
2. cd /tmp/dolphin-dream-12/
3. git checkout -b dream/12
4. 执行所有 edit → 逐 commit
5.
   if auto_apply:
     cd .dolphin/brain
     git pull /tmp/dolphin-dream-12 dream/12   ← ff-only merge
     git branch -d dream/12
     TUI: 💡 "Dream #12 已应用 N 项改进"
   else:
     cd .dolphin/brain
     git fetch /tmp/dolphin-dream-12 dream/12:dream/12
     TUI: 💡 "Dream #12 等待审核 — /dream review"

6. rm -rf /tmp/dolphin-dream-12/
```

### 3.2 为什么隔离

| 直接在 brain 上操作 | 临时工作区 |
|-------------------|-----------|
| Checkout 需干净工作树 | 无此约束 |
| AutoCommit 需暂停 | 不需要——互不干扰 |
| 中断后需清理分支 | 直接 rm -rf |
| 如果用户同时在编辑 brain | 冲突风险 | 合并时才发现冲突，安全 |
| go-git 并发操作 | 需加锁 | 各自操作各自 repo |

### 3.3 中断

用户活跃 → `d.abort()` → `dreamCtx.Cancel()` → Phase 2 LLM 终止 → `rm -rf /tmp/dolphin-dream-12/`。干净。

### 3.4 `/dream revert <id>`

```bash
/dream revert 12
# 1. 从 dream.json 读取 merge SHA
# 2. cd brain && git revert <merge_sha>
# 3. 更新 dream.json
```

---

## Phase 4: TIDY

### 4.1 自校准

**核心决策：只对有人类反馈信号的编辑做校准。**

| 模式 | 反馈信号 | 校准？ | 理由 |
|------|---------|--------|------|
| `auto_apply: false` | `/dream accept` / `/dream reject` | ✅ 是 | 用户明确确认或拒绝了编辑 |
| `auto_apply: true` | 无明确的人类信号 | ❌ 否 | 不能用系统输出验证系统自己 |

```go
func (d *Dream) calibrate(edits []Edit) {
    if d.autoApply {
        // 不校准。阈值保持在初始默认值。
        // V2: 可引入隐式负例——后续 session 的纠正信号。
        return
    }

    // 手动审核 → 有真实反馈 → 计入滑动窗口
    window := d.state.Calibration.Window
    for _, sigType := range []string{"correction", "preference", "refinement", "repetition"} {
        adopted, total := countAdoptedInWindow(window, sigType)
        if total == 0 {
            continue
        }
        rate := float64(adopted) / float64(total)

        threshold := d.state.Calibration.Thresholds[sigType]
        if rate < 0.3 && threshold < d.calibrationConfidenceCeiling {
            threshold += d.calibrationMinStep   // 更严格
        } else if rate > 0.7 && threshold > d.calibrationConfidenceFloor {
            threshold -= d.calibrationMinStep   // 更宽松
        }
        // rate 在 0.3~0.7 → 稳定区，不调整

        d.state.Calibration.Thresholds[sigType] = threshold
    }

    d.saveState()
}
```

**为什么 auto_apply 不校准：** 在自动模式下，缺少"编辑是否被用户接受"的真实信号。V2 可引入隐式负例检测（后续 session 的纠正信号是否针对上次 dream 的编辑），但 V1 不做。未校准的默认阈值已经足够保守。

**采纳率写入路径：** `/dream accept` 和 `/dream reject` 在执行时更新 `.dolphin/dream.json`：

```go
// /dream accept 处理器
func onDreamAccept(dreamID int, acceptedCommitIDs []int) {
    s := loadState()
    total := len(s.LastAppliedEdits)
    adopted := len(acceptedCommitIDs)
    s.Calibration.Window = append(s.Calibration.Window, CalibrationEntry{
        DreamID: dreamID, Adopted: adopted, Total: total,
    })
    if len(s.Calibration.Window) > s.CalibrationWindowSize {
        s.Calibration.Window = s.Calibration.Window[1:]
    }
    s.save()
}
// /dream reject 同理：adopted = 用户选择保留的 commit 数，total = 总编辑数
```

**`/dream review` 输出格式：**
```
$ /dream review
Dream #12 — 分支 dream/12 (5 commits)

  1. improve         (+3 -2) commands/deploy.md
  2. improve         (+2 -0) commands/test.md
  3. merge           (+8 -15) knowledge/deploy.md (合并 2→1)
  4. create          (+4) knowledge/deploy-preference.md
  5. deprecate       (+1) commands/old-deploy.md

接受全部？ /dream accept     拒绝全部？ /dream reject
选择性？ /dream accept 1 3    详细？ /dream diff 3
```
通过 `git log main..dream/N --oneline` + `git diff --stat main...dream/N` 拼接输出。

---

### 4.2 V2 预留：隐式负例

```go
// 每次 dream 触发时，检测上次编辑是否被后续 session 纠正
func (d *Dream) checkImplicitFeedback(sessions []*session.Session) {
    for _, edit := range d.state.LastAppliedEdits {
        for _, s := range sessions {
            if hasCorrectionTargeting(s, edit.Target) {
                d.state.EditFeedback[edit.ID] = "corrected_by_user"
                break
            }
        }
    }
}
// "用户没纠正"不算正例——只是没被否定。阈值不因此下调。
```

### 4.3 State 容灾

`dream.json` 是主存储，在 `.dolphin/`（gitignored）。如果丢失，系统能自愈但会丢掉所有历史数据。

**每次 dream 成功后，写一份只读副本到 brain git：**

```
brain/.dream/state.json   ← gitignored=false，每次 dream 后更新
.dolphin/dream.json       ← 主存储，gitignored
```

启动时恢复：

```go
func (d *Dream) loadState() {
    if fileExists(".dolphin/dream.json") {
        d.state = load(".dolphin/dream.json")
        return
    }
    if fileExists(".dolphin/brain/.dream/state.json") {
        d.state = load(".dolphin/brain/.dream/state.json")
        d.logger.Warn("dream state recovered from brain git backup")
        return
    }
    d.bootstrap()  // 全新启动
}
```

### 4.4 冷启动

首次运行（dream.json 不存在）：

```
Dream #0 (bootstrap):
  1. 扫描 brain 所有 `.md` 文件 → 建立文件索引
  2. 初始化 usage = {}
  3. 初始化 calibration 默认值（所有信号类型阈值 = baseline）
  4. 写入 .dolphin/dream.json + brain/.dream/state.json
  5. 不执行任何编辑
  6. last_dream_id = 0
```

---

## 用户命令

```bash
/dream status           # 上次 dream、效果摘要、待审核分支、连续空跑、是否有 interrupted
/dream history [N]      # 最近 N 次变更摘要
/dream preview          # 干跑 Phase 0+1+2，只读不写
/dream now              # 手动触发，跳过空闲等待 + Phase 0 门控（用户要了，就跑）
/dream review           # 审核当前分支
/dream accept [N ...]   # merge（全部或 cherry-pick）
/dream reject [N ...]   # 删除分支（全部或 cherry-pick）
/dream diff [N]         # 查看单条 commit diff
/dream revert <id>      # git revert 该次 dream 的 merge commit
```

---

## 编辑期望行为

**当用户从不纠正 agent 时（teachable moment = 0）：**
Dream 产出仅限于：重复模式检测（create）、文件合并（merge）、废弃标记（deprecate）。不产生不存在的编辑。这是**正确行为**——agent 做得对时不需要改。不产生编辑 ≠ Dream 坏了。

---

## 实现文件

| 文件 | 内容 |
|------|------|
| `internal/brain/fetch.go` | **新增** — `FetchFrom(srcRepo, srcBranch, dstBranch)` 从外部仓库拉分支到 brain |
| `internal/dream/state.go` | state 管理（load/save `.dolphin/dream.json`） |
| `internal/dream/gate.go` | Phase 0 门控 |
| `internal/dream/scan.go` | Phase 1 session 提取 + brain 扫描 + 交叉分析 |
| `internal/dream/edit.go` | Phase 2 批量 LLM 调用 + few-shot + 因果验证 + 质量约束 |
| `internal/dream/apply.go` | Phase 3 临时工作区 + git clone + 编辑 + ff-merge + 中断清理 |
| `internal/dream/tidy.go` | Phase 4 自校准阈值 |
| `internal/dream/bootstrap.go` | 首次运行初始化 |
| `internal/dream/types.go` | 所有类型 |
| `internal/command/dream.go` | /dream 命令集 |
| `internal/setup/boot_dream.go` | DreamBootstrapper |
| `internal/lifecycle/builder.go` | StepDream() |
| `internal/lifecycle/pipeline.go` | AutoCommit 跳过 dream 活跃窗口 |

---

## 质量保证

| 风险 | 对策 |
|------|------|
| 冷门文件被忽略 | Impact 不因低频引用做惩罚，只因高频引用做奖励 |
| 同文件多编辑覆盖 | Phase 1 合并（保留最高 Impact） |
| LLM 幻觉编辑 | 因果验证 5 条规则 + 质量约束 5 项检查 |
| 冷启动空洞 | bootstrap dream (#0) 建基线 |
| 中断 | rm -rf 临时工作区，下次重新做梦 |
| 校准振荡 | 稳定区 [0.3, 0.7] + 有界步长 |
| Compaction 回声 | 跳过 IsSummary 消息 |
| 自动 merge 不可逆 | `/dream revert <id>` 用记录的 merge SHA |
| 无纠正信号时"无产出"被误解 | 文档明确：无纠正 = 无产出是正确行为 |

---

## 验证策略

Dream 的核心风险清单：

| # | 风险 | 严重性 | 可自动化 |
|---|------|--------|---------|
| R1 | 纯规则逻辑 bug（Phase 0/1/4 计算错误） | 中 | ✅ |
| R2 | LLM 输出格式非法（JSON 不解析） | 高 | ✅ |
| R3 | LLM 幻觉编辑（因果断裂、虚构证据） | 致命 | ⚠️ 部分 |
| R4 | 因果验证门控失效（漏掉幻觉） | 致命 | ✅ |
| R5 | 恶意/危险 LLM 输出（命令注入、路径逃逸） | 致命 | ✅ |
| R6 | 并发竞态（同时触发、同时读写 state） | 中 | ✅ |
| R7 | 中断后资源泄漏（临时工作区残留、goroutine 未退） | 低 | ✅ |
| R8 | 校准漂移/振荡 | 中 | ✅ |
| R9 | Prompt 迭代导致 LLM 输出质量退化 | 中 | ✅ |
| R10 | 生产环境长期退化未被发现 | 中 | ✅ |
| R11 | 性能退化（大规模 session 下超时） | 中 | ✅ |

---

### 性能边界（所有层的约束条件）

测试必须在性能预算内通过。超出预算 → 失败。

| 阶段 | 输入规模 | 最大耗时 | 最大 token | 测试验证方式 |
|------|---------|---------|-----------|------------|
| Phase 0 | 100 sessions | 10ms | 0 | `TestPerf_Gate_100Sessions` |
| Phase 1 | 100 sessions, 5000 messages | 2s | 0 | `TestPerf_Scan_100Sessions` |
| Phase 2 | 10 EditProposals | 30s | 5000 input + 3000 output | `TestPerf_Edit_MaxBatch` |
| Phase 3 | 10 文件编辑 | 5s | 0 | `TestPerf_Apply_MaxEdits` |
| 完整梦 | Phase 0→4 | 60s timeout | 8000 total | `TestPerf_FullDream_Under60s` |

```go
TestPerf_Gate_100Sessions:
  // 创建 100 个 mock session，总计 5000 条消息
  // Phase 0 gate 必须 < 10ms
  
TestPerf_Scan_100Sessions:
  // Phase 1 扫描 100 session → 处理 5000 条消息
  // 必须 < 2s（单 goroutine）
  
TestPerf_Scan_QuadraticGuard:
  // 100 session 两两比较 → 10000 对
  // O(n²) 的相似问题检测不得超过 500ms
  // 如果超过 → 降级采样（随机 50 个 session 做全量比较）
  
TestPerf_Edit_TokenBudget:
  // 10 EditProposal → Phase 2 prompt
  // input tokens ≤ 5000, output tokens ≤ 3000
  // token 超出 → 按 Impact 截断到 token budget 内
  
TestPerf_FullDream_Timeout:
  // 带 60s context timeout 运行完整 Dream
  // 超时 → 测试失败。Dream 必须能在 60s 内完成或 fail fast
```

### 层 1：单元测试（纯规则逻辑，CI 每次 push）

覆盖 Phase 0/1/2/3/4 的所有纯规则路径，含正常路径 + 边缘路径。

```go
// === Phase 0 Gate — 正常路径 ===
TestGate_TooFewSessions           // sessions < min_sessions → skip
TestGate_TooFewUserMessages       // user messages < 8 → skip
TestGate_ConsecutiveEmpty         // 连续 3 次空跑 + sessions < 5 → skip
TestGate_SessionOverlap           // 与上次重叠 session → skip
TestGate_ExitAcceleration         // /exit 后 timer 缩到 exit_idle_minutes
TestGate_ZeroSessions             // 0 session → skip 不 panic
TestGate_NegativeConsecutiveEmpty  // 边界: empty 计数异常 → 重置为 0

// === Phase 1 Scan — 正常路径 ===
TestScan_TeachableMoment_L1         // "不对，用 X" + tool_call 变化 → confidence 0.85
TestScan_TeachableMoment_L2_Confirm // tool_call 参数变了 → 确认
TestScan_TeachableMoment_L2_Drop    // tool_call 没变 → 丢弃
TestScan_TeachableMoment_L3_CrossSession // 同类型纠正 ≥2 session → +0.05
TestScan_CompactionFilter           // IsSummary 消息被跳过
TestScan_ExplicitPreference         // "以后都用 X 不要 Y" → L1
TestScan_RepeatedPattern            // ≥5 次跨 session → confidence 0.80
TestScan_FactDedup                  // Jaccard > 0.7 → merge 信号
TestScan_RhetoricalCandidate        // "你觉得这样做对吗" → IsRhetorical
TestScan_RhetoricalNotAlone         // 反问无 L1/L2 配对 → Phase 2 拒绝

// === Phase 1 Scan — 边缘验证 ===
TestScan_EmptySessionMemory         // session 存在但 memory 为空 → 不 panic
TestScan_MalformedMessage           // 消息缺 role 字段 → 跳过，不崩溃
TestScan_ToolCallNilArgs            // tool_call 有 name 无 args → 正常处理
TestScan_UserMsgEmptyContent        // user content 为空字符串 → 跳过
TestScan_MultipleSameRoundCorrections // 同一轮 3 次纠正 → 合并为 1 条
TestScan_CorrectionAcrossRoundBoundary // 纠正跨轮边界 → 正确归属
TestScan_SingleSessionRepeated10    // 单 session 重复 10 次 → 不算跨 session
TestScan_ChineseNestedNegation      // "没错，不对的地方是..." → 识别为纠正
TestScan_MixedLang                  // "以后用 k8s not docker" → 分词正确
TestScan_VeryLongMessage            // 5000 字符 → 截断，不 OOM
TestScan_ConsecutiveBlankMessages   // 序列中间空白消息 → 跳过
TestScan_TimestampOutOfOrder        // 时间戳乱序 → 排序后处理
TestScan_AllToolCallsIdentical      // 100 条相同 → O(n) 不退化
TestScan_NoUserMessages             // 全部是 system/tool → 无 TeachableMoment

// === Phase 1 Impact — 正常路径 ===
TestImpact_HighRefs                 // Ref 30 → ×2.0
TestImpact_ZeroRefs                 // Ref 0 → ×1.0（不惩罚）
TestImpact_CorrectionBoost          // correction → ×1.5
TestImpact_RefinementModerate       // refinement → ×0.8
TestImpact_ObsolescenceLow          // obsolescence → ×0.3
TestImpact_FreshnessDecay_7Day      // 7 天 → ×0.5
TestImpact_FreshnessDecay_30Day     // 30 天 → 下界 0.1
TestImpact_SameTargetMerge          // 同 Target → 保留最高
TestImpact_MaxEditsPerDream         // 截断到配置上限

// === Phase 1 Impact — 边缘验证 ===
TestImpact_ZeroRefs_FreshSignal     // Ref=0 但 1 天前纠正 → ~1.5
TestImpact_ZeroRefs_StaleSignal     // Ref=0 且 60 天前 → 下界 0.1
TestImpact_NegativeRefs             // Ref 为负 → 视为 0
TestImpact_DivisionByZero           // 所有权重为零 → return 0.0
TestImpact_NilTarget                // target=nil → 基础权重 1.0

// === Phase 2 因果验证 — 每条规则一拒一过 ===
TestVerify_ProposalNotFound         // proposal_id 不存在 → 拒
TestVerify_CreateNoL1Evidence       // create + 无 L1/L2 → 拒
TestVerify_CreateWithL1             // create + L1 + 0.90 → 过
TestVerify_ImproveNoPattern         // Before 无错误模式 → 拒
TestVerify_ImproveWithPattern       // Before 含模式 → 过
TestVerify_MergeLowConfidence       // merge + conf < 0.7 → 拒
TestVerify_MergeWithJaccard         // merge + conf ≥ 0.7 → 过
TestVerify_DeprecateAlways          // deprecate 总是通过
TestVerify_RhetoricalNoPair         // 无 L1/L2 配对 → 拒
TestVerify_RhetoricalWithPair       // shareEvidence L1 → 过

// === Phase 2 因果验证 — 边缘验证 ===
TestVerify_EvidenceSessionNotExist  // evidence 引用不存在的 session
TestVerify_EvidenceMsgIndexOOB      // evidence 消息索引超出范围
TestVerify_EmptyProposalList        // proposals=[] → 所有 edit 拒绝
TestVerify_DuplicateProposalIDs     // 同 ID 两个 proposal → 只一条通过
TestVerify_EvidenceFabrication      // after 中提了不存在的 evidence → 拒

// === Phase 2 质量约束 — 正常路径 ===
TestQuality_YamlParseFail           // 非法 YAML → 丢弃
TestQuality_EmptyAfter              // 空 after → 丢弃
TestQuality_JaccardTooSimilar       // Jaccard > 0.95 → 丢弃
TestQuality_NoReasoning             // reasoning 空 → 丢弃
TestQuality_ImproveNotShorter       // improve after > before → 丢弃

// === Phase 2 质量约束 — 边缘验证 ===
TestQuality_FrontmatterMissing      // 无 frontmatter 的普通 md → 不检查 YAML
TestQuality_AfterOnlyWhitespace     // "   \t\n  " → 丢弃
TestQuality_AfterOnlyFrontmatterChange // 只改 YAML 字段 → 有效
TestQuality_ReasoningGibberish      // "...###!#" → 拒绝
TestQuality_JaccardExactlyZero      // before/after 完全不同 → 通过
TestQuality_JaccardUndefinedNew     // before="" → 跳过 Jaccard
TestQuality_AfterMaxLength          // after > 100KB → 拒绝
TestQuality_FrontmatterOnlyContent  // before 无 frontmatter, after 有 → 正常（不检查）

// === Phase 3 Apply — 边缘验证 ===
TestApply_EmptyEditList             // edits=[] → 不创建分支
TestApply_AllEditsFail              // 全部质量失败 → 不创建分支
TestApply_MergeSourceNotExist       // merge 源文件不存在 → skip 不崩溃
TestApply_DeprecateDouble           // 已 deprecated → 不重复加
TestApply_TempDirCreateFail         // /tmp 无权限 → 错误返回
TestApply_GitFetchFails             // fetch 失败 → 不删临时区
TestApply_FileCooldownActive        // 冷却期 → 跳过
TestApply_FileCooldownExpired       // 冷却过期 → 正常执行

// === Phase 4 校准 — 边缘验证 ===
TestCalibrate_AutoApplyNoop         // auto_apply → 不变
TestCalibrate_StableZone            // rate 0.5 → 不变
TestCalibrate_RaiseThreshold        // rate 0.2 → +0.05
TestCalibrate_LowerThreshold        // rate 0.8 → -0.05
TestCalibrate_Ceiling               // 上限 0.95
TestCalibrate_Floor                 // 下界 0.30
TestCalibrate_NoTotalSkip           // total=0 → 不除零
TestCalibrate_50RunStability        // 无漂移
TestCalibrate_AllTotalZero          // 全部 total=0 → 不调整
TestCalibrate_RateBoundary030        // rate=0.300 → 不变
TestCalibrate_RateBoundary070        // rate=0.700 → 不变

// === State 容灾 — 边缘验证 ===
TestState_PrimaryFallback           // 主存储优先
TestState_BrainBackupRecovery       // 主丢失，副本恢复
TestState_Bootstrap                 // 全新
TestState_SaveWriteBoth             // 保存双写
TestState_SavePartial               // 主成功，副本失败 → 不崩溃
TestState_BrainBackupStale          // 主新副本旧 → 选主
TestState_ConcurrentReadDuringSave  // 读时写 → 一致快照
TestState_FileCorrupt               // JSON 合法但缺字段 → 恢复+合并

// === Token 预算验证 ===
TestTokenBudget_ExtractCompact      // 100 条 → Top-20
TestTokenBudget_PromptUnderWindow   // prompt ≤ ctx 70%
TestTokenBudget_OutputUnderMax      // output ≤ max_reflect_tokens
TestTokenBudget_CreateRatioEnforced // create ≤ max_create_ratio
```

Mock 依赖：`session.Manager`、`memory.Memory`、`brain.Brain`、`llm.Provider`。

### 层 2：集成测试（真实 git + fake LLM，CI 每次 push）

```go
// === 完整 Dream 流程 ===
TestIntegration_FullDream_AutoApply
//   验证: merge 成功、文件内容正确变化、源文件删除、state 副本写入

TestIntegration_FullDream_NoAutoApply
//   验证: 分支存在但未 merge、main 不变、open_branch 设置、tips 消息收到

TestIntegration_FullDream_NoProposals
//   验证: 0 edits → 不创建分支、consecutiveEmpty++

TestIntegration_DreamPreview
//   验证: Phase 0/1/2 执行、Phase 3 未执行、proposals 返回正确、brain 未变

TestIntegration_DreamNow_SkipGate
//   验证: /dream now 跳过门控、0 edits 时提示 "无可用编辑信号"

TestIntegration_DoubleDreamNow_Reject
//   验证: 第二次 /dream now 被拒绝 "already in progress"

// === 边缘验证 ===
TestIntegration_EmptyBrain
//   验证: brain 目录无任何 .md 文件 → 0 proposals → 不创建分支

TestIntegration_GitCloneFails
//   验证: brain 目录无 git → Phase 3 优雅失败、临时区不创建

TestIntegration_Phase4Idempotent
//   验证: 同一个 state 跑两次 Phase 4 → 校准不重复调整

TestIntegration_ReapplyAlreadyApplied
//   验证: dream/N 分支已存在且 merge 过 → 跳过、不重复 merge
```

Fake LLM 返回确定性 After 内容，不依赖网络。

### 层 3：LLM 输出质量

#### 3a：Golden Set 自动评估（每次 prompt 变更时触发）

CI 中有专门的 golden-set 任务。任何人修改 Phase 2 prompt → golden set 自动运行。

```
Golden set: 15 个 EditProposal
  - 覆盖 5 种 action (improve/merge/create/deprecate/split)
  - 覆盖 5 种 brain 文件 (commands/knowledge/rules/scripts/contacts)
  - 每个 proposal 带:
    * 标准输入 (before + reason + evidence)
    * Golden after (人工编写，两人交叉审阅)
    * 最低 Jaccard 0.7
    * 语义约束: 必须包含 X / 禁止包含 Y

自动评估:
  1. 对 15 个 proposal 逐个调 Phase 2
  2. 批量: 15 个合并在一次 LLM 调用中
  3. 解析输出 (分隔符: --- PROPOSAL pN --- / --- END pN ---)
  4. 每个 proposal:
     - Jaccard(golden, llm) < 0.7 → FAIL
     - 违反语义约束 → FAIL
  5. 总分 < 12/15 → PROMPT_REGRESSION, prompt 回退
  6. 总分 12-13/15 → WARNING, 标记需人工审查
  7. 用与 CI 隔离的 LLM API key 跑（不计入用户 quota）
```

**Golden set 维护协议：**

```
1. 初始 golden set 由两人共同编写 + 交叉审阅
2. 每次 prompt 调整后:
   - 通过 golden set → 保留
   - 新增编辑类型 (如 split action) → 追加 2 个新 golden case
3. 每 3 个月: golden set review
   - 检查每个 golden after 是否仍然正确 (brain 可能会进化)
   - 过时的 → 修订
4. CI 中 golden set 跑在隔离 runner (不与其他 test 共享 LLM 限流)
```

**Golden set 腐化检测：**

```go
TestGoldenSet_NotStale
//   检查 golden set 文件最后修改时间
//   > 90 天未更新 → FAIL (提醒维护)
```

#### 3b：结构合法性（CI 每次 push，fake LLM）

```go
TestPhase2Output_ValidJSON        // 合法 JSON
TestPhase2Output_AllIDsMatch      // proposal_id 全部匹配
TestPhase2Output_NonEmptyAfter    // deprecate 外全非空
TestPhase2Output_ReasoningPresent // deprecate 外都有 reasoning
TestPhase2Output_JaccardDiffers   // Jaccard < 0.95
TestPhase2Output_NoExtraProposals // 不输出不存在的 proposal
TestPhase2Output_ProposalSeparators // 每个 proposal 有 --- PROPOSAL / --- END 分隔符
TestPhase2Output_EmptyForNoProposals // 输入 [] → 输出 {"edits":[]}
```

#### 3c：语义安全性（LLM-as-Judge，每次 prompt 变更 + CI 调度）

用与 Dream LLM 不同的模型 (或同一模型不同 temperature) 评估输出安全性。

```go
TestSemantic_NoDestructiveCommands
//   LLM-as-Judge 检查 after 字段
//   含: "rm -rf", "DROP TABLE", "chmod 777", "sudo", "> /dev/sda"
//   → FAIL

TestSemantic_NoCredentialLeak
//   含: API key pattern, password, token, secret
//   → FAIL

TestSemantic_NoUrlInjection
//   含: "curl http://evil.com", "wget -O - http://..."
//   → FAIL

TestSemantic_NoClientSideExploit
//   含: "<script>", "eval(", "require('child_process')"
//   → FAIL (brain 内容会进入 LLM context)

TestSemantic_PreservesCorrectContent
//   纠正类 edit: after 必须保留 before 中未被纠正的部分
//   LLM-as-Judge 比较 before/after, 不是纠正目标的部分不应改变
//   改变率 > 40% → FAIL (过度编辑)
```

### 层 4：对抗性测试（CI 每次 push，fake LLM 输出即恶意内容）

```go
TestAdversarial_ShellInjection      // after: "Run: docker; rm -rf /" → 拦截
TestAdversarial_YamlEscape          // after 含 "\n---\n" 分离符 → YAML parse fail
TestAdversarial_PathTraversal       // target: "../../../etc/passwd" → 拒绝
TestAdversarial_EmptyAfterDisguised // after: "\n\n\n" → TrimSpace 为空
TestAdversarial_MassiveAfter        // after: 100KB → 拒绝
TestAdversarial_DeprecateWithContent // deprecate 含非空 after → 拒绝
TestAdversarial_MissingProposalID   // 无 proposal_id → 因果拒绝
TestAdversarial_ExtraProposal       // 输出 11 条 (输入 10 条) → 截断
TestAdversarial_UnicodeHomoglyph    // 用 cyrillic "а" 替代 ascii "a" → 不解析为合法路径
TestAdversarial_NullByteInjection   // after 含 \x00 → 拒绝
TestAdversarial_RepeatedFrontmatter // after 含双份 "---" → YAML parse fail
TestAdversarial_CommandNameChange   // improve 时改了 name 字段 → 拒绝 (之前约束已删，但前端验证保留)
```

### 层 5：并发与竞态测试

```go
TestConcurrent_DreamNowTwice         // 两个 goroutine → 一个拒
TestConcurrent_StatusWhileRunning    // Phase 2 中 /dream status → 不 panic
TestConcurrent_AbortDuringPhase3     // Phase 3 写文件时 abort → 临时区清理
TestConcurrent_StateReadDuringWrite  // 写 dream.json 时读 → 一致

TestCleanup_StaleTempDirs            // SIGKILL 残留 → 启动清理
TestCleanup_GoroutineLeak            // 100 次 abort → 不增长
TestCleanup_TempDirNotExist          // 启动时 /tmp/dolphin-dream-* 不存在 → 不报错
TestCleanup_DoubleAbort              // abort 两次 → 第二次不 panic
```

### 层 6：会话回放验收（自动化，CI 调度 / PR merge 前）

用真实 LLM 跑一次完整的 Dream。session 数据使用 testdata。

```go
TestReplay_FullDream_3DaySimulation
//   1. 创建 fresh brain + 5 个构造 session
//   2. 运行 Dream(autoApply=true)
//   3. 验证:
//      ✓ dream 成功完成 (< 60s)
//      ✓ 产生了 ≥ 1 条编辑
//      ✓ 所有 edit 通过了因果验证 + 质量约束
//      ✓ brain 文件内容合法 (YAML parseable / 不含非法字符)
//      ✓ state.json 正确更新
//      ✓ LLM-as-Judge 扫描 after 内容 → 无危险指令
//   4. 与基线比较:
//      ✓ 文件净增长 ≤ +2
//      ✓ token 消耗 ≤ 8000
//      ✓ 编辑后的 brain 文件功能完整 (YAML 可解析)

TestReplay_DreamIdempotent
//   对同一个 state + sessions 跑两次 Dream
//   第二次应产出 0 edits 或少于 2 条提案 (第一次已经修好了)

TestReplay_DreamWithRevert
//   1. Dream → merge → 验证
//   2. /dream revert → 验证 brain 恢复
//   3. Dream again → 应产生与第一次相同的编辑
```

### 层 7：生产观测 + 主动告警

| 指标 | 正常 | ⚠️ 告警 | 🚨 干预 | Source |
|------|------|---------|----------|--------|
| 编辑采纳率 | 50-80% | 30-50% | <30% | dream.json |
| 连续空跑 | 0-1 | 3-5 | >5 | dream.json |
| `/dream revert` 率 | <10% | 10-30% | >30% | dream.json |
| brain 净增长/梦 | -2~+1 | +1~+2 | >+2 | git diff |
| 因果验证 reject | <30% | 30-60% | >60% | rejected.json |
| 临时区残留 | 0 | 1-2 | >2 | /tmp |
| Phase 2 tokens | <5000 | 5k-10k | >10k | metrics |
| Golden set score | 14-15/15 | 12-13/15 | <12/15 | CI |

**主动告警 (TUI tips)：**

```go
// 连续空跑 ≥3 次
TUI: 💡 "Dream 已连续 3 次跳过，信号不足或配置需调整 → /dream status"

// 连续 reject + 因果验证拒绝率 ≥50%
TUI: ⚠️ "Dream 最近 5 次编辑中 60% 被拒绝，可能 LLM 输出质量下降 → /dream history"

// /dream revert 频率 ≥30%
TUI: ⚠️ "Dream 最近 10 次编辑中 3 次被回滚，建议检查编辑质量 → /dream history"

// state.json 恢复自 brain 副本
log.Warn("dream state recovered from brain backup — check .dolphin/dream.json")
```

### 测试数据

```
testdata/dream/
  sessions/
    s1_deploy_incorrect.json  ← user: 部署，asst: docker compose
    s2_correction_k8s.json    ← user: 不对，用 k8s，asst: kubectl
    s3_create_command.json    ← user: 写一个命令，asst: 多次 exec
    s4_refinement_lint.json   ← user: 顺便跑 lint
    s5_repeat_question.json   ← user: 怎么部署 (重复)
    s6_empty_session.json     ← 0 消息 session (stress)
    s7_malformed_message.json ← 含损坏消息 (robustness)
    s8_long_session.json      ← 100 轮对话 (perf)
    s9_chinese_rich.json      ← 全部中文 + 复杂纠正模式
    s10_rhetorical_only.json  ← 只有反问，无 L1/L2

  brain/
    commands/deploy.md         ← docker compose
    commands/test.md           ← 只 go test
    commands/unused-cmd.md     ← 从未被引用 (deprecation)
    knowledge/k8s.md           ← 与 docker.md 重叠
    knowledge/docker.md        ← 与 k8s.md 重叠
    rules/code-style.md        ← 正常文件 (不应被编辑)
    scripts/backup.sh          ← 正常脚本 (验证非 command 类型)
    contacts/team.md           ← 正常联系人
    workflow/deploy.yaml       ← yaml 文件 (测试非 md 是否被跳过)

  golden/
    p01_improve_deploy.json
    p02_create_k8s_knowledge.json
    p03_merge_deploy_rules.json
    p04_deprecate_unused.json
    p05_split_long_fact.json
    p06_improve_script.json
    p07_create_contact.json
    p08_deprecate_stale_fact.json
    ... (15 total)

  adversarial/
    shell_injection.json
    yaml_escape.json
    path_traversal.json
    unicode_homoglyph.json
    null_byte.json
    massive_after.json
    repeated_frontmatter.json
```

### 测试覆盖矩阵

| 风险 | L1 | L2 | L3 | L4 | L5 | L6 | L7 | 覆盖率 |
|------|----|----|----|----|----|----|----|--------|
| R1 逻辑 bug | ✅ | - | - | - | - | - | - | 1层 |
| R2 JSON 非法 | ✅ | - | ✅ | - | - | ✅ | - | 3层 |
| R3 LLM 幻觉 | ✅ | - | ✅ | - | - | ✅ | - | 3层 |
| R4 因果验证失效 | ✅ | - | - | - | - | ✅ | - | 2层 |
| R5 恶意输出 | - | - | ✅ | ✅ | - | ✅ | - | 3层 |
| R6 并发竞态 | - | - | - | - | ✅ | - | - | 1层 |
| R7 资源泄漏 | - | - | - | - | ✅ | - | ✅ | 2层 |
| R8 校准漂移 | ✅ | - | - | - | - | - | ✅ | 2层 |
| R9 Prompt 退化 | - | - | ✅ | - | - | - | - | 1层 |
| R10 长期退化 | - | - | - | - | - | - | ✅ | 1层 |
| R11 性能退化 | ✅ | - | - | - | - | ✅ | ✅ | 3层 |
