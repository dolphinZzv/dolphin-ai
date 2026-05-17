# Windows 支持增强设计

## 目标

使 Dolphin Agent 能够在 Windows 平台上正常运行，解决当前运行时阻塞问题。

## 变更清单

### 1. Shell MCP — 平台感知的 Shell 检测

**文件**: `internal/mcp/shell.go`

**当前问题**: 第 129 行硬编码 `exec.CommandContext(ctx, "sh", "-c", params.Command)`，Windows 无 sh。

**方案**:
- 新增 `internal/mcp/shell_unix.go` 和 `internal/mcp/shell_windows.go`，用 build tag 分离
- 定义 `func shellCommand(ctx context.Context, command string) *exec.Cmd` 接口
- **Unix**: 保持 `exec.CommandContext(ctx, "sh", "-c", command)`
- **Windows**: 按优先级自动检测可用 shell:
  1. `powershell.exe` → `powershell.exe -NoProfile -Command command`
  2. `cmd.exe` → `cmd.exe /C command`
  3. `bash.exe` (Git Bash/WSL) → `bash.exe -c command`
  4. 均不可用 → 返回错误 "no suitable shell found"

**检测策略**: 使用 `exec.LookPath()` 在 PATH 中查找，首次调用时检测并缓存结果。

### 2. Script 插件 — 平台感知的解释器 + 扩展名

**文件**: `internal/plugin/scripts.go`

**当前问题**: 第 214、252 行硬编码 `exec.CommandContext(ctx, "sh", scriptPath)`。

**方案**:
- 新增 `plugin/interpreter_unix.go` 和 `plugin/interpreter_windows.go`
- 定义 `func shellInterpreter() string`，返回 shell 可执行文件名
  - **Unix**: 返回 `"sh"`
  - **Windows**: 按优先级检测 `powershell.exe` → `cmd.exe` → `bash.exe` → 报错
- 修改 `runHookScript`/`runEventScript`: 用 `shellInterpreter()` 替换硬编码 `"sh"`
- 修改 `discoverHookScripts`/`discoverEventScripts`(仅 Windows): 
  - 除 `.sh` 外也匹配 `.ps1`、`.bat`、`.cmd` 扩展名
  - 因为 `strings.TrimSuffix(entry.Name(), ".sh")` 只会去掉 `.sh`，不影响其他扩展名

### 3. 会话/临时目录 — 使用 os.TempDir

**文件**: `internal/config/config.go`

**当前问题**: `Session.Dir` 默认值为 `/tmp/dolphin`，第 331/476/664 行有硬编码。

**方案**:
- 新增 `internal/config/path_unix.go` 和 `internal/config/path_windows.go`
- 定义 `func defaultSessionDir() string`:
  - **Unix**: 返回 `"/tmp/dolphin"`（兼容现有行为）
  - **Windows**: 返回 `filepath.Join(os.Getenv("TEMP"), "dolphin")` 或 `filepath.Join(os.Getenv("TMP"), "dolphin")`
- 将 `cfg.Session.Dir` 的默认值从字面量改为调用 `defaultSessionDir()`
- 同时修复第 434 行 `hd = "/tmp"` 后备逻辑：
  - **Unix**: `"/tmp"`
  - **Windows**: `os.Getenv("TEMP")`

### 4. 系统配置目录 — Windows 路径适配

**文件**: `internal/config/config.go` 第 21 行

**当前问题**: `SystemConfigDir = "/etc/dolphin"`，Windows 上变 `\etc\dolphin`。

**方案**:
- 转用到 `config/path_unix.go` / `config/path_windows.go`
- 定义 `func defaultSystemConfigDir() string`:
  - **Unix**: `"/etc/dolphin"`
  - **Windows**: `filepath.Join(os.Getenv("ProgramData"), "dolphin")`
- `SystemConfigDir` 改为运行时计算的变量，而非编译时常量

### 5. 信号处理 — 平台感知

**文件**: `cmd/root.go` 第 338 行，`internal/mcp/transport_stdio.go` 第 124 行

**当前问题**: `syscall.SIGTERM` 在 Windows 上行为不同/不支持。

**方案**:
- `cmd/root.go`: 保留 `syscall.SIGINT` 和 `syscall.SIGTERM`，Go 标准库在 Windows 上能编译但 SIGTERM 不会被真正发送。保持当前代码不变（无副作用）。
- `internal/mcp/transport_stdio.go` 第 122-126 行: 已有 `process.Signal(syscall.SIGTERM)` → 错误时 fallback 到 `Process.Kill()`。错误信息从 "interrupt not supported" 改为更准确的 "signal not supported on this platform"。

### 6. SHELL 环境变量 Fallback

**文件**: `internal/transport/stdio.go` 第 54 行，`internal/config/career.go` 第 305/312 行

**当前问题**: `os.Getenv("SHELL")` 在 Windows 上为空。

**方案**:
- `internal/transport/stdio.go`: 使用 `shellInterpreter()` (来自第 2 项) 获取可读的 shell 名称
- `internal/config/career.go`: 当 `SHELL` 为空时，检测并填入 Windows shell 名称

### 7. 单元测试

**新增/修改测试**:

| 测试文件 | 测试内容 |
|---------|---------|
| `internal/mcp/shell_test.go` | 新增 Windows shell 检测测试（mock LookPath） |
| `internal/plugin/scripts_test.go` | 新增 Windows 解释器检测测试 |
| `internal/config/config_test.go` | 验证 Session.Dir 默认值在 Windows 上使用 TEMP |
| `internal/config/path_test.go` | 新文件：测试 defaultSessionDir / defaultSystemConfigDir |
| `internal/mcp/transport_stdio_test.go` | 验证 SIGTERM fallback 行为 |

### 8. CI — Windows Runner

**文件**: `.github/workflows/ci.yml`

**变更**:
- 增加 `windows-latest` 矩阵策略
- 排除 race 检测（Windows 上不支持 `-race` 的某些场景）
- 跳过需要 Unix 特定环境的测试步骤

## 不处理项

- 文件权限位 (`os.MkdirAll(dir, 0700)` 等): Windows 忽略权限位，不影响功能
- WSL 集成: 超出当前 scope
- Windows Service/守护进程: 超出当前 scope

## 兼容性

- 所有 Unix 代码路径不变，无回归风险
- Build tag `!windows` 确保现有平台代码不受影响
- 新代码通过 CI 和用户本地验证

## 回滚方案

- 单个文件级别回滚: 每个变更独立，可单独 revert
- 全局回滚: `git revert <merge-commit>`

<!-- last-modified: 2026-05-15 -->
