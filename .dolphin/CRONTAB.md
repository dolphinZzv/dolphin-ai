# CRONTAB.md — Scheduled tasks
# Each entry: YAML frontmatter between --- delimiters, followed by task instructions.
# Fields: name, schedule (5-field cron), description, enabled (true/false).

---
description: 每天早上9点执行 date 命令
enabled: true
name: morning_date
schedule: 0 9 * * *
---
执行 date 命令并输出当前时间

