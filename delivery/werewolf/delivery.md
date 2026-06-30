# Agent Mesh 交付清单

## 一、设计修订（design/modules/agent-mesh.md）

修订 7 个设计漏洞：

1. traceparent 注入改用 OTel 标准 propagator（W3C TraceContext）
2. 熔断器与重试的交互明确化（重试失败递增熔断计数，OPEN 时终止重试）
3. 大文件 reference 模式退路明确（共享存储→分块上传→报错，禁止静默退化 inline）
4. `tasks/cancel`/`tasks/get` 降为 proto=1 基础能力
5. per-session 限流从 5/m 提到 30/m，与 max_children 并发自洽
6. gossip 同名冲突加 5 级 tie-breaker
7. workflow StepSpec.Agent 标注具体迁移点

## 二、实现（internal/agentmesh/ 新包）

| 文件 | 内容 | Phase |
|---|---|---|
| message.go | 全部核心类型（AgentCard/DelegatePayload/DelegateResult/DelegateError/SessionLink） | 1 |
| registry.go | 静态注册 + Upsert + 去重 + 冲突 tie-breaker | 1 |
| router.go | 能力匹配 + 评分排序 + 负载过滤 + fallback | 1 |
| config.go | AgentConfig + 从 dot-notation `agents.` 段加载 + TLSConfig | 1/6 |
| a2a_client.go | HTTP JSON-RPC 客户端 + 版本协商 + discover/ping/send/cancel/get/tools | 1/4 |
| rate_limiter.go | 发送方 token bucket（stdlib） | 1 |
| circuit_breaker.go | CLOSED/OPEN/HALF_OPEN 状态机 | 1 |
| agentmesh.go | Delegate 主流程：route→限流→熔断→send→retry→fallback | 1 |
| delegate_tool.go + context.go | `delegate_to_agent` 内置工具 | 1 |
| task_manager.go | async 委托跟踪 + GetResult + Cancel | 2 |
| spawner.go + spawn_tool.go | 动态子进程 spawn + `spawn_agent` 工具 | 2 |
| tracing.go | OTel span + propagator 注入/提取 | 2 |
| server_handlers.go | server 端 tasks/cancel/get + tools/list/call extHandler | 2/4 |
| workflow_adapter.go | `*AgentMesh` → workflow.Delegator 适配 | 3 |
| lifecycle.go | LifecycleManager 心跳 + 断连/重连 + 优雅关闭 | 4 |
| server_rate_limiter.go | 接收方三层限流 | 4 |
| tool_mount.go | ToolMount 远程工具挂载 | 4 |
| gossip.go | UDP Gossip 发现 | 5 |
| metrics.go | Prometheus metrics | 6 |

### 对现有包的修改

- `internal/transport/a2a/a2a.go`：加 `ExtHandler`/`SSEHandler` 注入点 + `agents/discover`/`agents/ping`/`tasks/sendSubscribe`（SSE 降级）+ AgentCard 扩展字段
- `internal/workflow/types.go`：StepSpec 加 `Agent` 字段
- `internal/workflow/engine.go`：加 `Delegator` 接口 + `SetDelegator`
- `internal/workflow/executor.go`：`executeStep` 检测 `step.Agent` 走委托 + `delegateStep`

## 三、测试

| 文件 | 覆盖 |
|---|---|
| registry_test.go | 静态注册/去重/冲突 tie-breaker（狼人杀 + 通用） |
| router_test.go | 能力匹配/排序/忙过滤/preferred not found（狼人杀 + 通用） |
| message_test.go | v1 payload 向后兼容/错误字符串/可重试判定/结果往返 |
| rate_limiter_test.go | 突发/回补/per-agent 隔离 |
| circuit_breaker_test.go | 熔断/半开/恢复/重开 |
| delegate_test.go | sync 成功/超时/熔断/fallback/深度/disabled/bad-payload |
| phase2_test.go | async/cancel/SSE/ToolMount/ServerRateLimiter/metrics |
| gossip_test.go | 双向发现/bye 注销 |
| workflow_agent_test.go | agent 步骤委托/禁用降级 |

**结果**：`go build ./...` 无回归，`go vet` 干净，三包测试全绿。

## 四、Phase 对照

| Phase | 内容 | 状态 |
|---|---|---|
| 1 | message + registry + router + a2a_client + delegate_tool + sync Delegate + 发送方限流 | ✅ |
| 2 | async + cancel + SSE + spawn_agent + 分布式追踪 + 版本协商 | ✅ |
| 3 | workflow StepSpec.agent 集成 | ✅ |
| 4 | LifecycleManager + 接收方限流 + 工具联邦 | ✅ |
| 5 | UDP Gossip 发现 | ✅ |
| 6 | Prometheus metrics + mTLS | ✅ |

未实现（设计中标注的后续）：NAT 穿透/WebSocket 远程通道、负载均衡、Prometheus 告警规则、in-process agent 优化。
