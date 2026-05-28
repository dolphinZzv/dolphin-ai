# Dynamic Config Reload

## Motivation

Currently `config.Load()` runs exactly once at startup. All components hold a sub-pointer into the single `*config.Config` struct (e.g. `&cfg.Transport.SSH`). The only way to pick up config file changes is a full process restart. This document proposes a reload system that:

- Watches config files for changes and reloads automatically
- Propagates relevant changes to running components without restart
- Preserves backward compatibility for the existing in-memory MCP config tool

## Current Architecture

```
config.Load() → *config.Config (single shared pointer)
    ├── cmd/root.go stores *config.Config
    ├── transports store sub-pointers (e.g. &cfg.Transport.SSH)
    ├── MCP tools store sub-pointers (e.g. &cfg.MCP.Shell)
    └── agent.Agent stores *config.Config
```

Key observations:

1. **Single shared pointer**: Mutations to the struct are visible to all sub-pointer holders immediately, but a full pointer swap (reload from disk) invalidates every sub-pointer.
2. **No notification mechanism**: When the MCP config tool mutates a field (e.g. `log.level`), there is no propagation — consumers simply read the value next time they check.
3. **No file watcher**: No `fsnotify`, no inotify, no polling. Config is read once.
4. **Rigid actor group**: `run.Group` is assembled once in `runActorGroup()`. Actors cannot be added or removed at runtime.

## Design

### Layered Model

```
┌──────────────────────────────────────────────┐
│                File Watcher                   │
│  (fsnotify on config.yaml + included files)   │
└──────────────────┬───────────────────────────┘
                   │ file changed
┌──────────────────▼───────────────────────────┐
│             Config Reloader                   │
│  1. Load new config (parallel namespace)      │
│  2. Diff old vs new                           │
│  3. Emit typed change events                  │
└──────────────────┬───────────────────────────┘
                   │ events
┌──────────────────▼───────────────────────────┐
│           Change Event Bus                    │
│  Subscribers: transport, MCP, agent, ...      │
└──────────────────┬───────────────────────────┘
                   │ handle
┌──────────────────▼───────────────────────────┐
│           Component Handlers                  │
│  Each component decides what to reload:       │
│  • transport.SSH → rotate listener           │
│  • mcp.shell → update timeouts               │
│  • session → in-place (already works)        │
└──────────────────────────────────────────────┘
```

### Phase 1 — Config Manager (`internal/config/manager.go`)

A new `Manager` struct that wraps `*Config` and provides:

```go
type Manager struct {
    mu     sync.RWMutex
    config *Config
    enabled bool          // false = skip file watcher / remote sources
    watcher *watcher.FileWatcher
    subscribers []Subscriber
}

type EventType int
const (
    EventTransportSSH  EventType = iota
    EventTransportMQTT
    EventTransportEmail
    EventTransportDingTalk
    EventMCPTool
    EventSession
    EventLLM
    EventLog
    // ...
)

type Event struct {
    Type    EventType
    Old, New *Config   // snapshot for diff
}

type Subscriber interface {
    OnConfigChange(Event)
}
```

**Loading**: `Manager.Load(path)` calls `loadConfig(path)` into a temporary `*Config`, validates, applies post-processing, acquires write lock, swaps the pointer, releases lock, then emits events for the diff.

```go
func (m *Manager) Load(path string, opts ...LoadOption) error {
    newCfg, err := loadConfig(path)  // fresh load (viper + unmarshal)
    if err != nil {
        return err
    }
    // Post-processing (must replicate config.Load() logic)
    applyEnvOverrides(newCfg)       // DZ_LLM_API_KEY, DZ_MQTT_BROKER, ...
    resolveWorkspace(newCfg)        // filepath.Abs()
    wireMQTTBroker(newCfg)          // Servers.MQTTBroker → Transport.MQTT
    ensureSSHPassword(newCfg)       // auto-generate if missing
    if err := newCfg.Validate(); err != nil {
        return err
    }
    // fillConfigDefaults only on first load, NOT on file-watch reloads
    if !m.loaded {
        syncConfigDefaults(newCfg)
        m.loaded = true
    }
    m.mu.Lock()
    oldCfg := m.config
    m.config = newCfg
    m.mu.Unlock()
    if oldCfg != nil {
        m.emitEvents(oldCfg, newCfg)
    }
    return nil
}
```

**Key post-processing that `Manager.Load()` must replicate** (from the existing `config.Load()`):

| Post-Processing | Detail | Re-run on reload? |
|:---|---:|:---:|
| Env var overrides (DZ_LLM_*, DZ_MQTT_*, DZ_EMAIL_*, etc.) | ~30 env vars override config fields | **Yes** — env may change between reloads |
| Workspace absolute path | `filepath.Abs(cfg.Workspace)` | **Yes** |
| MQTT broker auto-wiring | Auto-fill `Transport.MQTT.Broker/Username/Password` from `Servers.MQTTBroker` | **Yes** |
| MQTT broker account auto-gen | Random account if none configured | **No** (use existing if present) |
| SSH password auto-gen | Read or generate `~/.dolphin/ssh_password` | **Yes** (read existing, don't regenerate) |
| `fillConfigDefaults` / `sync_config` | Write missing defaults to config.yaml | **No** — first load only |
| `Validate()` | Full config validation | **Yes** — reject invalid reloads |

**`sync_config` on first load only**: `fillConfigDefaults()` writes to the config file on disk. If triggered on a file-watch reload, it would create a write→watch→reload loop. The `syncConfigDefaults()` call must be gated by `m.loaded`.

**MQTT broker mutation in `runActorGroup`**: Currently `cmd/root.go:660` mutates `cfg.Transport.MQTT.Broker` in-place after load to point at the embedded broker. In the Manager pattern, this must change — Manager holds the authoritative config, and the broker address wiring happens inside `Manager.Load()` itself (see `wireMQTTBroker` above). Actors read from `Manager.Get()` instead of mutating the shared config.

**Read access**: All existing `*config.Config` consumers switch to `Manager.Get()`:

```go
func (m *Manager) Get() *Config {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.config
}
```

This is safe for atomic pointer swap, but sub-pointers obtained before the swap remain valid (they point to old struct). Consumers that hold sub-pointers must re-acquire them after a reload event.

**Watch gating**: `Manager.Watch()` respects `dynamic_config.enabled`:

```go
func (m *Manager) Watch(ctx context.Context) error {
    if !m.enabled {
        zap.S().Infow("dynamic config disabled, skipping file watcher")
        <-ctx.Done()
        return nil
    }
    // start all configured sources...
}
```

This preserves the `/reload` CLI command and SIGHUP even when auto-watch is disabled — they call `Load()` directly.

### Phase 2 — Config Sources (File + Remote)

Introduce a `Source` abstraction so the Manager can load config from local files AND remote URLs:

```go
// Source is a config source that can be read and watched for changes.
type Source interface {
    Name() string
    Read() ([]byte, error)
    Watch(ctx context.Context, onChange func()) error
    Close() error
}
```

**FileSource** — wraps the existing fsnotify logic:

```go
type FileSource struct {
    path    string
    watcher *fsnotify.Watcher
}

func (s *FileSource) Read() ([]byte, error) { return os.ReadFile(s.path) }
func (s *FileSource) Watch(ctx context.Context, onChange func()) error {
    // fsnotify on s.path + all !include resolved paths
    // coalesce multiple rapid events (debounce 200ms)
    // fallback: mtime poll every 30s (handles vim backup-save pattern)
}
```

**RemoteSource** — periodic polling for HTTP/HTTPS config:

```go
type RemoteSource struct {
    url      string
    interval time.Duration  // poll interval, default 60s
    client   *http.Client
    etag     string         // If-None-Match for 304 optimization
    modTime  string         // Last-Modified
}

func (s *RemoteSource) Read() ([]byte, error) {
    resp, err := s.client.Get(s.url)
    // ...
}
func (s *RemoteSource) Watch(ctx context.Context, onChange func()) error {
    ticker := time.NewTicker(s.interval)
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            data, err := s.Read()
            if err != nil { /* log, retry */ }
            onChange()  // trigger Manager reload
        }
    }
}
```

**Master switch**: A top-level `dynamic_config.enabled` field controls whether the watcher starts at all. When disabled, `Manager.Get()` still provides the latest loaded config (for the `/reload` CLI command and SIGHUP), but auto-reload from file changes is skipped.

```yaml
dynamic_config:
  enabled: true        # default: true, set false to disable auto-reload
```

**Status quo First** (no remote config required by default): The initial implementation uses `FileSource` only. `RemoteSource` is designed but gated behind a `remote_config` YAML block. Users who don't set it get the same fsnotify behavior as before.

**Configuration** (optional, in config.yaml):

```yaml
# Default: local files only (backward compatible)
config_sources:
  - type: file
    path: .dolphin/config.yaml
  - type: remote
    url: https://config-server.example.com/dolphin.yaml
    poll_interval: 60s
    timeout: 10s
```

**Merge order**: Sources are read in list order and merged via viper (same layering as current file loading). Later sources override earlier ones.

**Watch flow**:
```
FileSource(fsnotify) ─┐
                       ├── debounce 200ms ──→ Manager.Load() ──→ diff ──→ emit events
RemoteSource(poll 60s) ┘
```

**Debouncing**: Local file changes are debounced at 200ms. Remote polls are inherently periodic (no debounce needed). If both fire simultaneously, the Manager serializes via its write lock.

### Phase 3 — Change Event Bus

The event bus in `internal/event` already exists. Extend it with config-specific topics:

```go
const (
    TopicConfigChanged event.Topic = "config.changed"
)
```

Events carry typed payloads. Each subscriber receives the full `Event{Type, Old, New}` and decides what to do.

### Phase 4 — Transport Hot-Reload Strategies

Each transport **must** support hot config change. This is a core requirement, not optional. A `ConfigReloader` interface is added to the transport package:

```go
type ConfigReloader interface {
    OnConfigChange(oldCfg, newCfg *config.Config) error
    // Returns ErrUnchanged if no relevant fields changed (skips actor restart).
    // Returns ErrRequiresRestart if the transport cannot hot-reload in-place
    //   and needs the ActorGroup to stop and restart the actor.
}

var ErrUnchanged = errors.New("config unchanged, no action needed")
var ErrRequiresRestart = errors.New("transport must be restarted")
```

`OnConfigChange` is called **before** the old actor is stopped. The transport can:
- Update fields in-place (lock + swap)
- Return `ErrRequiresRestart` to signal the ActorGroup should stop the old actor and start a new one with the updated config

#### SSH

On config change, SSH can hot-reload entirely in-place:

1. Build a new `gossh.ServerConfig` from the new `SSHConfig` — all closures (`PasswordCallback`, `PublicKeyCallback`) capture fresh config values
2. If `Addr` changed, start a new listener, then close the old one (drain)
3. Swap `gossh.ServerConfig` pointer — new connections use new auth rules
4. Old connections continue with the auth rules from when they connected (standard SSH SIGHUP semantics)

```go
func (t *SSHTransport) OnConfigChange(oldCfg, newCfg *config.Config) error {
    oldSSH := oldCfg.Transport.SSH
    newSSH := newCfg.Transport.SSH

    if oldSSH.Addr == newSSH.Addr && oldSSH.Password == newSSH.Password &&
        oldSSH.HostKey == newSSH.HostKey && eqStringSlice(oldSSH.AllowedUsers, newSSH.AllowedUsers) &&
        oldSSH.AuthorizedKeys == newSSH.AuthorizedKeys {
        return ErrUnchanged  // nothing relevant changed
    }

    // Rebuild gossh.ServerConfig with fresh closures
    newServerCfg := buildServerConfig(newSSH)
    t.swapServerConfig(newServerCfg)

    if oldSSH.Addr != newSSH.Addr {
        // Start new listener, swap, close old
        newListener, err := net.Listen("tcp", newSSH.Addr)
        if err != nil {
            return fmt.Errorf("ssh reload listen: %w", err)
        }
        oldListener := t.swapListener(newListener)
        go oldListener.Close()  // drain
    }
    return nil  // in-place, no restart needed
}
```

#### MQTT

MQTT transport maintains a long-lived `mqtt.Client` connection. Most config changes require a reconnect:

- **Broker / credentials / keep-alive changed**: Disconnect old client, create new `mqtt.ClientOptions`, connect, subscribe
- **Topic changed**: Unsubscribe old topic, subscribe new topic on existing connection
- **Publish topic changed**: Update `respTopic` field only (no reconnect)

```go
func (t *MQTTTransport) OnConfigChange(oldCfg, newCfg *config.Config) error {
    oldM := oldCfg.Transport.MQTT
    newM := newCfg.Transport.MQTT

    if oldM.PublishTopic != newM.PublishTopic {
        t.setRespTopic(newM.PublishTopic)  // in-place, no reconnect
    }
    if oldM.Broker == newM.Broker && oldM.SubscribeTopic == newM.SubscribeTopic &&
        oldM.Username == newM.Username && oldM.Password == newM.Password &&
        oldM.KeepAliveSeconds == newM.KeepAliveSeconds {
        return nil // only publish topic changed, already handled above
    }
    // Broker/credentials/topic changed — full reconnect
    return ErrRequiresRestart  // ActorGroup stops and restarts this actor
}
```

On restart, `Start()` connects with the new config. The old connection's `Close()` is called during actor cleanup.

#### Email

Email transport reconnects on every poll cycle, making hot-reload simpler:

- **SMTP/IMAP host/port/credentials changed**: Next poll/disconnect/reconnect uses the new config automatically (all reads go through `t.cfg`)
- **Poll interval changed**: Recreate ticker
- **AllowedSenders changed**: Already supported via `allowedSendersOv` override
- No connection state to manage — `pollOnce()` always opens a fresh IMAP connection

```go
func (t *EmailTransport) OnConfigChange(oldCfg, newCfg *config.Config) error {
    oldE := oldCfg.Transport.Email
    newE := newCfg.Transport.Email

    t.cfg = &newE  // swap pointer — all methods read from t.cfg

    if oldE.PollInterval != newE.PollInterval {
        t.restartTicker(newE.PollInterval)
    }
    return nil  // in-place, no restart needed
}
```

**Exception**: If SMTP `From` changes mid-conversation, the next `sendMail()` call uses the new address. No restart needed.

#### DingTalk

DingTalk uses the official Stream SDK which creates a long-lived WebSocket connection:

- **ClientID / ClientSecret changed**: The SDK's `cred` is set at `Start()` time. Changing credentials requires disconnecting the old stream client and creating a new one.
- **No other config fields affect the transport** (ReadTimeout is handled at ReadLine level, not SDK level)

```go
func (t *DingTalkTransport) OnConfigChange(oldCfg, newCfg *config.Config) error {
    oldD := oldCfg.Transport.DingTalk
    newD := newCfg.Transport.DingTalk

    if oldD.ClientID == newD.ClientID && oldD.ClientSecret == newD.ClientSecret {
        return ErrUnchanged
    }
    return ErrRequiresRestart  // must disconnect SDK and reconnect with new creds
}
```

On restart, `Start()` creates a new SDK client with the updated credentials.

#### A2A

A2A runs an HTTP server. Config changes that affect the listener or auth require a server restart:

- **ListenAddr changed**: Close old HTTP server, start new on new address
- **TLS cert/key changed**: Swap TLS config, restart server
- **APIKey changed**: `authMiddleware` captures `t.cfg.APIKey` at middleware creation time. Need to rebuild middleware.
- **HandlerPath / AgentCardPath changed**: Re-register HTTP routes
- **Agent identity fields** (Name, Description, Version): Only affect agent card JSON, can be hot-read on each request

```go
func (t *A2ATransport) OnConfigChange(oldCfg, newCfg *config.Config) error {
    oldA := oldCfg.Transport.A2A
    newA := newCfg.Transport.A2A

    // Identity fields are read from t.cfg at request time — hot swap is safe
    t.cfg = &newA

    if oldA.ListenAddr != newA.ListenAddr || oldA.APIKey != newA.APIKey ||
        oldA.TLSEnabled != newA.TLSEnabled || oldA.TLSCertFile != newA.TLSCertFile ||
        oldA.TLSKeyFile != newA.TLSKeyFile || oldA.HandlerPath != newA.HandlerPath {
        return ErrRequiresRestart  // server must be rebuilt
    }
    return nil  // identity fields only
}
```

On restart, `Start()` creates a fresh `http.Server` with new routes and middleware. The old server is gracefully shut down via `Shutdown()`.

#### MCP Tools

#### MCP Tools with Sub-Pointers

For MCP tools that hold a config sub-pointer (Shell, CDP, WebHost, WebSearch, Webhook, A2A, Email), the approach is to re-point on config change:

```go
// Simple re-point: most MCP tools
func (s *ShellTool) OnConfigChange(oldCfg, newCfg *config.Config) error {
    s.cfg = &newCfg.MCP.Shell  // re-point to new struct
    return nil
}
```

**Special cases that need more than re-pointing:**

**MCP Webhook**: Also holds `http.Client{Timeout: ...}` created from config at `New()` time. If `TimeoutSeconds` changes, the client must be recreated:

```go
func (w *WebhookTool) OnConfigChange(oldCfg, newCfg *config.Config) error {
    w.cfg = &newCfg.MCP.Webhook
    w.client = &http.Client{Timeout: webhookTimeout(w.cfg)}
    return nil
}
```

**MCP A2A Tool + ListTool**: Both hold `&cfg.MCP.A2A` sub-pointer. The `Tool` also has `http.Client` built from config timeout — same recreation logic as webhook. The `ListTool` only reads `cfg.Agents` and can just re-point.

**MCP Email Tool**: Holds `*config.Config` directly (not a sub-pointer). After reload, this pointer is stale:

```go
func (e *EmailMCPTool) OnConfigChange(oldCfg, newCfg *config.Config) error {
    e.cfg = newCfg  // re-point full config
    return nil
}
```

**MCP LLM Tool**: Does NOT hold a sub-pointer — it snapshots `cfg.LLM.EffectiveProviders()` into a `[]config.ProviderConfig` slice at `New()` time. After reload, the snapshot is stale and must be rebuilt:

```go
func (l *LLMTool) OnConfigChange(oldCfg, newCfg *config.Config) error {
    providers := newCfg.LLM.EffectiveProviders()
    l.providers = providers
    if len(providers) > 0 {
        l.defaultProv = providers[0].Name
        l.defaultModel = providers[0].Model
    }
    return nil
}
```

**MCP Registry**: Holds `&cfg.MCP` for tool filtering and server management. If the `filter` or external server list changes, the registry needs to re-sync:

```go
func (r *Registry) OnConfigChange(oldCfg, newCfg *config.Config) error {
    r.cfg = &newCfg.MCP
    r.syncServerTools(newCfg)  // add/remove external MCP server clients
    return nil
}
```

Building new `ServerClient` instances for external MCP servers requires creating sub-processes (stdio) or HTTP clients (SSE). This is handled similarly to transport actors — the old server client gets a shutdown signal, the new one starts.

#### Agent (Loop)

`agent.Agent` holds `cfg *config.Config` for: provider selection, compressor config, workspace path, limits, session config. After reload, it needs:

1. Re-point `a.cfg` to new config
2. If LLM providers changed → rebuild FailoverProvider (re-run health checks on new providers)
3. If compress mode/timeout changed → rebuild compressor
4. If limits changed → rebuild LimitsManager

```go
func (a *Agent) OnConfigChange(oldCfg, newCfg *config.Config) error {
    a.cfg = newCfg

    if !reflect.DeepEqual(oldCfg.LLM.Providers, newCfg.LLM.Providers) ||
        oldCfg.LLM.Type != newCfg.LLM.Type ||
        oldCfg.LLM.BaseURL != newCfg.LLM.BaseURL {
        a.provider = selectProvider(newCfg)  // re-run health checks
    }
    if oldCfg.LLM.CompressMode != newCfg.LLM.CompressMode ||
        oldCfg.LLM.CompressTimeoutSeconds != newCfg.LLM.CompressTimeoutSeconds {
        a.rebuildCompressor()
    }
    if oldCfg.LLM.Limits.Enabled != newCfg.LLM.Limits.Enabled {
        if newCfg.LLM.Limits.Enabled {
            a.limitsManager = limits.NewLimitsManager(&newCfg.LLM.Limits)
        } else {
            a.limitsManager = nil
        }
    }
    return nil
}
```

#### Context Builder / RenderData

`RenderData` snapshots config to a `map[string]any` at creation time via `mapstructure`. After reload, the context builder should detect the config change and rebuild `RenderData` so templates reflect current config values:

```go
// Inside ctxpkg.Builder, after a config change event:
func (b *Builder) OnConfigChange(newCfg *config.Config) {
    b.renderData = NewRenderData(newCfg)
}
```

#### Buildin Agent Handle

`AgentHandle.Cfg` is passed to built-in agents that may read config. After reload, re-point:

```go
func (h *AgentHandle) OnConfigChange(newCfg *config.Config) {
    h.Cfg = newCfg
}
```

#### Stdio Transport

Stdio transport does NOT store any config pointer. Its `New()` reads markdown render settings at startup to create `glamour.TermRenderer`, but the struct has no `cfg` field. If markdown settings change at runtime, stdio would need a new renderer. This is a low-priority edge case since stdio is typically used for interactive CLI sessions where restarting the process is trivial.

#### EventBus

`EventBus` stores webhook delivery config as state fields (`webhookURL`, `webhookEvents`, `webhookClient`) set via `SetWebhook()` in `cmd/root.go`. After a config reload, if `cfg.Plugins.WebhookURL` or `cfg.Plugins.WebhookEvents` changed, the bus won't see the changes:

```go
func (b *EventBus) OnConfigChange(oldCfg, newCfg *config.Config) error {
    if oldCfg.Plugins.WebhookURL == newCfg.Plugins.WebhookURL &&
        eqStringSlice(oldCfg.Plugins.WebhookEvents, newCfg.Plugins.WebhookEvents) {
        return ErrUnchanged
    }
    eventTypes := make([]event.Type, len(newCfg.Plugins.WebhookEvents))
    for i, et := range newCfg.Plugins.WebhookEvents {
        eventTypes[i] = event.Type(et)
    }
    if len(eventTypes) == 0 {
        eventTypes = []event.Type{"*"}
    }
    b.SetWebhook(newCfg.Plugins.WebhookURL, eventTypes)
    return nil
}
```

#### MQTT Broker Server

The in-process MQTT broker (`server/mqtt/broker.go`) holds `config.MQTTBrokerConfig` by value. After reload, address or account changes don't take effect:

```go
func (b *Broker) OnConfigChange(oldCfg, newCfg *config.Config) error {
    oldS := oldCfg.Servers.MQTTBroker
    newS := newCfg.Servers.MQTTBroker

    if oldS.Addr == newS.Addr && eqAccounts(oldS.Accounts, newS.Accounts) {
        return ErrUnchanged
    }
    // Close server, rebuild auth ledger, rebind listener
    b.server.Close()
    b.cfg = newS
    return b.Start()  // re-initialize with new config
}
```

Since the broker is started as an actor in `ActorGroup`, `ErrRequiresRestart` can also be used — the ActorGroup stops the old broker actor and starts a new one.

#### Cron Manager

`CronManager` holds `config.CrontabConfig` by value. The `CheckInterval` and `File` path are set at `NewManager()` and don't change after reload. This is low-impact since CRONTAB.md is re-read on each tick regardless. The fix is simple:

```go
func (m *Manager) OnConfigChange(newCfg *config.CrontabConfig) {
    m.cfg = *newCfg  // copy new config
    m.filePath = newCfg.File
    m.checkInterval = parseDurationOpt(newCfg.CheckInterval, 30*time.Second)
}
```

#### Agent Pool

`AgentPool` stores its own `PoolConfig` (a flat struct with parsed `time.Duration` fields) created once from `NewPoolConfigFromConfig(cfg.Pool)`. After reload, pool parameters are stale:

```go
func (p *AgentPool) OnConfigChange(oldCfg, newCfg *config.Config) error {
    newPoolCfg := NewPoolConfigFromConfig(newCfg.Pool)
    oldPoolCfg := NewPoolConfigFromConfig(oldCfg.Pool)

    if oldPoolCfg == newPoolCfg {
        return ErrUnchanged
    }
    p.cfg = newPoolCfg
    // IdleTimeout, ReapInterval, etc. take effect on next reap cycle
    // MaxConcurrency, DispatchTimeout take effect on next dispatch
    return nil
}
```

#### Resource Monitor

`Monitor` holds `resource.Config` by value (created from `config.ResourceConfig` via `ConfigFrom()` at startup). After reload, `Interval`, `DiskPaths`, `Thresholds`, and `MaxBandwidth` are stale:

```go
func (m *Monitor) OnConfigChange(oldCfg, newCfg *config.Config) error {
    oldR := ConfigFrom(oldCfg.Resource)
    newR := ConfigFrom(newCfg.Resource)

    if oldR == newR {
        return ErrUnchanged
    }
    m.cfg = newR
    m.maxBandwidth = newR.MaxBandwidth
    // Note: ticker interval change requires the ActorGroup to Stop/Start the monitor actor
    return nil
}
```

If `Interval` changes, the monitor needs a restart (the ticker is created inside `Start()` and cannot be hot-swapped). Return `ErrRequiresRestart` for interval changes.

#### Diary

`Diary` holds a `diary.Config` value copy created inline from `cfg.Diary.*` in the actor group setup. After reload, `Dir`, `MaxDaySessions`, `MaxWeekDays`, `MaxMonthWeeks`, `MaxYearMonths`, and `MaxTotalMB` are stale:

```go
func (d *Diary) OnConfigChange(newCfg *config.Config) {
    dc := newCfg.Diary
    d.cfg.Dir = dc.Dir
    d.cfg.MaxDaySessions = dc.MaxDaySessions
    d.cfg.MaxWeekDays = dc.MaxWeekDays
    d.cfg.MaxMonthWeeks = dc.MaxMonthWeeks
    d.cfg.MaxYearMonths = dc.MaxYearMonths
    d.cfg.MaxTotalMB = dc.MaxTotalMB
}
```

This is low-impact since diary only syncs once daily at 8pm, and the config values (limits, paths) are sanity checks rather than critical runtime state.

#### Log Level

`logger.Init()` creates `zapcore.NewCore` with a static `zapcore.Level` — no `AtomicLevel`. After reload, `log.level` changes have no effect on the running logger. A lightweight `syncLogLevel` utility is needed:

```go
// logger package
var atomicLevel zap.AtomicLevel  // added to package state

func Init(cfg Config) {
    atomicLevel = zap.NewAtomicLevelAt(parseLevel(cfg.Level))
    // use atomicLevel in NewCore instead of raw level
}

func SetLevel(s string) {
    atomicLevel.SetLevel(parseLevel(s))
}
```

In `Manager.Load()`, after a detected change:

```go
// Inside OnConfigChange:
if oldCfg.Log.Level != newCfg.Log.Level {
    logger.SetLevel(newCfg.Log.Level)
}
```

In `runActorGroup()`, the `newCoordinator` closure captures the `cfg` parameter:

```go
newCoordinator := func() *agent.Coordinator {
    agt := agent.New(cfg, sessMgr, toolRegistry)
    // ...
}
```

When the outer scope migrates from `*config.Config` to `*config.Manager`, this closure must read from `Manager.Get()` so each newly created coordinator starts with the latest config:

```go
newCoordinator := func() *agent.Coordinator {
    cfg := mgr.Get()  // read latest snapshot
    agt := agent.New(cfg, sessMgr, toolRegistry)
    // ...
}
```

#### Read-Dynamic Fields (no handler needed)

The following config sections are read at the point of use and don't cache pointers — they naturally pick up changes through `Manager.Get()`:

- **`session.MaxLoop`, `session.MaxSizeMB`**: Read each time a session is created
- **`log.*`**: Set once at startup via `logger.Init()` — no `AtomicLevel` support. Requires a dedicated `syncLogLevel()` call on reload
- **`plugin.*`**: Read at plugin invocation time
- **`update.*`**: Read at update check time
- **`skills.*`**, **`workflows.*`**: Read at manager init (filesystem based, not config-hot-reload-sensitive)
- **`diary.*`**, **`metrics.*`**, **`health.*`**, **`pprof.*`**, **`telemetry.*`**: Typically read once at startup for service init; changes take effect on next process restart

### Phase 5 — Dynamic Actor Group

The `run.Group` from `github.com/oklog/run` does not support dynamic add/remove. Replace it with a custom `ActorGroup`:

```go
type ActorGroup struct {
    mu       sync.Mutex
    actors   []Actor
    runCh    chan struct{}
}

type Actor struct {
    Name    string
    Execute func() error
    Interrupt func(error)
}

func (g *ActorGroup) Add(a Actor) { ... }
func (g *ActorGroup) Remove(name string) { ... }
func (g *ActorGroup) Run() error { ... }
```

**On config reload**, when a transport's `enabled` toggles or its address changes:

1. Call `Interrupt` on the old actor
2. Remove from the group
3. Create new transport from updated config
4. Add new actor to the group

This requires that all transport actors support graceful shutdown via their interrupt function, which they already do via `context.WithCancel`.

### Phase 6 — Security & Validation

Config reload must not weaken security:

- **Validation before swap**: `Manager.Load()` validates the new config via `Validate()` before swapping. If validation fails, the reload is rejected and an error is logged.
- **Auth changes**: If `allowed_users` is tightened, existing connections are NOT terminated — they continue with the old permissions. New connections use the new rules. This matches standard SSH behavior (SIGHUP reload).
- **Audit log**: All config reloads are logged at INFO level with old → new diff summary.

### Phase 7 — CLI / MCP Integration

- `/reload` console command triggers `Manager.Load()` immediately
- MCP `config` tool gains a `reload` action that triggers file re-read
- `SIGHUP` signal triggers reload (standard daemon convention)

## Migration Path

### Step 1: Introduce Config Manager
- Create `internal/config/manager.go` with `Manager` struct
- Add `Manager.Get()` for thread-safe read access
- Replace `*config.Config` passing in `cmd/root.go` with `*config.Manager`
- Migrate all `*config.Config` references in the call chain

### Step 2: Add config sources (file + remote)
- Define `Source` interface with `FileSource` and `RemoteSource` structs
- Add `fsnotify` dependency for FileSource
- Wire `Manager.Watch()` to start all configured sources
- Debounce local file changes (200ms), poll remote sources at interval
- Remote config behind `config_sources[].type=remote` — fully backward compatible

### Step 3: Event bus integration
- Define config change events
- Wire existing `event.EventBus` with config topics
- Add `Subscriber` registration to `Manager`

### Step 4: Component handlers
- Implement `OnConfigChange` on each transport
- Implement `OnConfigChange` on MCP tools that cache config
- Test each handler with targeted config mutations

### Step 5: Dynamic actor group
- Replace `run.Group` with `ActorGroup`
- Implement `Add/Remove` for dynamic transport lifecycle
- Wire transport handlers to use actor group

### Step 6: SIGHUP and CLI commands
- Register SIGHUP handler → `Manager.Load()`
- Add `/reload` console command
- Add `reload` action to MCP config tool

## Non-Goals

- Hot-swap of LLM provider credentials (API keys should be env vars, not config)
- Zero-downtime transport restarts for active sessions (existing sessions continue on old config; only new sessions use new config)
- Automatic rollback on failed reload (rejected at validation time, so no rollback needed)
- Distributed config sync (single-process only)

## Risks

| Risk | Mitigation |
|------|-----------|
| Stale sub-pointer after reload | Event-driven re-acquire; compile-time lint for `&cfg.X` patterns |
| Race between reload and active config read | `Manager.Get()` under RLock; swap under WLock |
| Partial reload leaves inconsistent state | All validation happens before swap; swap is atomic |
| fsnotify miss on some editors (vim backup pattern) | Debounce + also fallback to mtime poll every 30s |
| Transport restart drops in-flight connections | Old listener drained; active sessions finish on old connection |
| MQTT reconnect drops queued messages | Drain msg channel before disconnect; publish completion waits for ack |
| DingTalk SDK does not support hot credential swap | `ErrRequiresRestart` → ActorGroup handles stop/start cycle |
| A2A HTTP server restart races with active RPC requests | Graceful `Shutdown()` with connection draining before restart |
| Email poll ticker and sendMail use stale SMTP/IMAP host | Email already reconnects per-poll; swap `t.cfg` pointer atomically |
| SSH `PasswordCallback`/`PublicKeyCallback` closures capture stale values | Rebuild entire `gossh.ServerConfig` on every relevant change |
| MCP LLM snapshots providers at New() — stale after reload | `OnConfigChange` rebuilds provider list; no restart needed |
| MCP webhook/a2a http.Client captures timeout at New() | Recreate client on timeout change; `OnConfigChange` handles this |
| MCP Registry external server list needs add/remove | `syncServerTools` on config change; old server shut down, new one started |
| Agent provider selection re-run may fail (no healthy provider) | Keep old provider if new health checks all fail; log warning |
| Agent compressor rebuild may lose in-flight compression | Compression completes synchronously per turn; rebuild is safe between turns |
| Context RenderData template uses stale config values | Rebuild RenderData on config change event |
| MQTT broker config by value — Addr/Accounts changes ignored | Broker actor restart swaps value copy |
| CronManager `checkInterval` / `filePath` fixed at creation | OnConfigChange copies new CrontabConfig |
| AgentPool PoolConfig snapshot stale after reload | OnConfigChange re-runs NewPoolConfigFromConfig |
| Resource Monitor Config snapshot — Interval/DiskPaths/Thresholds/MaxBandwidth stale | OnConfigChange swaps config; interval change → ActorGroup restart |
| `newCoordinator` closure captures stale `cfg` pointer | Read from `Manager.Get()` inside closure instead |
| EventBus stores webhookURL/webhookEvents/webhookClient as state set once at startup | OnConfigChange re-detects changes and re-calls SetWebhook with new values |
| Health/Metrics/Pprof servers use inline config for Addr | Inline HTTP servers need ActorGroup restart on port change |
| Diary Config by value — Dir/limits stale after reload | OnConfigChange copies new diary config values |
| `Manager.Load()` misses env-var post-processing | `applyEnvOverrides()` called in Manager.Load() |
| `fillConfigDefaults` on reload creates write→watch loop | Skip `syncConfigDefaults` when `m.loaded == true` |
| `runActorGroup` mutates cfg.Transport.MQTT.Broker in-place | Broker wiring moved into `Manager.Load()`; actors read-only via `Manager.Get()` |
| Log level set statically at startup — no AtomicLevel | Add `atomicLevel` to logger package; `syncLogLevel()` called from OnConfigChange |
| SSH password re-generated on every reload | `ensureSSHPassword` reads existing file; only writes if missing |
| Remote config poll misses individual changes between intervals | Poll interval configurable (default 60s); short enough for non-critical config |
| Remote config HTTP timeout / network unavailable | Log warning, keep current config; exponential backoff on errors |
| Remote config returns stale data (no ETag/Last-Modified) | Use ETag + If-None-Match; fallback to body comparison |
| Simultaneous file+remote update triggers double reload | Manager write lock serializes; second load sees no diff → no events emitted |
| Security bypass during reload | Validation gates the swap; audit log tracks every reload |
| ActorGroup stop/start tears down old transport before new one is ready | Start new actor first, then signal old actor to stop (connection handoff) |

<!-- last-modified: 2026-05-27 -->
