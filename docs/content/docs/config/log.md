---
title: 日志配置
weight: 3
---

```yaml
log:
  level: info       # debug | info | warn | error
  file: .dolphin/dolphin.log
```

`level` 控制日志输出粒度，`file` 指定日志文件路径。不配置时默认输出到标准错误。
