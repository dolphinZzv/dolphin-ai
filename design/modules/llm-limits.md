# LLM Usage Limits Module

## Overview

添加 LLM 调用限制配置，防止成本爆炸。支持三个维度的限制：
- **调用次数限制**：每日/每月 API 调用次数
- **Token 使用量限制**：每日/每月 input/output token 总量
- **并发限制**：同时进行的 LLM 调用数量

## Configuration Design

### Config Structure

在 `LLMConfig` 中新增 `Limits` 字段：

```yaml
llm:
  limits:
    enabled: true
    scheduler_enabled: true       # 是否启用 buildin crontab 调度器（默认 true）
    # 调用次数限制
    requests:
      daily_max: 1000              # 每日最大调用次数，0=不限制
      daily_reset_cron: "0 0 * * *" # 每日重置时间
      weekly_max: 5000             # 每周最大调用次数，0=不限制
      weekly_reset_cron: "0 0 * * 1" # 每周重置时间（每周一零点）
      monthly_max: 10000            # 每月最大调用次数，0=不限制
      monthly_reset_cron: "0 0 1 * *" # 每月重置时间
    # Token 使用量限制
    tokens:
      daily_input_max: 1000000      # 每日 input token 限制
      daily_output_max: 500000      # 每日 output token 限制
      daily_reset_cron: "0 0 * * *"
      weekly_input_max: 5000000     # 每周 input token 限制
      weekly_output_max: 2000000   # 每周 output token 限制
      weekly_reset_cron: "0 0 * * 1"
      monthly_input_max: 10000000   # 每月 input token 限制
      monthly_output_max: 5000000   # 每月 output token 限制
      monthly_reset_cron: "0 0 1 * *"
    # 并发限制
    concurrency:
      max_running: 5
    # 强制模式
    enforcement: hard
    # 限流后的重试配置（仅 soft 模式）
    retry:
      max_attempts: 3
      initial_backoff: 1s
      max_backoff: 60s
```

### LimitsConfig Struct

```go
type LimitsConfig struct {
    Enabled         bool              `mapstructure:"enabled"`
    SchedulerEnabled bool             `mapstructure:"scheduler_enabled"` // 是否启用 buildin crontab 调度器
    Requests        MultiLevelLimits  `mapstructure:"requests"`
    Tokens          TokenMultiLimits  `mapstructure:"tokens"`
    Concurrency     ConcurrencyLimit  `mapstructure:"concurrency"`
    Enforcement     string            `mapstructure:"enforcement"` // "hard" or "soft"
    Retry           RetryConfig       `mapstructure:"retry"`
}

// 多层级限制（支持每日、每周、每月等不同周期）
type MultiLevelLimits struct {
    Daily   LevelLimit `mapstructure:"daily"`
    Weekly  LevelLimit `mapstructure:"weekly"`   // 可选
    Monthly LevelLimit `mapstructure:"monthly"`
}

type LevelLimit struct {
    Max       int    `mapstructure:"max"`
    ResetCron string `mapstructure:"reset_cron"` // 标准 crontab 表达式
}

// Token 多层级限制
type TokenMultiLimits struct {
    Daily   TokenLevelLimit `mapstructure:"daily"`
    Weekly  TokenLevelLimit `mapstructure:"weekly"`   // 可选
    Monthly TokenLevelLimit `mapstructure:"monthly"`
}

type TokenLevelLimit struct {
    InputMax   int    `mapstructure:"input_max"`
    OutputMax  int    `mapstructure:"output_max"`
    ResetCron  string `mapstructure:"reset_cron"`
}

type ConcurrencyLimit struct {
    MaxRunning int `mapstructure:"max_running"`
}

type RetryConfig struct {
    MaxAttempts    int           `mapstructure:"max_attempts"`
    InitialBackoff time.Duration `mapstructure:"initial_backoff"`
    MaxBackoff     time.Duration `mapstructure:"max_backoff"`
}
```

### 错误类型定义

```go
// 限制类型细分错误（含上下文）
type LimitError struct {
    Type      string    // "requests", "tokens_input", "tokens_output", "concurrency"
    Level     string    // "daily", "weekly", "monthly"
    Provider  string    // "claude", "deepseek", "" (global)
    Current   int       // 当前使用量
    Max       int       // 限制值
    NextReset time.Time // 下次重置时间
    enforcement string   // "hard" or "soft"
}

func (e *LimitError) Error() string {
    return fmt.Sprintf("llm limit exceeded: %s %s (%d/%d), next reset at %s",
        e.Provider, e.Type, e.Current, e.Max, e.NextReset.Format(time.RFC3339))
}

// 预定义错误变量（兼容 IsLimitError 检查）
var (
    ErrRequestLimitExceeded     = &LimitError{Type: "requests"}
    ErrTokenInputLimitExceeded  = &LimitError{Type: "tokens_input"}
    ErrTokenOutputLimitExceeded = &LimitError{Type: "tokens_output"}
    ErrConcurrencyLimitExceeded = &LimitError{Type: "concurrency"}
    ErrLimitExceededTimeout     = errors.New("llm: limit exceeded, timeout waiting")
)

// IsLimitError 检查是否为限制错误
func IsLimitError(err error) bool {
    var limitErr *LimitError
    return errors.As(err, &limitErr)
}
```

### 豁免机制（Exempt）

某些请求可以绕过限制检查（如系统健康检查、内部探测）：

```go
// ExemptConfig 豁免配置
type ExemptConfig struct {
    Enabled  bool     `mapstructure:"enabled"`
    Patterns []string `mapstructure:"patterns"` // glob 模式列表
}

// 判断请求是否豁免
func IsExempt(req *ProviderRequest, exempt *ExemptConfig) bool {
    if !exempt.Enabled {
        return false
    }
    for _, pattern := range exempt.Patterns {
        if glob.Match(pattern, req.Model) {
            return true
        }
    }
    return false
}

// LimitsManager.Check 中的豁免处理
func (lm *LimitsManager) Check(ctx context.Context, req *ProviderRequest) error {
    if lm.config.Exempt.Enabled && IsExempt(req, &lm.config.Exempt) {
        return nil // 豁免，直接通过
    }
    // ... 正常限制检查
}
```

**配置示例：**
```yaml
llm:
  limits:
    enabled: true
    exempt:
      enabled: true
      patterns:
        - "health-check"      # 健康检查模型
        - "*-internal"        # 内部使用模型
```

### Config 验证

```go
// Validate 验证 LimitsConfig 配置合法性
func (c *LimitsConfig) Validate() error {
    // 检查 enforcement
    if c.Enforcement != "hard" && c.Enforcement != "soft" {
        return fmt.Errorf("enforcement must be 'hard' or 'soft', got '%s'", c.Enforcement)
    }

    // 检查 crontab 表达式格式
    for level, cron := range c.getAllCronExpressions() {
        if cron != "" {
            if _, err := cron.ParseStandard(cron); err != nil {
                return fmt.Errorf("invalid cron expression for %s: %v", level, err)
            }
        }
    }

    // soft 模式需要配置 retry
    if c.Enforcement == "soft" && c.Retry.MaxAttempts == 0 {
        return errors.New("soft enforcement requires retry.max_attempts > 0")
    }

    return nil
}

// getAllCronExpressions 收集所有层级的 crontab 表达式
func (c *LimitsConfig) getAllCronExpressions() map[string]string {
    return map[string]string{
        "requests_daily":    c.Requests.Daily.ResetCron,
        "requests_weekly":   c.Requests.Weekly.ResetCron,
        "requests_monthly":  c.Requests.Monthly.ResetCron,
        "tokens_daily":      c.Tokens.Daily.ResetCron,
        "tokens_weekly":     c.Tokens.Weekly.ResetCron,
        "tokens_monthly":   c.Tokens.Monthly.ResetCron,
    }
}
```

### 多 Provider 独立限制

当配置多个 LLM provider 时，可以选择全局限制或独立限制：

```go
// ProviderLimitsMode 多 provider 限制模式
type ProviderLimitsMode string

const (
    ProviderLimitsGlobal    ProviderLimitsMode = "global"    // 共享全局限制
    ProviderLimitsPerProvider ProviderLimitsMode = "per_provider" // 每个 provider 独立限制
)

type LimitsConfig struct {
    // ... 现有字段

    // 多 provider 限制模式
    ProviderMode ProviderLimitsMode `mapstructure:"provider_mode"`
}

// PerProviderLimits 按 provider 独立的限制配置
type PerProviderLimits struct {
    Claude  LimitsConfig `mapstructure:"claude"`
    Deepseek LimitsConfig `mapstructure:"deepseek"`
}

// 使用示例
if lm.config.ProviderMode == ProviderLimitsPerProvider {
    providerLimits := lm.config.PerProvider[provider.Name]
    // 使用 provider 特定的限制进行检查
}
```

**配置示例：**
```yaml
llm:
  limits:
    enabled: true
    provider_mode: per_provider    # 每个 provider 独立计数
    per_provider:
      claude:
        requests:
          daily_max: 500
      deepseek:
        requests:
          daily_max: 2000
```

## Implementation Design

### 文件结构

```
internal/agent/
  limits/
    limits.go         # 核心限制逻辑
    limits_test.go
    token_counter.go  # Token 计数器
    concurrency.go    # 并发控制器
    scheduler.go      # 定时重置任务（buildin crontab 类型）
    logger.go         # limit 事件日志
```

### Metrics 设计

使用 Prometheus metrics 暴露 limit 相关指标，供监控和告警使用：

```go
// internal/agent/limits/metrics.go

var (
    // 请求次数指标
    llmRequestsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "llm_limits_requests_total",
            Help: "Total LLM requests by provider and level",
        },
        []string{"provider", "level"}, // level: daily, weekly, monthly
    )

    // Token 使用量指标
    llmTokensTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "llm_limits_tokens_total",
            Help: "Total LLM tokens by provider, direction and level",
        },
        []string{"provider", "direction", "level"}, // direction: input, output
    )

    // 当前使用量（ Gauge，用于仪表盘）
    llmLimitsCurrent = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "llm_limits_current",
            Help: "Current usage by provider, type and level",
        },
        []string{"provider", "type", "level"}, // type: requests, tokens_input, tokens_output
    )

    // 拦截次数指标
    llmLimitsBlockedTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "llm_limits_blocked_total",
            Help: "Total blocked requests by provider, limit type and enforcement",
        },
        []string{"provider", "limit_type", "enforcement"},
    )

    // 并发数指标
    llmConcurrencyCurrent = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "llm_limits_concurrency_current",
            Help: "Current concurrent LLM calls",
        },
    )
)
```

**指标说明：**

| 指标名 | 类型 | 标签 | 用途 |
|--------|------|------|------|
| `llm_limits_requests_total` | Counter | provider, level | 累计请求次数 |
| `llm_limits_tokens_total` | Counter | provider, direction, level | 累计 token 使用量 |
| `llm_limits_current` | Gauge | provider, type, level | 当前使用量（实时） |
| `llm_limits_blocked_total` | Counter | provider, limit_type, enforcement | 累计拦截次数 |
| `llm_limits_concurrency_current` | Gauge | - | 当前并发数 |

**使用示例：**

```go
// 请求被拦截时
llmLimitsBlockedTotal.WithLabelValues("claude", "requests", "hard").Inc()

// Token 使用时
llmTokensTotal.WithLabelValues("claude", "input", "daily").Add(usage.InputTokens)

// 更新当前使用量
llmLimitsCurrent.WithLabelValues("claude", "requests", "daily").Set(counter.RequestsDaily)
```

### 日志设计

#### 日志级别与内容

使用 zap structured logging，INFO 级别记录关键事件：

```go
// 重置事件日志
zap.Info("limits reset",
    zap.String("level", "daily"),
    zap.String("type", "requests"),  // requests / tokens_input / tokens_output
    zap.Int("previous", 850),
    zap.Int("current", 0),
    zap.Time("next_reset", nextResetTime),
)

// 拦截事件日志
zap.Info("limits exceeded, request blocked",
    zap.String("level", "daily"),
    zap.String("type", "requests"),
    zap.Int("current", 1000),
    zap.Int("max", 1000),
    zap.String("enforcement", "hard"),
    zap.String("provider", "claude"),
)

// 并发限制事件日志
zap.Info("concurrency limit reached, request queued",
    zap.Int("waiting", 3),
    zap.Int("max_running", 5),
    zap.String("enforcement", "soft"),
)
```

#### 日志输出位置

- 重置事件：写入 `logs/limits_reset.log`（按天轮转）
- 拦截事件：写入 `logs/limits_blocked.log`（按天轮转）
- 同时写入主日志，方便调试

#### 日志格式

结构化 JSON 便于后续分析：
```json
{
  "level": "INFO",
  "ts": "2026-05-22T10:30:00Z",
  "msg": "limits reset",
  "type": "requests",
  "target_level": "daily",
  "previous_count": 850,
  "next_reset": "2026-05-23T00:00:00Z"
}
```

### 核心组件

#### 1. LimitsManager

全局限制管理器，负责：
- 加载和验证配置
- 维护计数器的持久化（使用 agent session 目录下的 json 文件）
- 提供 Check 方法供 LLM provider 调用前检查
- 注册定时重置任务（当 limits.scheduler.enabled: true 时）
- 记录重置和拦截事件到日志

```go
type LimitsManager struct {
    config     *config.LimitsConfig
    counter    *TokenCounter
    semaphore  *ConcurrencyLimiter
    scheduler  *CronScheduler // 可选，buildin 类型 crontab
    logger     *zap.Logger
    mu         sync.RWMutex
}

func (lm *LimitsManager) Start(ctx context.Context) error {
    if lm.config.Enabled && lm.config.SchedulerEnabled {
        lm.scheduler = NewCronScheduler(lm.config)
        // 注册所有 crontab 重置任务（buildin 类型）
        for level, cron := range lm.getAllResetCron() {
            lm.scheduler.AddFunc(cron, func() {
                lm.resetWithLog(level)
            }, "buildin")  // 指定类型为 buildin
        }
        lm.scheduler.Start()
    }
    // ... 继续初始化 counter 等
}

// resetWithLog 重置并记录日志
func (lm *LimitsManager) resetWithLog(level string) {
    lm.mu.Lock()
    before := lm.counter.Get(level)
    lm.counter.ResetLevel(level)
    after := lm.counter.Get(level)
    lm.mu.Unlock()

    lm.logger.Info("limits reset",
        zap.String("target_level", level),
        zap.Any("previous", before),
        zap.Any("current", after),
    )
}
```

#### 2. Crontab 类型系统

Dolphin 的 crontab 支持两种类型：

```go
type CrontabType string

const (
    CrontabTypeUser   CrontabType = "user"    // 用户定义的定时任务
    CrontabTypeBuildin CrontabType = "buildin" // 内置系统任务（如 limit reset）
)
```

**配置示例：**
```yaml
crontab:
  # 用户级任务（可删除修改）
  - name: daily-report
    type: user  # 默认
    cron: "0 8 * * *"
    action: send-report

  # 内置任务（系统自动创建）
  - name: limit-reset-daily
    type: buildin  # 内置类型
    cron: "0 0 * * *"
    action: llm-limit-reset
    metadata:
      level: daily
```

**类型区别：**
| 类型 | 来源 | 可删除 | 用途 |
|------|------|--------|------|
| user | 用户配置 | 是 | 用户自定义定时任务 |
| buildin | 系统自动创建 | 否 | 系统内置功能（如 limit reset） |

#### 3. CronScheduler

基于 robfig/cron 库的定时任务调度器，负责：
- 解析 crontab 表达式
- 在指定时间触发重置操作
- 支持多任务注册
- 根据类型（user/buildin）管理任务

```go
type CronScheduler struct {
    cron      *cron.Cron
    entries   map[string]cron.EntryID
    lm        *LimitsManager
    mu        sync.Mutex
}

// AddFunc 注册定时任务，type 指定任务类型
func (cs *CronScheduler) AddFunc(cronExpr string, fn func(), crontabType CrontabType) error {
    id, err := cs.cron.AddFunc(cronExpr, fn)
    if err != nil {
        return err
    }
    cs.entries[cronExpr] = id
    return nil
}
```

#### 4. TokenCounter

同上的多层级 Token 计数器，支持持久化。

#### 5. ConcurrencyLimiter

基于信号量的并发控制，soft mode 下支持排队等待。

### Scheduler 与时间窗口双重保障

启用 `limits.scheduler.enabled: true` 时，定时任务会主动触发重置：

```
┌─────────────────────────────────────────────────┐
│  Scheduler (主动)        vs         Check (被动) │
├─────────────────────────────────────────────────┤
│  定时执行 crontab              请求到达时检查    │
│  精确到秒                     依赖时间差判断     │
│  重置更可靠                   可能有时钟偏差     │
└─────────────────────────────────────────────────┘
```

两者结合使用：
- **Scheduler** 保证精确的重置时间点
- **时间窗口判断** 作为后备，防止 scheduler 崩溃时不会无限累积

### 集成点

在 `internal/agent/provider/provider.go` 的 Provider 接口层面集成：

```go
// Provider 接口添加 context 参数包含 limits 信息
type Provider interface {
    Complete(ctx context.Context, req ProviderRequest) (*ProviderResponse, error)
    CompleteStream(ctx context.Context, req ProviderRequest) (<-chan StreamChunk, error)
    HealthCheck(ctx context.Context) error
}

// 在 coordinator.go 或 llm 调用入口处，检查 limits
```

具体集成位置：`internal/agent/coordinator.go` 中的 `sendToLLM` 或类似方法。

### 检查流程

```
LLM Call Request
       ↓
┌──────────────────┐
│ 1. 并发检查       │ ← semaphore.acquire()
│    (获取信号量)   │
└──────────────────┘
       ↓
┌──────────────────┐
│ 2. 次数检查       │ ← requests daily/monthly
└──────────────────┘
       ↓
┌──────────────────┐
│ 3. Token 检查    │ ← tokens daily/monthly
└──────────────────┘
       ↓
    调用 LLM
       ↓
  更新计数器
       ↓
  释放信号量
```

### 持久化

计数器存储在 `$DOLPHIN_SESSIONS_DIR/limits_counter.json`，使用原子写保证安全：

```json
{
  "requests_daily": 150,
  "requests_monthly": 3200,
  "input_tokens_daily": 50000,
  "output_tokens_daily": 25000,
  "input_tokens_monthly": 500000,
  "output_tokens_monthly": 250000,
  "last_reset_daily": "2026-05-22T00:00:00Z",
  "last_reset_monthly": "2026-05-01T00:00:00Z"
}
```

**写入安全**：先写临时文件 `limits_counter.tmp`，写成功后 rename 到 `limits_counter.json`。崩溃重启后如发现临时文件存在，说明上次写入可能不完整，应删除临时文件并重新加载。

### 错误处理

- **Hard mode**: 返回 `ErrLimitExceeded` 并附带详细信息
- **Soft mode**: 排队等待，超时后返回 `ErrLimitExceededTimeout`

```go
var (
    ErrLimitExceeded       = errors.New("llm: usage limit exceeded")
    ErrLimitExceededTimeout = errors.New("llm: usage limit exceeded, timeout waiting")
)
```

### 恢复逻辑（Recovery）

限制计数器的持久化不仅要防止重启丢失，还要支持正确的恢复验证：

#### 1. 时间窗口的准确判断（多层级）

计数器文件中存储了每个层级上次重置的时间戳，重启后需要：

```go
func (tc *TokenCounter) checkAndResetIfNeeded() {
    now := time.Now()
    tc.mu.Lock()
    defer tc.mu.Unlock()

    // 检查每个层级是否需要重置
    for _, level := range []string{"daily", "weekly", "monthly"} {
        config := tc.config.GetLevel(level)
        if config == nil || config.ResetCron == "" {
            continue
        }

        nextReset := tc.parseCronNext(config.ResetCron, tc.lastReset[level])
        if now.After(nextReset) {
            tc.resetLevel(level)
        }
    }

    tc.persist()
}

// parseCronNext 根据 crontab 表达式和上次执行时间计算下次重置时间
func parseCronNext(cronExpr string, lastReset time.Time) time.Time {
    // 使用 robfig/cron 库解析 crontab 表达式
    // 计算从 lastReset 开始的下一个符合表达式的时间点
}
```

#### 2. 并发控制的状态恢复

同上的启动冷却期设计。

#### 3. 持久化存储的安全性

- 使用原子写（写临时文件后 rename）防止数据损坏
- 每次更新后立即 persist，避免内存丢失
- 设置合理的文件权限（0600）

#### 4. 限制触发的恢复

当 limit 被触发后：
- **Hard mode**: 直接返回错误，下次请求仍然会检查并拒绝
- **Soft mode**: 排队等待，有超时机制

软限制排队时可能程序崩溃，重启后：
- 等待中的请求会丢失（内存状态）
- 这是预期行为，请求方会收到超时错误并可以重试

## 状态

- 创建时间: 2026-05-22
- 状态: 设计完成，待实现

## Status 命令集成

### 输出格式

在 `dolphin status` 命令中增加 LLM Limits 模块状态展示：

```bash
$ dolphin status

╔══════════════════════════════════════════════════════════════╗
║                    LLM Usage Limits                          ║
╠══════════════════════════════════════════════════════════════╣
║  Enabled: true          Scheduler: active                    ║
╠══════════════════════════════════════════════════════════════╣
║  Requests (daily)       850 / 1000  [████████░░] 85%         ║
║  Requests (weekly)     3200 / 5000  [█████░░░░░] 64%         ║
║  Requests (monthly)    8000 / 10000 [████████░░] 80%         ║
╠══════════════════════════════════════════════════════════════╣
║  Tokens Input (daily)   500K / 1M     [█████░░░░░] 50%       ║
║  Tokens Output (daily)  250K / 500K   [█████░░░░░] 50%       ║
╠══════════════════════════════════════════════════════════════╣
║  Concurrency            3 / 5        [█████░░░░░] 60%       ║
╠══════════════════════════════════════════════════════════════╣
║  Next reset: 2026-05-23 00:00:00 (daily)                    ║
║  Next reset: 2026-05-25 00:00:00 (weekly)                   ║
║  Next reset: 2026-06-01 00:00:00 (monthly)                 ║
╚══════════════════════════════════════════════════════════════╝
```

### 实现方式

#### 1. StatusReporter 接口

```go
// internal/agent/limits/status.go

type StatusReporter interface {
    Report() LimitsStatus
}

type LimitsStatus struct {
    Enabled         bool
    SchedulerActive bool
    Requests        map[string]UsageStat  // "daily", "weekly", "monthly"
    Tokens          map[string]TokenStat
    Concurrency     UsageStat
    NextResets      map[string]time.Time
}

type UsageStat struct {
    Current int
    Max     int
    Percent float64
}
```

#### 2. 注册到 Status 命令

在 `cmd/status.go` 中注册 LimitsReporter：

```go
type StatusCommand struct {
    reporters []StatusReporter
}

func (s *StatusCommand) AddReporter(reporter StatusReporter) {
    s.reporters = append(s.reporters, reporter)
}

func (s *StatusCommand) Run() error {
    fmt.Println("\n╔══════════════════════════════════════╗")
    fmt.Println("║          LLM Usage Limits             ║")
    fmt.Println("╠══════════════════════════════════════╣")

    for _, reporter := range s.reporters {
        status := reporter.Report()
        s.renderLimitsStatus(status)
    }
}
```

#### 3. LimitsManager 实现 StatusReporter

```go
func (lm *LimitsManager) Report() LimitsStatus {
    counter := lm.counter.GetAll()
    config := lm.config

    return LimitsStatus{
        Enabled:         config.Enabled,
        SchedulerActive: lm.scheduler != nil && lm.scheduler.IsRunning(),
        Requests: map[string]UsageStat{
            "daily":   {Current: counter.RequestsDaily, Max: config.Requests.Daily.Max},
            "weekly":  {Current: counter.RequestsWeekly, Max: config.Requests.Weekly.Max},
            "monthly": {Current: counter.RequestsMonthly, Max: config.Requests.Monthly.Max},
        },
        Tokens: map[string]TokenStat{
            "daily_input":   {Current: counter.InputTokensDaily, Max: config.Tokens.Daily.InputMax},
            "daily_output":  {Current: counter.OutputTokensDaily, Max: config.Tokens.Daily.OutputMax},
        },
        Concurrency: UsageStat{
            Current: lm.semaphore.Current(),
            Max:     config.Concurrency.MaxRunning,
        },
        NextResets: lm.calculateNextResets(),
    }
}
```

### JSON 输出模式

当使用 `dolphin status --json` 时，输出结构化 JSON：

```json
{
  "limits": {
    "enabled": true,
    "scheduler": "active",
    "requests": {
      "daily": {"current": 850, "max": 1000, "percent": 85.0},
      "weekly": {"current": 3200, "max": 5000, "percent": 64.0},
      "monthly": {"current": 8000, "max": 10000, "percent": 80.0}
    },
    "tokens": {
      "daily_input": {"current": 500000, "max": 1000000, "percent": 50.0},
      "daily_output": {"current": 250000, "max": 500000, "percent": 50.0}
    },
    "concurrency": {"current": 3, "max": 5},
    "next_resets": {
      "daily": "2026-05-23T00:00:00Z",
      "weekly": "2026-05-25T00:00:00Z",
      "monthly": "2026-06-01T00:00:00Z"
    }
  }
}
```