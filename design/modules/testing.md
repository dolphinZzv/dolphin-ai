# Testing

## 设计原则

- **覆盖率 ≥ 70%** — 单元测试覆盖所有包
- **gock mock HTTP** — LLM Provider 等 HTTP 调用用 gock 拦截，不 mock 接口
- **InMemory 实现** — Memory、Session 等用内存实现测试，不依赖外部存储
- **Mock TransportIO** — TransportIO 用 `mockTransport` 测试 pipeline 流程

## LLM Provider 测试

LLM 调用是 HTTP POST，用 gock 拦截，不启动真实服务：

```go
import "github.com/h2non/gock"

func TestLLMComplete(t *testing.T) {
    defer gock.Off()

    // 拦截 LLM HTTP 请求，返回 mock 流式响应
    gock.New("https://api.openai.com").
        Post("/v1/chat/completions").
        Reply(200).
        BodyString(`data: {"choices":[{"delta":{"content":"hello"}}]}` + "\n\ndata: [DONE]")

    provider := NewProvider(Config{APIKey: "test-key", BaseURL: "https://api.openai.com"})
    chunks, err := provider.CompleteStream(ctx, req)
    // assert chunks, err
}
```

### 测试场景

| 场景 | gock 行为 |
|------|----------|
| 正常流式 | 返回多行 `data: {...}` → `data: [DONE]` |
| 网络超时 | gock 延迟后返回 error |
| HTTP 错误 | `Reply(429)` 限流 / `Reply(500)` 服务端错误 |
| 空响应 | 立即返回 `data: [DONE]` |
| 部分 chunk 后断连 | 发几条后连接中断 |

## InMemory 实现

Memory、Session 等接口提供内存实现，测试中直接使用：

```go
type InMemoryStore struct {
    mu    sync.Mutex
    data  map[string][]Message
}

func (m *InMemoryStore) Read(ctx context.Context, sessionID string) ([]Message, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    return append([]Message{}, m.data[sessionID]...), nil  // 返回副本
}

func (m *InMemoryStore) Write(ctx context.Context, sessionID string, msg Message) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.data[sessionID] = append(m.data[sessionID], msg)
    return nil
}
```

## 覆盖率要求

- **总覆盖率 ≥ 70%**（`go test -cover`）
- 核心模块目标：

| 包 | 目标 | 策略 |
|----|------|------|
| `llm` | ≥ 80% | gock 覆盖所有 HTTP 场景 |
| `agent` | ≥ 75% | mock LLM + InMemory Memory |
| `session` | ≥ 80% | InMemory SessionStore |
| `config` | ≥ 90% | 各种 YAML 组合测试校验 |
| `transport` | ≥ 70% | mockTransport 模拟 stream/chunk |
| `memory` | ≥ 80% | InMemory + FileMemory 临时目录 |
| `signal` | ≥ 85% | 纯逻辑，无外部依赖 |
| `tool` | ≥ 70% | mock 工具执行 |
| `command` | ≥ 60% | cobra 命令表测试 |

## 覆盖率验证

```makefile
# Makefile
test:
	go test ./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out

test-coverage-check:
	go test ./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out | grep total | awk '{print $$NF}' | \
		while read pct; do \
			val=$${pct%\%}; \
			if [ "$$(echo "$$val < 70" | bc)" -eq 1 ]; then \
				echo "FAIL: coverage $$val% < 70%"; exit 1; \
			fi; \
		done
```

CI 中 `test-coverage-check` 失败则不允许合并。

## Lint

使用 `golangci-lint`，配置在项目根目录 `.golangci.yml`：

```yaml
# .golangci.yml
linters:
  enable:
    - gofmt
    - govet
    - errcheck
    - staticcheck
    - gosimple
    - ineffassign
    - misspell
    - revive

run:
  timeout: 3m
  issues-exit-code: 1

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - errcheck
```

```makefile
lint:
	golangci-lint run ./... -v

ci: lint test-coverage-check
```

CI 执行顺序：lint → test → coverage-check。

## Mock TransportIO

测试 pipeline 流程时用 `mockTransport` 模拟双向 IO：

```go
type mockTransport struct {
    id      string
    readBuf []string
    writeBuf []string
}

func (m *mockTransport) ID() string { return m.id }
func (m *mockTransport) Read(ctx context.Context) (string, error) {
    if len(m.readBuf) == 0 {
        return "", io.EOF
    }
    s := m.readBuf[0]
    m.readBuf = m.readBuf[1:]
    return s, nil
}
func (m *mockTransport) Write(ctx context.Context, text string) error {
    m.writeBuf = append(m.writeBuf, text)
    return nil
}
```

## 单元测试结构

```
internal/
  llm/
    llm_test.go       ← gock mock HTTP
  memory/
    memory_test.go    ← InMemory 作为 baseline
  transport/
    transport_test.go ← mockTransport
  agent/
    loop_test.go      ← mock LLM + InMemory + mock Transport
  session/
    session_test.go   ← InMemory SessionStore
```

关键原则：不 mock 自身接口，只 mock 外部依赖（HTTP 用 gock，数据库用 InMemory）。

<!-- last-modified: 2026-05-29 -->
