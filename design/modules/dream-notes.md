# Dream 实现注意事项

本文档记录 Dream 设计之外的残余风险——实现依赖、运行环境和用户行为层面需要注意的问题。

## 残余风险与缓解

以下 11 项风险不在测试覆盖矩阵中——不是纯规则逻辑或 LLM 质量问题，而是实现依赖、运行环境和用户行为层面的风险。每一项都有具体缓解方案。

---

### 1. go-git `file://` protocol 兼容性

**风险：** go-git 对本地文件路径的 `Remote.Fetch()` 支持不稳定，Phase 3 在 `git fetch 临时工作区 → brain` 时可能失败。编辑已完成但无法合并。

**缓解：** Phase 3 的合并步骤用系统 `git` CLI 而非 go-git。

```go
// fallback 链：go-git → system git
func (d *Dream) fetchFromTemp(tmpDir, branch string) error {
    // 尝试 go-git Remote.Fetch(file://...)
    if err := d.brain.FetchFrom(tmpDir, branch, branch); err == nil {
        return nil
    }
    d.logger.Warn("go-git fetch failed, falling back to system git")
    // fallback: os/exec git fetch
    return exec.Command("git", "-C", d.brainDir, "fetch", tmpDir, 
        fmt.Sprintf("%s:%s", branch, branch)).Run()
}
```

Brain 的 go-git 负责本地 commit。系统 git 负责临时 repo ↔ brain 之间的传输。不引入新依赖——系统 git 已经存在。

---

### 2. Dream ↔ Compaction 读写中间态

**风险：** Dream Phase 1 调用 `memory.Read()` 时，CompactionStage 正在 `memory.Replace()` 写入新数据。Read 可能拿到 JSON 序列化的中间态。

**缓解：** Phase 1 启动时对 session memory 做一次性快照。

```go
func (d *Dream) scan(ctx context.Context) (*ExtractResult, error) {
    sessions := d.sessionMgr.List(ctx)
    
    // 快照：读取所有 session memory 到内存，不保持对 session 的引用
    var snapshots []sessionSnapshot
    for _, s := range sessions {
        msgs, _ := d.memory.Read(ctx, s.ID)
        snapshots = append(snapshots, sessionSnapshot{ID: s.ID, Messages: msgs})
    }
    
    // 后续所有 Phase 1 分析都用快照，不重新读取
    return d.analyze(snapshots)
}
```

后续 Compaction 的修改不影响快照。快照的内存开销可控（100 session × 平均 50 条消息 × 500 字节 ≈ 2.5MB）。

---

### 3. 模型升级破坏 golden set 基线

**风险：** 模型 provider 升级（v4→v5），相同 prompt 产生风格不同的输出。Jaccard < 0.7 但语义正确，或 Jaccard > 0.7 但漏掉关键纠正。

**缓解 A：** golden set 评估加入第二个维度——Jaccard + LLM-as-Judge 语义一致性。

```go
func evaluateGolden(output, golden LLMOutput) Verdict {
    jac := jaccard(output.After, golden.After)
    if jac >= 0.7 {
        return PASS  // 快速路径
    }
    // 低于阈值 → LLM-as-Judge 二次评估
    semantic := llmJudge.Compare(output.After, golden.After, golden.SemanticConstraints)
    if semantic.IsCorrect {
        return PASS_REBASELINE  // 语义正确，提示需要更新 golden after
    }
    return FAIL
}
```

**缓解 B：** 每次模型升级后的第一次运行结果不自动 reject——标记为 `NEEDS_REBASELINE`。人工确认后更新 golden set 文件。

---

### 4. 冷却期阻断纠正

**风险：** 文件被 Dream 编辑后进入 5 次冷却期。冷却期内用户的纠正信号、精炼信号全部被跳过。用户看到 Dream 不处理这些信号，困惑。

**缓解：** 冷却期对 correction 类型放开。

```go
func (d *Dream) isInCooldown(target string, signal EditSignal) bool {
    cd := d.state.FileCooldowns[target]
    if cd == nil {
        return false
    }
    if signal.Type == "correction" {
        // 纠正信号打破冷却期——上次编辑可能改错了
        return false
    }
    return d.state.LastDreamID < cd.CooldownUntilDream
}
```

只对 `refinement` 和 `obsolescence` 保持冷却——这两种不是紧急的，可以等。`correction` 总是立即处理。

---

### 5. Brain 文件编码

**风险：** 用户手动编辑的文件可能是 GBK/ISO-8859-1 编码。Dream 读取 → YAML parse → 乱码 → 丢弃。

**缓解：** Phase 1 扫描时检测编码并归一化。

```go
func normalizeEncoding(data []byte) (string, error) {
    // 1. 尝试 UTF-8 → 成功直接返回
    if utf8.Valid(data) {
        return string(data), nil
    }
    // 2. 尝试常见编码 → GBK, ISO-8859-1, Shift-JIS
    for _, enc := range []encoding.Encoding{simplifiedchinese.GBK, charmap.ISO8859_1} {
        if decoded, err := enc.NewDecoder().Bytes(data); err == nil && utf8.Valid(decoded) {
            return string(decoded), nil
        }
    }
    // 3. 无法识别 → 保留原始字节为 UTF-8 replacement
    return strings.ToValidUTF8(string(data), "�"), nil
}
```

`golang.org/x/text` 已经在项目依赖中（glamour 会引入），不需要额外依赖。

---

### 6. State 双写不一致检测

**风险：** 主存储写成功，brain 副本写失败（磁盘满）。副本悄悄落后一个版本。用户不会发现。

**缓解：** 保存时在 state 中嵌入 checksum。加载时比对。

```go
func (d *Dream) saveState() error {
    d.state.Checksum = d.state.ComputeChecksum()
    
    // 写主存储
    writeJSON(".dolphin/dream.json", d.state)
    
    // 写副本
    if err := writeJSON(".dolphin/brain/.dream/state.json", d.state); err != nil {
        d.logger.Warn("dream state backup write failed", zap.Error(err))
        d.state.BackupStale = true  // 标记，下次 /dream status 可见
    } else {
        d.state.BackupStale = false
    }
}
```

`/dream status` 显示 `backup: OK` 或 `backup: stale (last save failed)`。

---

### 7. 首次体验恐惧

**风险：** 用户首次使用 Dolphin，Dream 在后台自动编辑 brain 文件。用户不知道发生了什么，觉得 agent 擅自改了自己的配置。

**缓解：** 首次 Dream 完成时不静默——在 TUI tips 中额外显示一行解释。

```go
if d.state.TotalDreams == 1 {
    tui.Send(tipsMsg{
        text: "💡 Dream 已完成首次自我改进。Dream 在你离开时自动优化脑内容（修正错误、去重）。用 /dream status 查看详情。关闭: dream.enabled=false",
        duration: 15 * time.Second,  // 首次显示更久
    })
}
// 后续静默
```

Bot 首次启动时也在 welcome 消息中提及 Dream 的存在和目的。

---

### 8. `/dream now` 的 0 用户体验

**风险：** 用户手动触发，0 产出。感觉像功能坏了。

**缓解：** 改进输出措辞。

```
/dream now

{产出编辑}  → "Dream #15 完成: 改进了 deploy 命令、合并了 k8s 相关文件"
{0 编辑}    → "Dream #15: 你的 2 个 session 中没有发现需要改进的脑内容——agent 做得很好。"
               (这不是错误，是正面反馈)
{无信号}    → "Dream #15: 信号不足（需要至少 8 条用户消息），无法启动。继续使用的过程中 Dream 会自动积累信号。"
```

三种场景三种措辞，0 不是错误是 affirmation。

---

### 9. TUI tips 提醒疲劳

**风险：** 同一告警每次启动都推送。用户习惯后无视。

**缓解：** 每个告警类型只推送一次——在状态变化时。

```go
func (d *Dream) alertIfChanged(key string, current int, threshold int, msg string) {
    last := d.state.LastAlerted[key]
    crossed := (last < threshold && current >= threshold) || (last == 0 && current >= threshold)
    if crossed {
        tui.Send(tipsMsg{text: msg, duration: 8 * time.Second})
        d.state.LastAlerted[key] = current
        d.saveState()
    }
}
// 连续空跑: 只在达到 3 时提醒一次，不重复提醒 4、5、6...
// /dream status 中持续显示最新状态
```

---

### 10. 测试数据与真实 session 的差距

**风险：** 234 条测试全部通过，但上线后 Phase 1 产出与测试预期完全不同。测试 session 是精雕的，真实 session 是混乱的。

**缓解 A：** testdata 中加入真实 session dump。

```
testdata/dream/sessions/
  real_session_001.json   ← 从实际使用导出，脱敏（替换真实路径、人名）
  real_session_002.json
  real_session_003.json
```

这些 session 不追求触发特定信号——追求真实性。测试不是"验证 Phase 1 正确识别了 X"而是"验证 Phase 1 在处理真实消息时行为不退化、不崩溃"。

**缓解 B：** Phase 1 对 production session 做 shadow mode。定期（每周一次）在非生产环境跑一次完整 Dream，用最近一周的真实 session 数据。不 edit brain——只输出 proposals 到文件供人工审查。

---

### 11. 设计文档漂移

**风险：** 实现时调整了参数、逻辑或阈值，忘了更新设计文档。6 个月后重读文档，与代码不一致。

**缓解：** 测试用例名链接到设计章节。

```go
// 每个测试文件头部注释引用设计文档
// Design: design/modules/dream.md § Phase 1.1 — 信号分层
// Design: design/modules/dream.md § Phase 2.4 — 因果验证规则 3

func TestVerify_ImproveNoPatternInBefore(t *testing.T) { ... }
```

代码 review 时检查：改动逻辑 → 改了测试 → 更新设计文档对应章节。CI 中加一个简单的 lint：检查测试文件头部的设计文档引用章节是否仍然存在（文件改名/行号漂移时会报警）。

---

### 风险缓解汇总

| 风险 | 严重性 | 缓解复杂度 | 缓解后残余 |
|------|--------|-----------|----------|
| 1. go-git protocol | 高 | 低（15行 fallback） | 低 |
| 2. 读写中间态 | 中 | 低（快照） | 极低 |
| 3. 模型升级 golden set | 中 | 中（LLM-as-Judge 二次） | 中（语义判断不完美） |
| 4. 冷却期阻断纠正 | 中 | 低（correction 跳过冷却） | 极低 |
| 5. 文件编码 | 低 | 低（golang.org/x/text） | 低 |
| 6. State 双写不一致 | 低 | 低（checksum + /dream status） | 极低 |
| 7. 首次体验恐惧 | 中 | 低（首次提示） | 低 |
| 8. /dream now 0 产出 | 低 | 低（措辞改进） | 极低 |
| 9. TUI tips 疲劳 | 低 | 低（状态变化推送） | 极低 |
| 10. 测试数据真实度 | 中 | 中（真实 session + shadow mode） | 中 |
| 11. 设计文档漂移 | 低 | 低（测试注释引用） | 低 |
