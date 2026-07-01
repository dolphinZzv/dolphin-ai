# Changelog

All notable changes to Dolphin will be documented in this file.

## [Unreleased]

- **AgentMesh 自注册 + 服务端限流 + Gossip 发现 + `/agents` 命令**: AgentMesh 启动时通过 `Upsert` 将自身注册到 registry，`ListAgents()` 现在包含本地 agent。A2A transport 新增 `TaskRateLimiter` 接口，AgentMesh 对接 `ServerRateLimiter`（per-session/peer/global 三层）实现接收端限流。Gossip 增加 `Enabled` 开关，bootstrapper 在其启用时启动 UDP 发现。`boot_agentmesh.go` 集成 `LifecycleManager` 定期健康检查。新增 `/agents` 命令以 markdown/plain 格式列出 mesh 中所有 agent 的状态、负载、能力和模型。`config.schema.json` 新增完整的 `agents` 配置段和 `remoteAgent` 定义。`command_i18n.go` 新增中英文 `/agents` 相关字符串。

- **TUI 增量渲染实时化**: 移除 `textBlockDirty` 延迟渲染机制，每次 delta 到达立即通过 glamour 渲染 markdown，消除流式输出时"先显示原始 markdown 源码、再突然刷新为渲染后格式"的视觉闪烁。增量引擎仅重渲染尾部文本块，每次 delta 开销受当前段落大小约束而非整个文档。
- **TUI 队列简化**: 已完成项（`completedItems`）不再显示在队列中，`queueBodyLines` 和 `renderQueue` 签名移除 `completed` 参数，队列仅展示 pending 项。

- **LLM 动态模型回退 + 内置 provider 默认值**: 当 `LookupModelProvider` 找不到预注册的 per-model provider 时，不再跳过模型，而是回退到通用 shell（OpenAI/Anthropic），支持 OpenRouter 等动态模型列表。新增 `defaultBaseURL` 为 openrouter/deepseek 自动补全 `base_url`。新增 `providerHeaders` 为 OpenRouter 自动注入 `HTTP-Referer` / `X-Title`。

- **Memory.Read 支持 start/end 参数**: `Read(ctx, sessionID, start, end)` — `0,0` 返回全部，`-5,0` 返回最后 5 条。

- **TUI 历史会话自动恢复**: 新增 `tui.history_auto_restore` 配置（默认 false），启用后 TUI 启动时自动加载上次 session 的最后 5 条消息到消息区。

- **Ctrl+C 交互优化**: idle 时第一次 Ctrl+C 提示"再次按 Ctrl+C 退出"，第二次才退出 app；turn 进行中 Ctrl+C 终止 turn 并关闭 app。去掉 main.go SIGINT 注册 + 禁用 bubbletea 自建 signal handler，确保 Ctrl+C 仅走 bubbletea KeyMsg 路径。
- **go.mod 本地 glamour 替换**: 添加 `replace github.com/charmbracelet/glamour => ./dep/glamour`，使用本地修改版 glamour 子模块。
- **测试更新**: `tui_test.go`、`tui_e2e_test.go`、`tui_bench_test.go` 适配新签名和行为。

- **修复并行 tool_call 结果错位**: 部分 OpenAI/Anthropic 兼容端点流式不返回 `tool_call` ID，导致 TUI 按 ID 配对时所有 tool_result 都堆到最后一个 tool_call 下（`call A, call B, ok, ok`）。decode 层在 ID 为空时打印 warning 并合成唯一 UUID，保证 call/result 配对。
- **TUI 自定义主题系统**: 新增 `tui.theme` 配置，支持多套命名主题，每套含 light/dark 两组配色（按终端背景自动选择）。可主题化 user_message/tool_use/tool_result/thinking/response 的前景/背景色及整个 TUI 底色（`background: "default"` 跟随终端）。颜色支持 hex/ANSI256/命名色，留空回退内置 `default` 主题。新增 TUI 独有的 `/theme` 命令（无参循环、`/theme <name>` 指定切换）。
- **TUI UI 调整**: 用户消息每行加 `> ` 标识；welcome 页 pwd/branch 下加空行；CurrentMsg 顶部栏去掉用户名、emoji 改符号图标（▸/✓/✗）且处理完不消失；Ctrl+G 滚动提示从顶部栏移到 tips 区（不再与右下角百分比重复）；队列去掉标题行、active 不入队（在顶部显示），仅列 pending+completed；tip 符号 `💡`→`»`，中文队列符号统一为 `☰`。
- **TUI 多模态（图片）输入支持**: `types.Message` 由单一 `Content string` 重构为 `Parts []ContentPart`，使多模态内容能到达 LLM。`transport.IO.Read` 改为返回 `Input{Text, Parts}`，全部 10 个 transport 实现已迁移；附件经 `agentio.Turn`/`agentloop.State` 流入 `MemoryReadStage`。openai/anthropic build 在含图片时发出 `image_url`/`image` base64 块（纯文本路径行为不变；图片缺失降级为文本占位）。TUI 通过 `tea.PasteMsg` 捕获粘贴/拖拽的文件路径作为附件（≤20MB），输入框上方显示预览，空输入退格移除、Ctrl+X 清空。压缩摘要标注附件，`estimateTokens` 计入图片。自定义 `UnmarshalJSON` 兼容旧 `{"content":"..."}` session 文件。顺带修复提交闭包丢失附件、PasteMsg 重复插入两个 bug，以及同步 embedded `config.schema.json` 缺失的 mcp `headers` 字段。
- **Panda CI (回调/调用) 消息支持**: `handleMsgPush` 现在允许 ContentForm (10) 和 ContentFormResponse (11) 消息通过，修复 Panda 交互式表单回调消息被丢弃的问题。
- **MCP SSE 支持自定义 HTTP 标头**: MCP Client 和 LazyClient 新增 `SetHeaders` 方法，支持通过 `mcp_servers[].headers` 配置自定义请求标头。`ToolDesc` 新增 `Headers` 字段支持传输层 MCP 来源。
- **TUI welcome 显示 cwd 和分支**: Welcome 页面底部展示工作目录和 git 分支；Ctrl+G 在 tips 栏显示位置信息。
- **TUI welcome 居中 + queue 上限 2 行**: Welcome 页面垂直/水平居中，版本号移至底部；队列内容最多显示 2 行 body 以保持输入框区域突出。
- **README.md 增加 welcome.png 展示**: 在项目标语下方插入了 TUI 欢迎界面截图展示。

- **Ignore .mcp.json**: untracked chick `.mcp.json` files, added to `.gitignore`.
- **Gofmt dep/chick + panda.go**: formatting compliance for push hook.
- **Chick subtree + cleanup**: added `dep/chick` as git subtree from `dolphinZzv/chick:dev`. Removed AI agent access panel from chick login page and AI programming assistant integration section from chick README.
- **Rename deps/ → experiment/**: moved behavior-capture HTTP server and all references from `deps/` to `experiment/` directory. Formatted Go sources.
- **Dream fix**: zero-time display shows "never" instead of 0001-01-01; bootstrap sets LastDreamAt to now.
- **CI fix**: lint cleanup in dream tests — removed empty else branches, unused helper functions, fixed gci formatting.
- **Dream 离线自我编辑系统设计**: 完整设计文档 (`design/modules/dream.md`)，包含 Phase 0-4 架构、影响力权重、git 分支工作流、临时工作区隔离、自校准阈值、7 层验证策略。实现注意事项 (`design/modules/dream-notes.md`) 记录 11 项残余风险与缓解方案。
- **Dream 单元测试 + E2E**: gate/scan/edit/tidy/state 全覆盖 (63.6%)。Phase 0-4 纯规则路径全部通过，Phase 3 git 操作需要真实仓库集成测试。
- **Dream 离线自我编辑系统**: Phase 0-4 架构（门控、扫描、LLM编辑、应用、整理）、影响力度量、git 临时工作区隔离、自校准阈值、`/dream` 命令集。参见 `design/modules/dream.md`。
- **CI fix**: removed dead `hasBelow`/`hasAbove` assignments in `perm_dialog.go` that were overwritten before use (golangci-lint ineffassign).
- **`/brain push` / `/brain pull` / `/brain set url <url>`**: git operations on the brain repository. Authentication uses the system's SSH keys: ssh-agent first, falls back to `~/.ssh/id_ed25519` and `~/.ssh/id_rsa`. HTTPS remotes let go-git credential helpers or env vars handle auth. Aliases `/push` and `/pull` at root level.
- **Fix: qualified model name routing across sections**. `SetActiveModel` now preserves the section prefix when a qualified name (e.g. `deepseek_anthropic/deepseek-v4-flash`) is passed, preventing short-name collisions from routing to the wrong provider. When both `deepseek_anthropic` and `volcengine_agent` define `deepseek-v4-flash`, setting the active model via qualified name now reliably routes to the correct section rather than whichever loaded first in map-iteration order. `TestCrossSectionModelNameCollision` covers the fix.
- **`/session dump` and `/session compaction` subcommand fix**: previously registered as standalone root commands (`session dump`, `session compaction`), causing a duplicated `/session` entry in help output. Now properly mounted as subcommands of the `/session` command group (using `r.root.Find("session").AddCommand`), matching `session status`. Aliases `/dump` and `/compaction` remain at root level.
- **`workflow.schema.json`**: JSON Schema for `.workflow.yaml` files with `$id http://dolphin.siciv.space/workflow.schema.json`. Covers all fields from `WorkflowSpec`/`StepSpec` (version/name/description/steps[], each step: id/prompt/depends_on/foreach/output_schema/timeout/max_tokens/checkpoint). Embedded copy in `internal/context/` for go:embed, sync-tested via `TestEmbeddedWorkflowSchemaMatchesRepoRoot`. Copy in `docs/` root for public hosting. Existing `.workflow.yaml` files in `.dolphin/brain/workflow/` get `$schema` lines for IDE validation (gitignored, local only — no cache impact). `context.Workflow.BuildContent` now tells LLM the schema exists so it can read it via brain_read when unsure about field names/types.
- **Per-model custom HTTP headers**: `ModelConfig.Headers` (`llm.<section>.models.<i>.headers`) lets each model set its own headers. Model headers override same-named section-level headers (`llm.<section>.headers`); other section headers still apply. Useful for per-model route hints, version pins, or vendor-specific quirks. `mergedHeaders` helper in `models/` combines the two layers; shell providers (openai/anthropic) apply the merged set per request. Covered by `TestMergedHeaders` and `TestParseProviderModels/parses_per-model_headers`.
- **CI fixes**: `TestDingTalkStartWithWebhook` data race fixed (`notified` → `atomic.Bool`, was read by test goroutine while written by HTTP handler goroutine under `-race`); `TestHelpCommand` fixed by calling `root.InitDefaultHelpCmd()` after domain commands are registered in `NewRegistry` so `HasCommand("help")` works before `Execute`.
- **LLM retry with backoff**: `proto.HTTPStatusError` wraps non-200 responses with status code, `Retry-After` header (parsed per RFC 7231, seconds or HTTP-date), and body. `IsRetryable()` returns true for 429/5xx. `LLMStage` retry loop uses `isRetryableLLMError` (network/context errors + retryable HTTPStatusError) and `retryDelay` (honors `Retry-After` when present, else exponential backoff 500ms→30s with ±25% jitter). i18n strings `llm_backoff`/`llm_non_retryable` for user-visible retry notices. Fixes CI build break where `stages.go` referenced `proto.HTTPStatusError` before its definition was committed.
- **`/limit reset [target]`**: clear LLM usage counters so soft/hard limits start a fresh window. No target resets everything (global + all per-model). With a target: `reset deepseek` (vendor prefix), `reset deepseek-v4-flash` (short name, matches across providers), or `reset deepseek_anthropic/deepseek-v4-flash` (exact qualified model). Global counters are only cleared by the no-target form since they are shared across models. `Limiter.ResetUsage` also clears the matching `alerted` entries so those limits can re-fire alerts. Covered by `TestResetUsage_*`.
- Lint: `//nolint:exhaustive` on `pause.go` signal switch (only Interrupt/Cancel/Pause act, others ignored) and `//nolint:gosec` G404 on `stages.go` backoff jitter (`math/rand` is intentional — jitter needs no crypto randomness).
- **Lint cleanup**: `errorlint` (`errors.Is` for `http.ErrServerClosed`), `exhaustive` nolints on intentional default-absorbing switches (agentloop permission/signal, tui mouse button), `gocritic` ifElseChain→switch (tui viewport render), `staticcheck` QF1008 nolint on explicit `messageBuffer` selector, SA1012 nil context→`context.TODO`, gci/gofmt on `help_test.go` imports. `make lint` now reports 0 issues.
- **LLM refactor: per-model×api-type providers with transport/semantic split.** `internal/llm` split into four isolated layers: `proto/` (stateless SSE/HTTP transport), `proto/{openai,anthropic}/` (optional protocol toolkits), `models/` (one self-contained provider file per model×api_type), and `manager`/`registry` (routing, no silent fallback). Removes the 1400-line `openai.go`/`anthropic.go`/`stream.go` that mixed transport with semantics, the `custom`/`deepseek`/`volcengine` vendor dirs (95% copy-paste), the legacy `llm_test.go`, and the global mutable `LLMRequestHook` system. `LookupModelProvider` errors on miss instead of falling back to `openai`; `deepseek-v4-pro`'s `reasoning_effort=high` default moves from a global hook to a typed `reasoningWrapper`. `LLMRequest` gains `StreamSet` to mirror `ModelConfig.StreamSet`, letting providers distinguish "stream=false" from "stream not specified".
- Compaction uses real `last_input_tokens` (from provider) as threshold floor — `SessionMgr` wired into `CompactionStage`, new `estimateTokensReal()` method prefers session-stored prior-turn token count over rune estimate, which missed system prompts and tool schemas
- Tests: `TestCompaction_RealInputTokensTriggers` covers real-token-driven compaction and rune-based fallback
- Mouse-driven text selection: click and drag to select text in the viewport, `Ctrl+Shift+C` to copy selection to clipboard. Selection overlay renders on top of viewport content. "Copied" indicator appears in the status bar after successful copy.
- Permission dialog: `a (always)` requires double-press to confirm (safety guard against accidental permanent grants). Dialog remains 3-choice: once/always/deny. Reverted abort/yolo additions.
- Slash-command Tab-completion: `/` prefix triggers autocomplete from cobra registry + TUI-only commands (`/tools`, `/thinking`, `/windows`, `/exit`). Completions popup renders between queue and input. Tab cycles matches.
- Mouse wheel scrolling enabled in TUI viewport via `tea.WithMouseCellMotion()`.
- Input history: up/down arrow navigates previously submitted messages (last 100, dedup consecutive). Bubble Tea textarea provides built-in readline keybindings (Ctrl+A/E/W/U/K, Alt+B/F, etc.).
- **Permission system overhaul**: `always` now grants per-tool access (glob match) instead of exact-parameter matching, so approving `shell` once covers all future shell commands. Deny rules checked first and take priority over allow/safe-command auto-approval. Built-in safe commands (`ls`, `pwd`, `cat`, `grep`, etc.) auto-allow without prompting when no rule matches. Config-driven deny defaults (`permission.deny`) act as a safety net blocking dangerous operations like `rm *`, `sudo *`, `dd *` regardless of runtime grants.
- **Compaction**: auto-compaction now has a dedicated `compaction` config section (schema + defaults). `/session compaction` and `/compaction` commands for on-demand manual compaction — always compacts regardless of token threshold, with dynamic keepRounds adjustment to ensure at least one round gets summarized. `Compact()` exported from `CompactionStage`.
- **`/session dump` and `/dump`**: write the last turn's LLM request/response + tool calls to `<session.dump_dir>/<session_id>.json`. `dump` package with `Recorder` + `TurnDump` struct; recorded per-turn in `AgentLoop.processTurn` on success.
- **MCP SSE fixes**: removed `http.Client{Timeout: 10s}` that broke long-lived SSE connections; added `Accept: application/json, text/event-stream` header; removed `httpReq.Close = true`; `readSSE` became a `Client` method with `SetOnProgress` callback for intermediate progress events; `scanner.Err()` check added; buffer increased to 1MB.
- **TUI sidebar improvements**: `/windows` toggle for right-side status panel (persisted). Status bar always shows `turn:N` and `tok:N/60k(X%)` (live compaction progress). Session ID prefixed `session:`, git hash shown as `git:abc1234`. Side panel now includes `reasoning` and `thinking` rows when configured per-model. Separator changed to dashed line `╌`.
- **LLM `thinking` + `reasoning_effort`**: per-model `thinking` (bool) and `reasoning_effort` (string high/max) in model config. OpenAI format: `{"reasoning_effort":"high","thinking":{"type":"enabled"}}`. Anthropic format: `{"output_config":{"effort":"high"},"thinking":{"type":"enabled","budget_tokens":4096}}`.
- **Human-readable limits**: `1k`, `2.5m`, `1b` suffixes in `limitValue` config and `/limit` command display, parsed by `parseHumanCount` in `config.GetInt`.
- **AgentLoop**: `State.ModelName` populated before LLM call. `SetDumpRecorder` + `SetSessionGcInterval` setter methods.
- **AgentMesh 与工作流委托**: 新增 AgentMesh 模块，支持 agent 间委托、工作流步骤委托到远程 agent、A2A 协议扩展（agents/discover、agents/ping、tasks/sendSubscribe）、熔断器与重试交互、大文件分块传输协议。

- Add session idle-expiry: when a new turn arrives more than `session.expire_after` (default `1h`, set to `0` to disable) after the active session's last activity, AgentIO asks the user via their transport whether to rotate to a fresh session. "Yes" creates a new session and binds the turn to it; "no" reuses the current session and refreshes its `UpdatedAt`. Background turns without a transport context silently continue the active session.
- `transport.IO` gains `Confirm(ctx, prompt) (bool, error)` — a simple yes/no helper used by the expiry prompt and any future confirmations. Interactive transports (TUI/stdio) delegate to `RequestPermission` (Once/Always → yes, Denied → no); non-interactive transports return `false` with an error. `Session` now persists `UpdatedAt`; `session.Manager` exposes `Touch(id)` for AgentIO to refresh it per turn. `lifecycle.Builder` now captures the first bootstrap error via a `boot()` helper and panics in `Build()` instead of silently dropping it (was 13 unchecked `Bootstrap()` calls); `Limiter.RecordLLM` routes counter increments through an `incr()` helper that logs store errors without breaking the LLM path; `MCP.Execute`/`StdioClient.Execute` return argument-parse errors; `skills enable/disable` and `skill load` propagate save errors to the user; `main` exits non-zero on cobra errors; HTTP response encoders, multipart writes, best-effort index/migration, and advisory deadline sets are marked with explicit `_, _ =` / `_ =` and rationale where the error is genuinely not actionable. tighten `os.WriteFile` permissions to `0o600` for user data/config/session/permission/skill/scheduler files (G306, 14 sites); add `ReadHeaderTimeout` to HTTP servers in a2a and prometheus to mitigate Slowloris (G112); fix off-by-one slice indexing in `i18n.Register` that could read past the end of an odd-length dicts slice (G602, real bug); validate SMTP recipients against CRLF injection at the `sendSMTP` entry (G707); suppress G204/G117/G118/G401/G501 false positives (shell tool, login credential field, WeWork md5 digest, detached panic-event publish) with `//nolint:gosec` + rationale. most are event subscriber switches that intentionally handle a subset of an open `event.Type` enum (new event types must not force updates to every subscriber); others have a `default` that legitimately absorbs the remaining cases (e.g. `PermissionDenied`, `WebhookHTTP`, non-terminal step states). Annotated each with `//nolint:exhaustive` and a one-line rationale rather than listing every case redundantly. `WriteString(fmt.Sprintf(...))`→`fmt.Fprintf` (QF1012), `if/else if` on status→tagged switch (QF1003), nil context→`context.TODO` (SA1012, with `//nolint` where nil is the behavior under test), drop empty `else` branch (SA9003), `//nolint` on `Lock/Unlock` pairs whose Lock side-effect registers a session entry (SA2001) `errPermissionDenied` sentinel, `limitStore` interface, `FileStore.save` method, `mcpServerConfig` struct, `_removed` test stub, unused `models`/`ctx`/`cancel`/`mu` fields
- Apply `gocritic` cleanups: `assignOp` (`prefix +=`), `unlambda` (pass `appctx.NewRegistry` directly), `sloppyTypeAssert` (drop redundant type assertions), `singleCaseSwitch` (type switch → if), `appendAssign` (don't alias slice headers across append); suppress `ifElseChain` false positives on compound-predicate branches with `//nolint:gocritic` `sh -c "sleep N"` left the `sleep` grandchild alive holding the stdout pipe open after `sh` was killed, so the reader goroutine blocked until the grandchild exited naturally (watchdog appeared not to fire). The shell handler now puts the child in its own process group (`Setpgid`) and kills the whole group on context cancellation, and closes the pipe write end via `context.AfterFunc` so the reader unblocks immediately even if a grandchild lingers. Fixes the CI `TestWatchdog_ShellToolFiresOnSilentStall` failure (elapsed was the full `sleep 2`, now ~50ms).
- Fix `TestPanda_HandleFrame_SendAck_FlushesPending` race: replace a fixed `time.Sleep(100ms)` waiting for the async ack flush with a poll-until-flushed loop (3s deadline), so the assertion no longer races the readLoop on slow CI runners under `-race`.
- Loosen panda timeline frame-wait timeouts 1s→3s throughout panda_test.go for CI runner headroom under `-race`.
- Fix CI lint (`golangci-lint run ./... --new`): `skill.NewFileStore` now returns `(*FileStore, error)` instead of silently swallowing `os.MkdirAll` failures (mirrors `limit.NewFileStore`); `ToolsBootstrapper` propagates the error, test callers fail loudly via a `mustNewStore` helper
- Tighten `.golangci.yml`: add `gci` formatter (import grouping std / 3rd-party / `dolphin` prefix), exclude `errcheck`/`gosec`/`noctx` in test files, declare default presets explicitly (v2 disables them when an `exclusions` block is present), raise timeout to 5m
- Add high-value linters (`bodyclose`, `errorlint`, `noctx`, `unconvert`, `usestdlibvars`, `wastedassign`) and fix all their findings at net-zero issue count: close WebSocket dial response bodies; use `errors.Is` for sentinel comparisons; thread a real `context.Context` through the model-discovery chain (`Bootstrap` → `createProvider` → `discoverProviderModels` → `DiscoverModels` → `DiscoverAnthropic/OpenAIModels` → deepseek); use `ListenConfig`/`tls.Dialer.DialContext` for a2a/email; bound the Prometheus remote-write push with a 10s timeout context
- Makefile gates `-race` behind `RACE=1` / `make build-race`; the default `make build` no longer links ThreadSanitizer, cutting idle CPU from ~8% to ~0
- Fix TUI side-panel overflow: `renderSideStatus` passed inner dimensions to lipgloss `Width`/`Height` (content area), but the border adds 2 rows/cols outside it, so the panel overshot by 2 rows and scrolled the top of the view off-screen (hiding the Status header and the top of the message viewport, including the user's own echo)
- Add context compaction: when the estimated token count exceeds `compaction.max_tokens`, the oldest messages are summarized into a single `IsSummary` message kept at the head of Messages, while the most recent `compaction.keep_rounds` rounds are preserved verbatim; the compacted history is persisted via a new `Memory.Replace` so subsequent turns (and restarts) use the trimmed context without re-summarizing
- `Memory` interface gains `Replace`; `types.Message` gains `IsSummary`; new `EventCompaction` event; `compaction.enabled` switches the feature off
- Compaction summarizer switch marked `//nolint:exhaustive` (system messages never flow through Messages); drop an unused test helper
- Agent loop: catch panics raised by compositor stages — convert to a TurnResult with Done=true and re-panic so runWorker's recovery still drives backoff and publishes EventWorkerPanic; the turn is no longer silently dropped on a stage panic
- TUI: permission dialog colors switched from hardcoded literals to adaptive theme colors (light/dark terminal support)

- /context list uses 1-based numbered format (1) base, 2) soul, ...)
- Add /session stop and /session continue to pause and resume turn processing
- Add test coverage for pause/resume paths in LLMStage, ToolStage serial, and processParallel
- Add tea.WithInputTTY() and error logging to TUI transport for diagnostics
- Add speedtest workflow
- Add idle-timeout watchdog (llm_idle_timeout) with throttled per-chunk/per-line feed (feed_min_interval) so stalled LLM/tool calls fail fast instead of hanging the turn
- Add checkpoint recovery: on recoverable failures the unflushed memory tail is written with IsPartial markers, enabling future /continue resume
- Add internal/progress package: nil-safe context-attached feed point that breaks the agentloop<->tool import cycle
- Shell tool now streams stdout line-by-line and feeds the watchdog per line, so long builds/commands aren't misjudged as stalls
- `config init` now also writes config.schema.json (embedded in binary) alongside config.yaml; default config.yaml references it via $schema for editor validation
- TUI gains a right-hand side status panel (~20% width, min 16 cols) showing model, temperature, pool size, workmode, turn/req/tok/tools usage with k/m/b/t suffixes; long values truncate with ellipsis instead of wrapping
- Side panel uses dashed open-bottom border (left/right `┊` extend to the queue separator); viewport and side panel resize dynamically with textarea/queue/statusbar heights so borders stay aligned
- Narrow terminals fall back to a full bottom status bar; wide terminals keep a compact bottom bar (identity + model + /exit) alongside the side panel
- gofmt fix for side panel border struct alignment
- TUI: bottom status bar shows a spinner + elapsed time while a turn is pending, so there is live feedback even with tool/thinking output hidden
- TUI: scrolling away from the bottom shows a scroll-position indicator (`↑ 42%`) and `Ctrl+G` jumps back to the latest output
- TUI: tool errors (`❌`) now always render even when tool-call display is off, using the previously-unused error color; failed tool calls are no longer silently invisible
- TUI: queue shows `+N queued`/`+N done`/`+N more active` indicators instead of silently dropping items; pending list shows the head (next to run) rather than the tail
- TUI: side panel renames the tool-call-count row to `calls` so it no longer collides with the `tools` on/off toggle
- TUI: permission dialog is now a true modal — captures all keys (no stray textarea typing), supports `y/Y a/A n/N`, arrow-key + Enter navigation, and is centered on screen
- TUI: fix permission dialog being unresolvable — the response channel is now delivered with the prompt message instead of a stale nil copy
- Agent loop: feeds the idle watchdog while a permission prompt waits for user input, so a slow reader no longer trips `llm_idle_timeout` and cancels the turn mid-prompt
- TUI: drop the redundant `/exit` hint from the wide-mode bottom status bar
- Fix cross-test data race: `requestPermissionFeeding`'s detached goroutine read the test-mutable global `permFeedInterval` directly, racing with the next test's write to it under `-race`; the value is now read once on the caller's synchronous path and captured in the closure so no detached goroutine touches the global

### Changed

- Merged create/update/delete into upsert for scheduler, subscriptions, commands, scripts, skills to reduce tool count and per-turn context.
- TUI status bar now displays git hash version alongside agent name (e.g., `🐬 Dolphin abc1234`).
- TUI queue display now properly renders after boot; SetAgentIO send deferred until program ready.

### Added

- TUI transport with adaptive terminal theming, markdown rendering, and permission dialogs.
- Ctrl+Enter in TUI sends message as priority turn, jumping to the front of the agent queue.
- Priority queue channel in AgentIO and AgentLoop for front-of-queue turn dispatch.
- TUI status bar shows session usage (tokens, rounds) and limit caps with percentage.
- TUI live queue display showing active and pending turns above the input area.
- `config init` command to generate a default config.yaml file.

### Fixed

- Turn timeout now covers the entire turn (init + loop stages), not just each loop round.
- LLM HTTP client now derives timeout from context deadline, preventing hung connections.
- SubscriptionEngine data race on `running` field (switched to atomic.Bool).
- DroppingMemory no longer orphans tool_result messages when truncating the message window.
- Nil pointer panic in status command when no active session exists.
- Per-model `stream`, `temperature`, `top_p` configuration in provider model lists.
- Non-streaming LLM path (`CompleteOpenAI`) for models that don't support SSE.
- LLM request hooks (`internal/llm/models/`) for per-model request rewriting.
- DeepSeek V4 Pro preset: default `reasoning_effort=high`.
- `/status` command shows current model's `temperature` and `top_p`.
- Python test scripts for OpenAI and Anthropic API validation (`scripts/`).
- Sticky floating indicator at top showing current user message with pending/success/error state.
- GitHub Issue auto-review system: poller script (`scripts/github-issue-poll.sh`) cross-repo, deduped via `.dolphin/github-issues-state.json`, cron every 5min, subscription `review-assigned-issues` triggers LLM analysis + auto-label + reply draft on `file.update` of `.dolphin/issue-changes.json`.

### Changed

- TUI theme system replaced with terminal-native adaptive colors via lipgloss.AdaptiveColor.
- Status bar stays single-line, dropping low-priority parts when content exceeds terminal width.
- Status bar req/tok simplified to percentage only; added tool call count display.
- TUI `show_tools`, `show_thinking`, `workmode` now read from config; config values take priority over persisted prefs.
- TUI status bar shows `pool_size` and `tool_parallelism` when above defaults.
- Thinking continuation lines padded to align with content text.
- gofmt formatting fixes across changed files.
- Fix agentloop LLMRequest missing `Stream: true`, causing non-streaming path and DeepSeek API 400 error.
- Per-round turn timeout: each agent loop round gets a fresh timeout so long-running tools don't starve subsequent LLM calls.
- Tool parallelism config (`agent.tool_parallelism`) for concurrent tool execution.
- Workflow agent-driven e2e tests.

### Changed

- User input text color changed to green in both TUI themes.
- Markdown rendering preserves ANSI color codes for syntax highlighting.
- Various lint and formatting fixes for the TUI transport.
- `os.Exit` made replaceable via package-level `osExit` variable for testability.
- Transport test coverage: `stdio.go` improved from 67.8% to 92.1% with pipe-backed readline tests.
- TUI test coverage: all changed files (`model.go`, `tui.go`, `renderer.go`, `theme.go`, `perm_dialog.go`) now above 80%.
- TUI viewport uses incremental rendering: only re-renders the changed message tail instead of rebuilding all content on every append.
- Replace range loop with variadic append for block offsets in incremental renderer.
- TUI messages capped at 500 entries with front-trim to prevent memory exhaustion in long conversations.
- TUI streaming text bypasses glamour markdown rendering to avoid O(n²) re-render cost; block is finalized on next non-text event.
- TUI e2e tests: 8 integration tests covering streaming conversation, permission flow, multi-turn memory, theme switch, and dirty block lifecycle.
- TUI benchmarks: 10 benchmark suites measuring render performance, streaming vs non-streaming paths, and markdown rendering cost.
- TUI `Flush()` now sends `flushMsg` to program to finalize dirty text blocks; previously dirty blocks persisted until the next non-text event.

### Breaking Changes

- **Stage.Clone()** is now a required method on the `Stage` interface. External `Stage` implementations must add a `Clone() Stage` method. See `docs/adr/002-compositor-clone.md` for rationale.
- **Engine.NewEngine** signature changed: removed `brain *brain.Brain` parameter (was stored but never used).
- **AgentIO.SetProcessing** removed. This method was a no-op kept for backward compatibility. Use `ActiveSnapshot()` or `QueueSnapshot()` to inspect worker state.

### Added

- Worker pool support: `agent.pool_size` config (default 1) enables multi-worker turn processing.
- Chaos tests: context cancellation under session lock, compositor panic recovery, 100 concurrent same-session turns.
- Fuzz tests: `FuzzValidSessionID`, `FuzzSessionLock`, `FuzzCompileTemplate`.
- Benchmarks: `BenchmarkAgentLoopSingleTurn`, `BenchmarkAgentLoopMultiWorker`, `BenchmarkCompileTemplate`, `BenchmarkRenderPrompt`.
- Architecture Decision Records: `docs/adr/001-per-session-lock.md`, `docs/adr/002-compositor-clone.md`, `docs/adr/003-workflow-no-session-memory.md`.
- Workflow context section expanded with complete YAML format reference, template syntax, foreach, checkpoint, and file lifecycle instructions for the LLM.

### Changed

- Session lock GC interval is now configurable via `agent.session_gc_interval` (default 300s).
- `SetActive`/`ClearActive` moved after session lock acquisition — elapsed time in `/queue` no longer includes lock wait.
- Agentloop tests replace `time.Sleep` with channel/waitgroup synchronization — test suite runtime reduced ~40%.

### Removed

- `workflow.max_steps` config key (was never read).
- `Engine.brain` field (was never referenced).
- `AgentIO.SetProcessing` method (was a no-op).

### Fixed

- Worker panic recovery: exponential backoff (1s, 2s, 4s...) capped at 30s, worker exits after 5 consecutive panics. Publishes `EventWorkerPanic` event on every panic including the final one.
- `truncateInput`/`truncateForMarkdown` duplicate functions merged.
- `sortedWorkerIDs` replaced bubble sort with `sort.Strings`.
- Error wrapping: all `fmt.Errorf` calls that pass an error use `%w`, enabling `errors.Is`/`errors.As` throughout the codebase.
- Data races in `-race` CI: fixed races in `command.Execute` (lock entire body), `ExecuteWithTimeout` (pre-check ctx.Err), `weWork.sendAndWait` test, `watcher` test. Skipped readline-based stdio tests under `-race` (third-party race in `github.com/chzyer/readline`).
- golangci-lint: fixed gofmt, exhaustive, gosec, and staticcheck issues in transport, workflow, limit, and llm packages.
- Workflow path resolution: `run_workflow` now resolves file paths relative to the brain directory, fixing a mismatch where `brain_write` wrote to the brain dir but `run_workflow` read from CWD.
- Workflow step timeout: `run_workflow` handlers now use `context.WithoutCancel` to strip the tool pipeline's 30s timeout. Workflow steps can now be configured to never timeout by setting `workflow.step_timeout: "0s"` or per-step `timeout: "0s"`.
	- Root OpenAI/Anthropic providers now route to non-streaming HTTP path when `Stream: false`, matching the volcengine provider pattern. Adds `CompleteAnthropic` for non-streaming Anthropic API calls.
	- gofmt formatting fixes across changed files.
	- Add test coverage for temperature and topP display in `/status` command.
	- Add test coverage for LLM request hook registration and dispatch.
	- Add test coverage for non-streaming OpenAI and Anthropic HTTP completion paths.
	- Fix data race in TUI TestRequestPermission_ReplyPaths: protect permCh access with mutex.

  (format fix)
- (gofmt formatting fix)
- Fix CI flaky tests: add mutex to testSessionStore, sort multi-worker results, poll for timeline ack instead of fixed sleep
- Add `bin` to agent schema properties

- **Async MCP client**: New `mcp.AsyncClient` for lazy/non-blocking MCP connections during bootstrap. Both URL-based and stdio MCP servers use async initialization — startup proceeds even if a server is slow to connect. The `Connector` callback runs in a background goroutine; `List()` returns cached tools once connected, `Execute()` errors until ready. `SetOnConnect` provides a logging hook. Bootstrappers in `setup/boot_tools.go` and `setup/boot_transports.go` register async clients instead of blocking on `List()`.
- **TUI tips notification system**: Toggle messages (`/tools`, `/thinking`, `/windows`) and copy confirmations no longer pollute the message history. Instead they appear as a temporary `💡` banner between the viewport and the queue, auto-dismissing after 3 seconds. New model messages: `tipsMsg`, `clearTipsMsg`, `mcpCountMsg`.
- **MCP tool count in TUI status bar**: Shows `mcp:N` in both the narrow (inline) status bar and the side panel. Tool registry is passed to TUI during bootstrap; `syncSession()` queries it and sends `mcpCountMsg` to update the model.
- **Copy-to-clipboard confirmation**: After `Ctrl+Shift+C` copies selected text, a "Copied" (`已复制到剪贴板`) tip message appears for 2 seconds instead of a silent copy.
- **Column-aligned command list**: `/commands list` now renders aligned columns with `command`/`status`/`description` headers and dashed separators instead of a simple indented list. Repeated list-rendering logic extracted into `printCommandList` helper.
- **Side panel separator simplified**: `╌` (U+254C) changed to `-` (ASCII hyphen) for simpler rendering.
