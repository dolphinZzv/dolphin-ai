# Resource Monitor Module

## Context

Monitors system resources (CPU, memory, disk, network bandwidth) and emits events when usage crosses predefined thresholds. Enables the agent and plugins to react to resource pressure or recovery.

## Design

### Threshold System

By default, four thresholds at 20%, 40%, 60%, 80% of usage. Thresholds are fully configurable via `resource.thresholds` — any sorted list of percentages works.

Each resource maintains a "bracket" index for the current threshold bucket:

| Usage Range (defaults) | Bracket |
|------------------------|---------|
| < 20%                  | -1      |
| 20%–39%                | 0       |
| 40%–59%                | 1       |
| 60%–79%                | 2       |
| ≥ 80%                  | 3       |

When the bracket changes between samples, an event is emitted with the crossed threshold and direction (up/down). This avoids repeated events when usage oscillates within the same bucket.

### Sampling

```
Monitor.Start(ctx)
  └─ ticker (default 30s)
       └─ sampleAndCheck(ctx)
            ├─ CPU:     /proc/stat     (delta idle/total)
            ├─ Memory:  /proc/meminfo  (MemTotal - MemAvailable)
            ├─ Disk:    statfs(2)      (total - avail)
            └─ Network: /proc/net/dev  (delta rx/tx bytes → rate)
```

- CPU and Network use delta between consecutive samples (rate calculation).
- Memory and Disk are point-in-time snapshots.
- Network percentage = max(rx_rate, tx_rate) / configured `max_bandwidth`.
- Disk monitors each path in `disk_paths` (default: `["/"]`).

### Event Emission

Each crossing emits an `event.Event`:
```go
{
  Type: "resource:cpu" | "resource:memory" | "resource:disk" | "resource:network",
  Data: {
    "resource":   "...",
    "threshold":  20|40|60|80,
    "direction":  "up"|"down",
    "current":    42.5,
    "path":       "/data"           // only for disk
  }
}
```

### Actor Integration

The monitor runs as an `oklog/run` actor in the startup group, started when `resource.enabled = true`. It gracefully terminates when the context is cancelled.

### Config (in `config.yaml`)

```yaml
resource:
  enabled: false
  interval: "30s"
  disk_paths:
    - "/"
  max_bandwidth: 125000000  # 1 Gbps in bytes/sec
  thresholds: [20, 40, 60, 80]  # sorted ascending; empty = defaults
```

### Platform Support

- Linux: `/proc`-based sampler (full support).
- Other platforms: stub that returns an error on every sample. The monitor logs at debug level and continues; no events are emitted.

## Data Flow

```
/proc ─→ Sampler ──→ threshold Bracket ──→ EventBus ──→ plugins / webhook
                              ↑
                      bracketIndex() logic
```

## Testing

- Unit tests use a mock sampler to verify threshold detection in both directions.
- Edge cases: zero total (no-op), network wrap-around (clamp to 0), missing /proc files (logged, no crash).

<!-- last-modified: 2026-05-18 -->
