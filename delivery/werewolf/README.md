# Agent Mesh 交付包

Dolphin 多 agent 协作层（Agent Mesh）的设计修订 + Phase 1–6 实现 + 狼人杀玩法交付。

## 目录内容

| 文件 | 说明 |
|---|---|
| [agent-mesh-werewolf-guide.md](./agent-mesh-werewolf-guide.md) | 狼人杀玩法指南：角色配置、委托、观测、故障排查 |
| [delivery.md](./delivery.md) | 交付清单：实现了什么、文件清单、测试结果、Phase 对照 |
| [play_werewolf.py](./play_werewolf.py) | 全 AI 对局（旁观模式）：随机分配，主持人委托各角色 agent |
| [play_werewolf_human.py](./play_werewolf_human.py) | **真人参与**：你是 1 号玩家，主持人当裁判并扮演其余 8 个 AI 玩家，终端交互 |
| [e2e_werewolf.py](./e2e_werewolf.py) | E2E 自动化测试：断言主持人成功委托 seer/guard/werewolf |

## 设计文档

原始设计在 [`design/modules/agent-mesh.md`](../design/modules/agent-mesh.md)，已修订 7 个设计漏洞（traceparent 规范化、熔断/重试耦合、大文件退路、cancel 基础化、限流自洽、gossip tie-breaker、workflow 迁移点）。

## 代码

实现全部在 [`internal/agentmesh/`](../internal/agentmesh/)（新增包），以及对 [`internal/transport/a2a/`](../internal/transport/a2a/)、[`internal/workflow/`](../internal/workflow/) 的扩展。

## 快速开始

```bash
cd /Users/jzx/Desktop/DolphinzZ
go build -o dolphin ./cmd/dolphin   # 编译（首次或改代码后）

# 真人参与（推荐）：你是 1 号玩家，其余 8 人由 AI 扮演
python3 delivery/play_werewolf_human.py

# 全 AI 对局（旁观）：看 AI 角色互相委托
python3 delivery/play_werewolf.py

# E2E 自动化测试
python3 delivery/e2e_werewolf.py
```

详见 [狼人杀指南](./agent-mesh-werewolf-guide.md)。
