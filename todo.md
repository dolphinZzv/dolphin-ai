# 再审计结果 (2026-06-14)

> **说明**: 部分发现已在首次审计后修正。当前版本基于最新代码重新审计。

---

## 缺陷 (Bugs)

### 1. [严重] Workflow tool handlers 传输空 transportID
`internal/workflow/tools.go:67,104` — `runWorkflowHandler` 和 `continueWorkflowHandler` 调用 `engine.Run(ctx, spec, "")` 传递空 `transportID`。`progress()` 调用导致 `agentIO.OnResult` 发现空 `TransportID` 后**广播到所有 transports**。长工作流下每条进度消息 = 发给每个连接的 transport。

**修复方向**: 从 tool call context 的 `transport.GetInfo(ctx)` 提取 transportID。如果可以获取就传入；否则保持空字符串降级为广播。

### 2. [中] SetProcessing 为空操作但保留公开签名
`internal/agentio/agentio.go:234` — `SetProcessing(v bool)` 函数体为空。任何仍调用此方法的外部代码将静默观察到 `Processing()` 返回不一致。agentloop 已迁移到 `SetActive/ClearActive`，但 `SetProcessing` 是公开 API，无法保证无其他调用方。

### 3. [中] writeResult() 输出路径与 spec 来源无关
`internal/workflow/result.go:41` — `path := spec.Name + ".result.yaml"` 使用裸文件名写入当前工作目录，而非 spec 源文件所在目录。如果 workflow 从 `/tmp/proj/my.workflow.yaml` 加载，结果写到 `CWD/my.result.yaml` 而非 `/tmp/proj/my.result.yaml`。

**影响**: 多项目场景下结果文件互相覆盖或散落在错误位置。

### 4. [低] workflow.max_steps 配置项未被任何代码读取
`internal/config/config.go:74` 定义 `workflow.max_steps: 50`，但全代码库无任何读取该配置的地方。**配置项是无用字段 (dead config)**。

### 5. [低] Engine.brain 字段存储但从未使用
`internal/workflow/engine.go:22` — `brain *brain.Brain` 在 `NewEngine` 中存储（engine 结构体字段、boot_workflow.go:17 传入），但 Engine 的任何方法均未引用该字段。要么移除、要么在未来真正使用。

---

## 逻辑 / 正确性

### 6. [中] buildTemplateData 对 foreach 单实例暴露格式不一致
`internal/workflow/template.go:121-139` — 当 foreach 有多个实例时，`$stepID` 暴露为 `[{key, result}, ...]` 列表；当 foreach 只有一个实例时，`$stepID` 通过 `sr.Result`（可能为 nil）或 `Instances[0].Result` 暴露为裸值。模板作者无法统一处理。根因在 engine.go:165-171 依赖 `instResults[0].Key == s.ID` 判断是否使用裸结果。

### 7. [低] allInstDone 对空结果返回 true
`internal/workflow/engine.go:317-318` — `allInstDone([]InstanceResult{})` 返回 `true`。空 foreach 无实例时走 done 分支。行为是 intended（vacuously true），但无文档说明。

### 8. [低] specLookup 线性扫描，大批量 workflow 效率低
`internal/workflow/engine.go:307-314` — 每个调度周期对每个 ready step 调用一次，整体 O(n²)。建议改用 `map[string]int` 预处理。

---

## 数据竞态

### 9. [严重] command 并发执行有 cobra 内部竞态
`internal/command/command_test.go:116` `TestRegistryExecuteContextConcurrent` — cobra 的 `Commands()` 排序（修改 commands 切片）与 `ExecuteC()` 的 `InitDefaultHelpCmd`（增删 commands）之间有数据竞争。**非本次变更引入**，但 `go test -race` 持续暴露。属于上游库问题，可考虑 `Registry.Execute` 加锁或限制单并发。

---

## 代码质量

### 10. sortedWorkerIDs 手动冒泡排序
`internal/command/queue.go:118-132` — 应使用 `sort.Strings(ids)`。

### 11. Session lock GC 仅当 poolSize > 1 时运行
`internal/agentloop/agentloop.go:54` — 默认 `agent.pool_size = 1` 时 GC goroutine 不启动，session locks 无限累积。即使单 worker 也应在 `Run()` 返回时清理 map。

### 12. Workflow executor 每轮 LLM 调用发送全部注册 tool
`internal/workflow/executor.go:38` — `e.toolReg.List(ctx)` 返回所有 tool 定义。考虑按 step 按需筛选以节省 token。

### 13. Template 渲染缺少 FuncMap
`internal/workflow/template.go:61` — `template.New("prompt").Parse(compiled)` 无 `FuncMap`。用户无法使用 `{{upper}}` 等标准模板函数。

### 14. ForEach 实例缺少 per-instance 事件
`internal/workflow/engine.go:131-139` — 只有 step 级别发布 `EventWorkflowStepChange`。Observability 工具无法跟踪单个 foreach 实例进度。

---

## 已修正 (首次审计时发现、之后修复)

| 原问题 | 状态 |
|--------|------|
| validateContinue 是空操作 | 已修正 — 现在检查已完成的 step 不被删除 |
| resultPath() 未使用 | 已修正 — 已删除该函数 |
| findCheckpointIndex() 未使用 | 已修正 — 已删除该函数 |
| 19 个 .result.yaml 检入源码树 | 已修正 — `.gitignore` 已添加 `internal/workflow/*.result.yaml` |
