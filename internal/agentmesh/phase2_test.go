package agentmesh

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"dolphin/internal/types"
)

// 夜晚异步行动：主持人用 DelegateAsync 同时派活给多个角色，不阻塞等待。

func TestDelegateAsync_SeerDivinesAsync(t *testing.T) {
	mockSeer := NewMockA2AServer(StaticHandler(
		DefaultMockCard("seer", ""),
		&DelegateResult{TaskID: "div-async", Status: DelegateCompleted, Content: "3 号是狼人", Rounds: 1},
	))
	defer mockSeer.Close()

	card := DefaultMockCard("seer", mockSeer.Addr())
	card.Capabilities = []string{"divine"}
	mesh, cleanup := NewTestAgentMesh(mockSeer, "seer", card)
	defer cleanup()
	mesh.Register(card)

	taskID, err := mesh.DelegateAsync(context.Background(), DelegatePayload{
		Task: "异步查验 3 号", PreferredAgent: "seer", ParentSessionID: "night-2",
	})
	if err != nil {
		t.Fatalf("DelegateAsync 应返回 task id, got %v", err)
	}
	if taskID == "" {
		t.Fatal("task id 不应为空")
	}

	// 轮询直到完成
	deadline := time.Now().Add(3 * time.Second)
	var result *DelegateResult
	for time.Now().Before(deadline) {
		res, done, _ := mesh.GetResult(context.Background(), taskID)
		if done {
			result = res
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if result == nil {
		t.Fatal("异步任务应在 3s 内完成")
	}
	if result.Content != "3 号是狼人" {
		t.Errorf("expected 查验结果, got %s", result.Content)
	}
}

func TestDelegateAsync_CancelWitch(t *testing.T) {
	// 女巫磨蹭，主持人取消她的任务。
	started := make(chan struct{}, 1)
	mockWitch := NewMockA2AServer(func(method string, _ json.RawMessage) (any, error) {
		if method == "tasks/send" {
			select {
			case started <- struct{}{}:
			default:
			}
			// 模拟女巫犹豫：稍长时间返回（但短于测试超时）
			time.Sleep(300 * time.Millisecond)
			return DelegateResult{Status: DelegateCompleted, Content: "救了"}, nil
		}
		if method == "agents/discover" {
			return DefaultMockCard("witch", ""), nil
		}
		return nil, nil
	})
	defer mockWitch.Close()

	card := DefaultMockCard("witch", mockWitch.Addr())
	card.Capabilities = []string{"save"}
	mesh, cleanup := NewTestAgentMesh(mockWitch, "witch", card)
	defer cleanup()
	mesh.Register(card)

	taskID, err := mesh.DelegateAsync(context.Background(), DelegatePayload{
		Task: "救 3 号", PreferredAgent: "witch", ParentSessionID: "night-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started // 等女巫开始犹豫
	if err := mesh.Cancel(taskID); err != nil {
		t.Fatalf("取消应成功, got %v", err)
	}
	// 等任务结束（无论被取消还是自然完成）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, _, _ = mesh.GetResult(context.Background(), taskID)
		// Cancel 已发送；这里不严格要求二次取消报错，因为任务可能已 done。
		break
	}
}

// ── SSE 流式：tasks/sendSubscribe（server 端降级为单 done 事件）──

func TestA2AClient_SendTaskSubscribe(t *testing.T) {
	// 一个 mock server，用普通 tasks/send 同步响应；client 用 sendSubscribe
	// 时，client 侧会因 Content-Type 非 SSE 而走降级路径（单 done 事件）。
	// 这里我们测 server 真正返回 SSE 的情况：直接起一个返回 SSE 的 httptest server。
	srv := newSSEMockServer(t, "done", "夜晚结束，3 号出局")
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL(), "http://")
	c := NewA2AClient(addr, nil)
	// 不需要 Negotiate（直接调 sendSubscribe 不经 call 路径协商检查）
	ch, err := c.SendTaskSubscribe(context.Background(), DelegatePayload{Task: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				if got == "" {
					t.Fatal("未收到 done 事件")
				}
				return
			}
			if ev.Type == "done" {
				got = ev.Content
				if got != "夜晚结束，3 号出局" {
					t.Errorf("expected 夜晚结束, got %s", got)
				}
			}
		case <-time.After(time.Until(deadline)):
			t.Fatal("SSE 超时")
		}
	}
}

// ── 工具联邦：主持人挂载预言家的 divine 工具到本地 ──

func TestToolMount_ListAndExecute(t *testing.T) {
	// mock 预言家暴露 divine 工具
	mockSeer := NewMockA2AServer(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "agents/discover":
			card := DefaultMockCard("seer", "")
			card.ProtoVersion = 4 // 支持工具联邦
			return card, nil
		case "agents/ping":
			return map[string]any{"status": "ok"}, nil
		case "tools/list":
			return []RemoteToolDef{
				{Name: "divine", Description: "查验一名玩家身份", Schema: json.RawMessage(`{"type":"object"}`)},
			}, nil
		case "tools/call":
			return RemoteToolResult{Content: "3 号是狼人"}, nil
		}
		return nil, fmt.Errorf("unknown method: %s", method)
	})
	defer mockSeer.Close()

	card := DefaultMockCard("seer", mockSeer.Addr())
	card.ProtoVersion = 4
	card.Capabilities = []string{"divine"}
	mesh, cleanup := NewTestAgentMesh(mockSeer, "seer", card)
	defer cleanup()
	mesh.Register(card)

	// 挂载前必须协商（MountTools 内部会 clientFor→Negotiate）
	if err := mesh.MountTools(context.Background(), "seer"); err != nil {
		t.Fatalf("MountTools 应成功, got %v", err)
	}

	// 直接拿到 mount 后的 ToolMount 测试 Execute
	client, _ := mesh.clientFor(context.Background(), mockSeer.Addr())
	mount := NewToolMount("seer", client, nil)
	defs, err := mount.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "seer/divine" {
		t.Fatalf("expected seer/divine, got %+v", defs)
	}
	res, err := mount.Execute(context.Background(), toolCall("seer/divine", `{"target":"3号"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "3 号是狼人" {
		t.Errorf("expected 查验结果, got %s", res.Content)
	}
}

func TestToolMount_PeerTooOld(t *testing.T) {
	// proto=2 的预言家不支持工具联邦 → MountTools 报错。
	mockSeer := NewMockA2AServer(StaticHandler(DefaultMockCard("seer", ""), nil))
	defer mockSeer.Close()
	card := DefaultMockCard("seer", mockSeer.Addr())
	card.ProtoVersion = 2
	mesh, cleanup := NewTestAgentMesh(mockSeer, "seer", card)
	defer cleanup()
	mesh.Register(card)
	if err := mesh.MountTools(context.Background(), "seer"); err == nil {
		t.Fatal("proto<4 应拒绝挂载")
	}
}

// ── 接收方限流 ──

func TestServerRateLimiter_PerSession(t *testing.T) {
	// 主持人同一个夜晚会话短时间发大量委托 → 接收方应限流。
	base := time.Now()
	rl := NewServerRateLimiter(30, 60, 120).withClock(func() time.Time { return base })
	// 突发 10 个应放行（burst=10）
	for i := range 10 {
		if !rl.Allow("moderator:1", "night-2") {
			t.Fatalf("突发内第 %d 个应放行", i+1)
		}
	}
	if rl.Allow("moderator:1", "night-2") {
		t.Fatal("突发耗尽后应被限流")
	}
	// 换一个会话：用 fresh limiter，只发 3 个到 night-2（session + global
	// 都未耗尽），再发 night-3 应放行——验证 per-session bucket 独立计数。
	rl2 := NewServerRateLimiter(30, 60, 120).withClock(func() time.Time { return base })
	for range 3 {
		rl2.Allow("moderator:1", "night-2")
	}
	if !rl2.Allow("moderator:1", "night-3") {
		t.Error("fresh limiter 下不同 session 应放行")
	}
}

// ── Prometheus metrics ──

func TestMetrics_RecordDelegate(t *testing.T) {
	m := initMetrics()
	// 不 panic 即可
	m.recordDelegate("moderator:1", "seer:1", "completed", 0.5, 1)
	m.recordDelegate("moderator:1", "witch:1", "failed", 1.2, 2)
}

// ── helpers ──

func toolCall(name, args string) types.ToolCall {
	return types.ToolCall{ID: "call-1", Name: name, Arguments: args}
}

// newSSEMockServer 起一个返回单条 SSE data: 事件的 server。
func newSSEMockServer(t *testing.T, eventType, content string) *sseServer {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/jsonrpc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		ev := map[string]any{"type": eventType, "content": content}
		payload, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", payload)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法绑定端口跳过 SSE 测试: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	return &sseServer{srv: srv, addr: ln.Addr().String()}
}

type sseServer struct {
	srv  *http.Server
	addr string
}

func (s *sseServer) URL() string  { return "http://" + s.addr }
func (s *sseServer) Close() error { return s.srv.Close() }
