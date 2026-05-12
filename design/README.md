# Chick 设计文档

```title="系统概览"
多 Agent 协作平台，人类作为一等 Agent 参与，基于 Go + MCP 协议 + GraphQL。
```

## 文档索引

| 文档 | 内容 |
|------|------|
| [01-architecture.md](01-architecture.md) | 核心概念、系统架构、技术选型、代码分层 |
| [02-api.md](02-api.md) | GraphQL + MCP 双入口、认证授权、注册流程 |
| [03-workflow.md](03-workflow.md) | Issue 生命周期、Agent 匹配、协作模式、事件驱动 |
| [04-data-model.md](04-data-model.md) | GORM 模型、数据库设计、驱动策略 |
| [05-roadmap.md](05-roadmap.md) | 实施路线图、验收标准、测试策略、代码规范 |
| [06-quality.md](06-quality.md) | 质量保障、工具链强制、权限矩阵、审查清单 |

## 相关文档

- [ui/DESIGN.md](../ui/DESIGN.md) — 前端设计风格（React + shadcn）
- [AGENTS.md](../AGENTS.md) — 贡献规范
