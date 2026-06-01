---
title: 可观测性
weight: 4
---

Dolphin 集成 OpenTelemetry，支持通过标准 OTLP 协议导出链路追踪数据：

```yaml
otel:
  endpoint: http://localhost:4318
```

配置后，Dolphin 会自动将追踪数据发送到指定端点，方便对接 Jaeger、Zipkin 等观测平台。
