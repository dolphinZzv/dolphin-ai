# Behavior Capture — 独立 Chrome 扩展行为数据采集

## 概述

一个**完全独立**的 Chrome 扩展，采集用户在浏览器中的真实操作行为（点击、导航、表单交互等）。

扩展将行为事件实时推送到一个**独立的本地 HTTP server**，server 以 JSON Lines 格式追加写入文件。用户在 Dolphin agent 中说"分析我的录制数据"，agent 用已有的 `shell` 工具 `cat` 文件，分析后调用 `skill_upsert` 生成 Skill。

**核心决策：**

1. **零 Dolphin Go 代码变更。** 扩展 + HTTP server 是独立组件，与 Dolphin 仅通过文件系统耦合。
2. **扩展只做采集。** Content script 采集 → SW 聚合 → HTTP 推送。存储逻辑全在 server 侧。
3. **JSON Lines 存储。** 每条事件一行 JSON，追加 O(1)，崩溃可恢复，LLM 可读。
4. **本地处理，隐私优先。** 绑定 127.0.0.1，三层过滤脱敏，数据不离开本机。

---

## 架构

```
┌───────────────────────────────────────────────────────────────────┐
│                     CHROME BROWSER                                  │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  |           CHROME EXTENSION (Manifest V3, unpacked)           │  │
│  |                                                              │  │
│  |  [Popup UI]  <--------->  [Background SW]                   │  │
│  |      |                          |     |                       │  │
│  |      v                          v     v                       │  │
│  |  [Content Script]          [Ring Buffer]  [Privacy Filter]   │  │
│  |   Tab A, Tab B, ...                                          │  │
│  |       |                          |                            │  │
│  |       | chrome.runtime.Port      | fetch() HTTP POST          │  │
│  |       v                          v                            │  │
│  +-------|--------------------------|----------------------------+  │
└──────────|--------------------------|--------------------------------┘
           | long-lived Port          | 127.0.0.1:9200
           | (keep SW alive)          | JSON Lines batch
           v                          v
┌──────────────────────────────────────────────────────────────────┐
│             BEHAVIOR HTTP SERVER (独立二进制, 非 Dolphin)          │
│                                                                    │
│  POST /api/session/start    → 创建 session (写入 .meta.json)        │
│  POST /api/session/:id/events → 追加事件 (JSON Lines append)       │
│  POST /api/session/:id/end    → 结束 session                       │
│  GET  /api/sessions           → 列出 sessions (读 .meta.json)      │
│  GET  /api/session/:id        → 返回完整 session (cat .jsonl)      │
│  DELETE /api/session/:id      → 删除 session                       │
│  GET  /api/health             → 健康检查                           │
│                                                                    │
│  存储: experiment/fox/data/sessions/{id}.jsonl    ←    事件              │
│        experiment/fox/data/sessions/{id}.meta.json ←    元数据            │
└──────────────────────────────────────────────────────────────────┘
                                    │
                                    │ 用户告诉 agent:
                                    │ "cat {data_dir}/abc123.jsonl"
                                    ▼
┌──────────────────────────────────────────────────────────────────┐
│                    DOLPHIN AGENT (零代码变更)                      │
│                                                                    │
│  shell → LLM 分析行为模式 → skill_upsert → SKILL.md               │
└──────────────────────────────────────────────────────────────────┘
```

---

## 1. JSON Lines 存储设计

### 1.1 为什么 JSON Lines

标准 JSON 数组无法追加——每次写入都必须重写整个文件。录制 1 小时产生 ~200KB JSON 还能忍，但一个重度 session 可能 2MB+，每次都全量重写不可接受。

JSON Lines (`.jsonl`):

```
{"session_id":"abc","sequence":1,"timestamp":"...","event_type":"page.load",...}
{"session_id":"abc","sequence":2,"timestamp":"...","event_type":"click",...}
{"session_id":"abc","sequence":3,"timestamp":"...","event_type":"input",...}
```

- **追加 O(1):** 新事件只需在文件末尾写一行。
- **LLM 可读:** 每行是合法 JSON，`cat file.jsonl | head -100` 即可查看前 100 个事件。
- **崩溃恢复:** 每行是一次原子 write。崩溃只丢最后一行（未写完）。
- **流式处理:** 逐行读取不需加载整个文件到内存。

### 1.2 文件布局

```
{data_dir}/                      # 可配置, 默认 experiment/fox/data/sessions/
  abc123.meta.json               # session 元数据
  abc123.jsonl                   # 事件序列 (每行一个 BehaviorEvent JSON)
```

`.meta.json`:

```json
{
  "id": "abc123",
  "started_at": "2026-06-26T10:30:00Z",
  "ended_at": "2026-06-26T10:45:00Z",
  "status": "completed",
  "domains": ["github.com", "jira.example.com"],
  "event_count": 234,
  "chrome_info": {"version": "130.0.0.0", "os": "macOS 15.0"},
  "extension_version": "0.1.0",
  "segments": [
    {"start_seq": 150, "end_seq": 165, "reason": "auth_page", "domain": "[REDACTED]"}
  ]
}
```

`abc123.jsonl`:

```
{"session_id":"abc123","seq":1,"ts":"2026-06-26T10:30:01Z","tab":42,"domain":"github.com","path":"/dolphin","type":"page.load","p":{"url_path":"/dolphin","is_spa":false}}
{"session_id":"abc123","seq":2,"ts":"2026-06-26T10:30:05Z","tab":42,"domain":"github.com","path":"/dolphin","type":"click","p":{"el":"button","text":"Pull requests","sel":"a[href$='/pulls']","m":"id+class","alts":[{"s":"a[aria-label=\"Pull requests\"]","m":"aria-label","st":0.9}]}}
```

### 1.3 原子写入策略

```
追加事件 (POST /api/session/:id/events):

  1. 获取 per-session 文件锁 (flock / syscall.Flock)
  2. 对每条事件:
     a. 序列化为单行 JSON (无换行符)
     b. 调用 write(fd, line + "\n")
     c. write 在 POSIX 系统上对小于 PIPE_BUF (通常 4096 bytes) 的写入是原子的
        → 单条事件 JSON 约 200-500 bytes，远小于 PIPE_BUF，保证原子
  3. 每 30 秒或每 100 条事件触发一次 fsync
  4. 更新 meta 文件:
     a. 写入 .meta.json.tmp
     b. fsync .meta.json.tmp
     c. rename(.tmp, .meta.json)   → 原子替换
  5. 释放锁

崩溃场景:
  - 写事件行中途崩溃 → 最后一行是不完整 JSON (读时丢弃即可)
  - 写 meta.tmp 中途崩溃 → .meta.json 保持旧值 (rename 语义保证)
  - server 启动时扫描: 有 .jsonl 但 meta.status != "completed" → 标记 aborted
```

### 1.4 JSON 字段压缩

事件 JSON 使用短 key 减少体积和 LLM token 消耗：

| 长 key | 短 key |
|--------|--------|
| `session_id` | `s` |
| `sequence` | `seq` |
| `timestamp` | `ts` |
| `tab_id` | `tab` |
| `url_domain` | `domain` |
| `url_path` | `path` |
| `event_type` | `type` |
| `payload` | `p` |
| `element_tag` | `el` |
| `element_text` | `text` |
| `css_selector` | `sel` |
| `css_selector_method` | `m` |
| `alternatives` | `alts` |
| `stability` | `st` |

Agent 端分析时 LLM 能理解短 key，无需转换。文件体积减少 ~30%。

---

## 2. Service Worker 生命周期

### 2.1 核心问题

Chrome MV3 在 SW 闲置 ~30 秒后终止进程。但录制 session 可能长达 45 分钟，期间用户可能长时间阅读页面而不交互。

### 2.2 存活机制：Long-lived Port

```
Content Script (每个 tab)
  │
  │  chrome.runtime.connect({ name: "behavior-events" })
  │  Port 创建后 Chrome 不会 kill 对应的 SW
  │
  ▼
Background SW
  │
  │  chrome.runtime.onConnect.addListener((port) => {
  │      // 只要至少一个 Port 打开，SW 不会被 Chrome 终止
  │      port.onMessage.addListener(handleEvent)
  │      port.onDisconnect.addListener(() => {
  │          // 所有 Port 关闭 → Chrome 可能在 ~30s 后终止 SW
  │          // 此时需要保护关键数据
  │          flushRemainingEvents()       // 发送剩余数据到 server
  │          saveStateToStorage()         // 存最后序列号到 chrome.storage
  │      })
  │  })
  │
  ▼
Ring Buffer → HTTP POST → Behavior Server
```

**为什么 long-lived Port 能保持 SW 存活:**
- Chrome 的 SW 生命周期管理会根据活跃连接判断
- 打开的 `chrome.runtime.Port` 被视为活跃连接
- 即使没有消息传输，Port 本身的存在就阻止 SW 终止

**Content script 连接策略:**

```
每个录制中的 tab 在注入后立即:
  1. port = chrome.runtime.connect({ name: "behavior-events" })
  2. port.onDisconnect → 500ms 后重连 (防止 SW 异常重启)
  3. page.unload → port.disconnect()

至少有一个 tab 在录制 → SW 保持存活
用户关闭所有 tab → SW 可能终止 → 但 session 已结束
```

### 2.3 SW 被动重启恢复

即使有 Port 保护，SW 仍可能被 Chrome 强制终止（更新、内存压力、用户手动停止）。

```
SW 启动时:

  // 1. 恢复录制状态
  chrome.storage.local.get(['active_session', 'last_seq']).then(data => {
      if (data.active_session) {
          // 之前有活跃 session，恢复
          currentSession = data.active_session
          lastFlushedSeq = data.last_seq

          // 2. 检查是否有 dangling 事件
          // (content script 可能在 SW 死前发送了事件但 SW 没 flush)

          // 3. 重新创建 Ring Buffer
          ringBuffer = new RingBuffer(1000)

          // 4. 监听新的 Port 连接
          setupPortListener()
      }
  })

  Content script 重连后:
    → 从 chrome.storage 读取 last_seq
    → 重新发送 last_seq 之后的事件 (content script 侧有缓冲)
```

### 2.4 事件去重

SW 重启后 content script 可能重发已推送的事件。去重策略：

```
HTTP Server 端去重:
  收到 POST /api/session/:id/events { events: [...], batch_seq: 42 }

  1. 读取 meta.json 的 last_batch_seq
  2. if batch_seq <= last_batch_seq: return 200 (幂等, 已处理)
  3. 写入事件到 .jsonl
  4. 更新 meta.json last_batch_seq = batch_seq

Content script 端:
  每个事件有 sequence 号。SW 定期发送 ack (HTTP response 中返回 last_sequence)。
  Content script 记录 last_acked_seq，重连时仅重发未确认的事件。
```

---

## 3. Content Script → Background SW 消息协议

### 3.1 Port 连接

```
Content Script:
  const port = chrome.runtime.connect({ name: "behavior-events" })

  // 每个 Port 对应一个 tab
  // 一个 session 可能有多个 Port (多 tab 录制)
```

### 3.2 消息类型

| 方向 | type | payload | 说明 |
|------|------|---------|------|
| CS → SW | `event` | `BehaviorEvent` | 单条 DOM 事件 |
| CS → SW | `page_meta` | `{url_domain, url_path, tab_id}` | 页面元信息，page.load 时发送 |
| CS → SW | `auth_detected` | `AuthDetectionResult` | Auth 页面检测结果 |
| SW → CS | `ack` | `{last_seq}` | 确认已收到的事件序号 |
| SW → CS | `pause` | `{reason}` | SW 要求暂停采集 (auth pause) |
| SW → CS | `resume` | `{}` | SW 要求恢复采集 |
| SW → CS | `session_end` | `{}` | Session 结束，CS 断开 Port |

### 3.3 背压处理

```
Content Script 端:
  - 事件采集速度: 峰值 ~3 events/s (填表场景)
  - SW → CS ack 间隔: 每 500ms 返回 last_seq

  - 如果 CS 连续 10 秒未收到 ack (SW 过载/网络慢):
    → 本地缓冲开始积累 (上限 500 events)
    → 新事件继续入缓冲
    → 收到 ack 后批量清空

  - 如果 CS 本地缓冲达到 500:
    → 最旧的事件被丢弃
    → 在下一个 page.load 事件中标记 dropped_count
```

### 3.4 序列号保证

```
每条事件携带:
  session_id: "abc123"
  tab_seq: 5          ← 此 tab 内的递增序号
  session_seq: 234    ← 全局 session 内的递增序号 (由 SW 分配)

SW 收到事件后:
  - 按到达时间分配 session_seq (全局递增)
  - 记录到 ack 消息中
  - 保证 .jsonl 中的事件按 session_seq 有序
```

---

## 4. 多 Tab 并发写入

### 4.1 场景

用户同时打开 github.com (tab A) 和 jira.example.com (tab B)，两个 tab 都在同一 session 录制中。Content script A 和 B 各自捕获事件，通过各自的 Port 发送给 SW。

### 4.2 SW 层序列化

```
SW 是单线程 Event-driven:
  Port A.onMessage → push to Ring Buffer
  Port B.onMessage → push to Ring Buffer
  (JavaScript 单线程保证不会并发修改 Ring Buffer)

  顺序: 按消息到达 SW 的时间分配 session_seq
  结果: 两个 tab 的事件在 .jsonl 中按到达时间交错排列 (这是正确的)
```

### 4.3 HTTP Server 层并发

两个独立的 HTTP 请求可能同时到达（HTTP 是多 goroutine 并发的）：

```
Goroutine A: POST /api/session/abc/events (batch_seq=5, tab A 的事件)
Goroutine B: POST /api/session/abc/events (batch_seq=6, tab B 的事件)

保护:
  server 持有 map[string]*sync.Mutex  (per session)
  → 同 session 的写入被串行化

Goroutine B 等待 A 释放锁:
  A 获取锁 → 写 .jsonl → 更新 meta → 释放锁
  B 获取锁 → 写 .jsonl → 更新 meta → 释放锁

最终 .jsonl:
  行1: tab A 事件 (batch 5)
  行2: tab B 事件 (batch 6)
  (按 batch_seq 排序是因为 SW 已序列化好了 batch_order)
```

### 4.4 文件锁细节

```go
// HTTP server 内 per-session 文件写入
type SessionWriter struct {
    mu       sync.Mutex
    fd       *os.File        // .jsonl, 追加模式打开
    metaPath string
}

func (w *SessionWriter) Append(events []Event) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    // 批量写入: 单次 writev 调用保证整批原子
    // (单行 < PIPE_BUF, 但批量需要显式保护)
    var buf bytes.Buffer
    for _, e := range events {
        line, _ := json.Marshal(e)
        buf.Write(line)
        buf.WriteByte('\n')
    }
    if _, err := w.fd.Write(buf.Bytes()); err != nil {
        // write 失败: buf 中部分可能已写入
        // 但每条事件是独立 JSON 行，读时会丢弃最后不完整的行
        return err
    }
    return nil
}
```

---

## 5. HTTP Server 崩溃恢复

### 5.1 崩溃场景分析

| 崩溃时机 | 后果 | 恢复策略 |
|---------|------|---------|
| 写入 .jsonl 行中途 | 最后一行不完整 JSON | 读取时跳过不完整行 |
| 写入 meta.json 前崩溃 | meta 记录的 event_count < 实际 .jsonl 行数 | 启动时重新计数 |
| 写 meta.tmp → rename 之间崩溃 | meta.json 保持旧值 | 下次重启重写 |
| Server 进程被 kill -9 | 同上 + 可能丢内存中的写缓冲 | fsync 间隔控制丢数据量 |

### 5.2 启动恢复流程

```
Server 启动时:

  for each session_dir in data_dir:
    meta = read meta.json (如果存在)
    jsonl = stat .jsonl 文件

    if jsonl 存在但 meta 不存在:
      → 重建 meta: 扫描 .jsonl 行数, 提取 domain 列表, 标记 status="aborted"

    if meta.status == "recording" 或 "paused":
      → 这是上次运行时未正常结束的 session
      → 标记 meta.status = "aborted"
      → 写入 meta

    if meta.event_count != countLines(jsonl):
      → 纠正 meta.event_count = countLines(jsonl)

  // 清理过期 session (retention_days)
```

### 5.3 fsync 策略

```
Append 事件后不立即 fsync (性能优化):
  - 每 30 秒进行一次 fsync (定时器)
  - 或每累积 200 条未 fsync 的事件后 fsync
  - session_end 时强制 fsync

  丢失窗口: 最多丢 30 秒内写入的事件
  权衡: 对于行为录制场景，丢 30 秒低价值事件 vs 频繁 fsync 的磁盘开销

  meta.json 更新:
  - 不需要每次事件写入都更新 meta
  - session 活跃期间 event_count 可以落后于实际行数
  - session_end 时强制同步 meta
```

---

## 6. HTTP Server 实现要点

### 6.1 技术选型

Go 标准库，零外部依赖。约 200 行代码。

```go
// cmd/behavior-server/main.go 结构

// 不需要任何 import 超过标准库:
// "encoding/json"
// "fmt"
// "net/http"
// "os"
// "path/filepath"
// "sync"
// "time"
// "bufio"
```

### 6.2 关键结构

```go
type Server struct {
    dataDir    string                          // data_dir (configurable)
    sessions   map[string]*SessionWriter       // session_id → writer
    sessionsMu sync.RWMutex
}

type SessionWriter struct {
    mu       sync.Mutex
    fd       *os.File         // .jsonl 文件句柄
    metaPath string           // .meta.json 路径
    meta     SessionMeta
    unsynced int              // 未 fsync 的事件数
}

func (s *Server) appendEvents(sessionID string, events []Event) error {
    s.sessionsMu.RLock()
    w := s.sessions[sessionID]
    s.sessionsMu.RUnlock()

    if w == nil {
        return ErrSessionNotFound
    }

    if err := w.Append(events); err != nil {
        return err
    }

    // 异步 fsync (goroutine pool 或计数器触发)
    w.unsynced += len(events)
    if w.unsynced >= 200 {
        w.fd.Sync()
        w.unsynced = 0
    }

    return nil
}
```

### 6.3 定时 fsync

```go
// Server 启动时启动一个 goroutine
func (s *Server) fsyncLoop() {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        s.sessionsMu.RLock()
        for _, w := range s.sessions {
            w.mu.Lock()
            if w.unsynced > 0 {
                w.fd.Sync()
                w.unsynced = 0
            }
            w.mu.Unlock()
        }
        s.sessionsMu.RUnlock()
    }
}
```

---

## 7. Chrome Extension 设计

### 7.0 分发模式

Unpacked extension，随仓库 `extension/` 目录分发。`chrome://extensions` 开发者模式加载。

### 7.1 组件结构

```
extension/
  manifest.json
  background/
    service-worker.js         # Ring Buffer, Port 管理, HTTP 推送, 恢复
  content/
    observer.js               # DOM 事件委托 + Port 连接 + 本地缓冲
    selector-builder.js       # CSS 选择器生成
    spa-detector.js           # SPA 导航检测
    auth-detector.js          # Auth 页面检测
  popup/
    popup.html / popup.js     # 录制控制, session 列表, 状态
  options/
    options.html / options.js # 域名过滤, 隐私, server 地址
```

### 7.2 Manifest V3

```json
{
  "manifest_version": 3,
  "name": "Fox",
  "version": "0.1.0",
  "description": "捕获浏览器操作行为，供 Dolphin AI agent 分析并自动生成 skill",
  "permissions": ["storage", "activeTab", "scripting", "tabs", "webNavigation"],
  "host_permissions": ["<all_urls>", "http://127.0.0.1:9200/*"],
  "background": { "service_worker": "background/service-worker.js" },
  "content_scripts": [{
    "matches": ["<all_urls>"],
    "js": ["content/observer.js", "content/selector-builder.js", "content/spa-detector.js", "content/auth-detector.js"],
    "run_at": "document_idle"
  }],
  "action": { "default_popup": "popup/popup.html" },
  "options_page": "options/options.html"
}
```

### 7.3 录制状态机

```
                    ┌──────────┐
        popup 点击   │          │  popup 点击 / session_end
   ┌───────────────→│ STOPPED  │←─────────────────────┐
   │                │ (默认)    │                      │
   │                └─────┬────┘                      │
   │       popup 点击     │                           │
   │  ┌───────────────────┘                           │
   │  │                                               │
   ▼  ▼                                               │
┌──────────┐   popup 点击    ┌──────────┐            │
│RECORDING │←───────────────→│  PAUSED   │            │
│(采集+推送)│                 │(缓冲不推送)│            │
└────┬─────┘                 └──────────┘            │
     │                                               │
     │ 检测到 auth 页面                               │
     ▼                                               │
┌────────────┐  离开 auth 页   ┌──────────┐         │
│AUTH_PAUSED │───────────────→│RECORDING  │─────────┘
└────────────┘                 └──────────┘
```

### 7.4 HTTP API 调用时序

```
Popup 点击 "开始录制":
  SW → POST /api/session/start  { session_id, chrome_info, timestamp }
  SW → chrome.storage.local.set({ active_session: session_id })
  SW → 向所有已注入 CS 的 tab 的 Port 发送 start 消息
  SW → 向 Popup 发送 status 更新

录制中 (每 500ms 或 50 条):
  SW → POST /api/session/{id}/events  { events: [...], batch_seq: N }
  SW ← HTTP 200 { last_sequence: 234 }

Popup 点击 "停止录制":
  SW → flush 缓冲中所有事件
  SW → POST /api/session/{id}/end  { timestamp }
  SW → chrome.storage.local.remove('active_session')
  SW → 向所有 CS Port 发送 session_end
  SW → 向 Popup 发送 session 完成
```

### 7.5 Ring Buffer 与推送

```
Ring Buffer (JS Array, 1000 容量, head/tail 指针)

内容: BehaviorEvent 对象 (已预先序列化? 否——保持对象以支持隐私过滤)

Flush 触发:
  - buffer.length >= 50
  - 距上次 flush >= 500ms (setInterval)
  - 接收到 stop 指令

Flush 流程:
  1. pop [head..tail] → batch[]
  2. 隐私过滤 (L1 → L2 → L3)
  3. JSON.stringify({ events: batch, batch_seq: nextBatchSeq })
  4. fetch('POST .../events', { body })
  5. 收到 200 → 更新 last_seq, 清空 batch
  6. 收到 error → 保留 batch 在 buffer, 等待重试
```

---

## 8. 数据模型

### 8.1 BehaviorEvent (JSON Lines 格式)

```typescript
// 每行一个 JSON 对象，使用短 key
interface BehaviorEvent {
  s: string;     // session_id
  seq: number;   // sequence (由 SW 分配的全局序号)
  ts: string;    // timestamp ISO8601
  tab: number;   // tab_id
  domain: string; // url_domain
  path: string;   // url_path
  type: string;   // event_type
  p: Payload;     // payload
}
```

### 8.2 事件类型

| event_type | Payload 关键字段 | 节流 |
|-----------|-----------------|------|
| `page.load` | url_path, is_spa, navigation_source | 无 |
| `click`, `dblclick` | el, text, sel, m, alts[], pos{x,y} | 无 |
| `input` | el, input_type, sel, value_length (**不记录值**) | 300ms |
| `change` (select) | el, selected_option_text | 无 |
| `focus`, `blur` | el, sel | 无 |
| `submit` | form_action_domain, field_count | 无 |
| `scroll` | scroll_pct | 500ms |
| `keydown` | key_combo (**仅含修饰键**) | 无 |
| `tab.activated`, `tab.created`, `tab.removed` | tab_id, url_domain | 无 |
| `window.focus`, `window.blur` | window_id | 无 |
| `recording.auto_paused` | reason | 无 |
| `recording.resumed` | reason | 无 |
| `oauth.flow_start` | start_domain | 无 |
| `oauth.flow_completed` | duration_ms, redirect_count | 无 |

### 8.3 Click Payload

```json
{
  "el": "button",
  "text": "Create Pull Request",
  "sel": "[data-testid=\"create-pr-btn\"]",
  "m": "data-testid",
  "alts": [
    {"s": "button[aria-label=\"Create pull request\"]", "m": "aria-label", "st": 0.9},
    {"s": "#new-pull-request > button", "m": "id+class", "st": 0.6}
  ],
  "pos": {"x": 850, "y": 320}
}
```

---

## 9. 隐私设计

### 9.1 三层过滤

```
Layer 1 — 硬阻断 (始终生效, content script 层面)
  ❌ input[type="password"] → 跳过此元素
  ❌ 信用卡号 → Luhn 校验
  ❌ 邮箱地址 → regex
  ❌ 跨域 iframe → 不注入 content script

Layer 2 — 域名过滤 (用户可配置, SW 层面)
  默认: blocklist (空)
  模式: allowlist | blocklist | off

Layer 3 — 内容脱敏 (始终生效, SW flush 时处理)
  - input 值 → 仅记录 value_length
  - 元素文本 → 截断到 100 字符
  - URL → 剥离 query params 和 hash
  - 键盘 → 仅记录含修饰键的组合
```

### 9.2 Auth 页面自动暂停

5 信号检测 (URL 路径 + 密码字段 + login form + OAuth 按钮 + 页面标题)。任一 ≥ 0.75 → AUTH_PAUSED。离开后恢复。

---

## 10. 用户交互

### 10.1 Popup

```
+------------------------------------------+
|  Fox                        |
+------------------------------------------+
|  Server: ● 已连接                        |
+------------------------------------------+
|   ● RECORDING    12:34                   |
|   [⏸ Pause]  [⏹ Stop]                   |
|                                          |
|   github.com · jira.example.com          |
|   事件: 234  |  批次: 6                  |
|                                          |
|   ─── Sessions ────────────────────────  |
|   github.com · 今天 10:30 · 45min        |
|     [查看] [删除]                        |
|                                          |
|   localhost:3000 · 昨天 · 12min          |
|     [查看] [删除]                        |
|                                          |
|   ─── 分析 ────────────────────────────  |
|   [📋 复制 agent 分析 Prompt]            |
+------------------------------------------+
```

### 10.2 Agent 分析 Prompt

```
请分析我的浏览器录制数据:

文件: {data_dir}/{session_id}.jsonl (配置项 data_dir 指定)

格式: JSON Lines (每行一个事件)

要求:
1. 识别重复 ≥ 3 次的操作序列 (按域名+页面分组)
2. 提炼为可泛化的自然语言 skill
3. 设置 enabled: false，我审核后再启用
4. 与已有 skill 去重
5. 汇总创建了哪些 skill
```

---

## 11. 配置

```yaml
# behavior-server 独立配置
server:
  addr: "127.0.0.1:9200"
  data_dir: "experiment/fox/data/sessions"
  max_sessions: 50
  retention_days: 7
  fsync_interval_sec: 30
  fsync_min_events: 200
```

---

## 12. 定量分析

| 指标 | 值 |
|------|-----|
| 1h session 事件数 | 300-1300 |
| 1h session 文件大小 | 55-220 KB (.jsonl) |
| 单条事件大小 | 200-500 bytes |
| 50 sessions 存储 | 3-11 MB |
| HTTP POST 批大小 | ~17.5 KB (50 events) |
| 推送频率 | ~1 batch/s 峰值 |
| 崩溃丢数据窗口 | ≤ 30s / ≤ 200 events (可配置) |
| Content script 内存 | ~300 KB |
| SW 内存 | ~2 MB |
| Server 内存 | ~5 MB |

---

## 13. 项目文件结构

```
experiment/fox/
  design.md                       # 本设计文档
  extension/                      # Chrome 扩展
    manifest.json
    background/service-worker.js
    content/observer.js, selector-builder.js, spa-detector.js, auth-detector.js
    popup/popup.html + popup.js
    options/options.html + options.js
  cmd/behavior-server/            # HTTP server (独立, 非 Dolphin 依赖)
    main.go                       # ~200 行, 仅标准库
  data/sessions/                  # 默认数据目录 (server --data-dir 可改)
    {id}.jsonl                    # 事件
    {id}.meta.json                # 元数据
```

---

## 14. 设计决策记录

| # | 决策 | 选择 | 理由 |
|---|------|------|------|
| 1 | 存储格式 | JSON Lines (.jsonl) | 追加 O(1), 崩溃恢复, LLM 可读 |
| 2 | SW 存活 | Long-lived Port | MV3 原生支持, 无需额外权限 |
| 3 | 事件序列化 | SW 分配 session_seq | 单线程保证多 tab 事件有序 |
| 4 | Server 并发 | per-session sync.Mutex | 同 session 串行, 不同 session 并行 |
| 5 | 原子写入 | 单行 write + meta rename | POSIX 保证 < PIPE_BUF 写入原子 |
| 6 | fsync | 30s / 200 events 触发 | 平衡性能与数据安全 |
| 7 | 崩溃恢复 | 启动时扫描 + 纠正 meta | 自动化, 无需手动干预 |
| 8 | 去重 | 幂等 batch_seq 检查 | 防止 SW 重启后重复写入 |
| 9 | 短 key | s/seq/ts/tab 等 | 文件体积减 30% + LLM token 节省 |

---

## 15. 后续演进

- **WebSocket 实时推送**: behavior-server 增加 WS 端点，Dolphin agent 可订阅实时事件流
- **扩展内预检测**: 简单统计算法检测高频序列，Popup 中给出 hint
- **多次 session 合并分析**: 跨 session 模式挖掘 (需 agent 端提示词优化)

<!-- last-modified: 2026-06-26 -->
