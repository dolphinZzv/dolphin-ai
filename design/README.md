# dolphin Architecture Design

dolphin is an AI agent connecting terminal, email, chat, and SSH — providing a unified agent experience across all transport layers.

## Version History

| Version | Focus | Key Additions |
|---------|-------|---------------|
| v0.1 | Single-agent core | Agent Loop, Provider, Transport, MCP, Session |
| v0.2 | Multi-agent | Coordinator, AgentPool, AgentDef, ChannelIO, Registry Clone |
| v0.3 | Observability & Extensions | Compressors, Skill/Command/Scheduler, Event/Hook/Plugin, Metrics/Diary/I18n, ConfigHandler, Webhook/Email MCP tools |

## Document Index

| File | Content |
|------|---------|
| [overview.md](overview.md) | Overall architecture with versioned diagrams |
| [modules/config.md](modules/config.md) | Configuration management |
| [modules/agent-loop.md](modules/agent-loop.md) | Agent Loop, Provider, Context compression |
| [modules/context.md](modules/context.md) | System prompt builder |
| [modules/session.md](modules/session.md) | Session management & summary |
| [modules/mcp.md](modules/mcp.md) | MCP tool system |
| [modules/coordinator.md](modules/coordinator.md) | Multi-agent coordination |
| [modules/scheduler.md](modules/scheduler.md) | Scheduled tasks |
| [modules/skill.md](modules/skill.md) | Skills system |
| [modules/command.md](modules/command.md) | User-defined commands |
| [modules/event-hook.md](modules/event-hook.md) | Event bus & hook system |
| [modules/plugin.md](modules/plugin.md) | Plugin system |
| [modules/diary.md](modules/diary.md) | Session diary/calendar |
| [modules/logger.md](modules/logger.md) | Structured logging |
| [modules/metrics.md](modules/metrics.md) | Prometheus metrics |
| [modules/i18n.md](modules/i18n.md) | Internationalization |
| [decisions.md](decisions.md) | Design decision log |
| [tree.md](tree.md) | Directory structure |
| [gaps.md](gaps.md) | Remaining design gaps |

> Last modified: 2026-05-17
