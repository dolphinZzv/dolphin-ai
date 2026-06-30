package agentmesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// 场景：主持人(moderator)通过 delegate_to_agent 指挥夜晚各角色行动。
// 每个角色是一个 mock A2A server。

func TestDelegate_Sync_SeerDivines(t *testing.T) {
	// 主持人委托预言家查验 3 号玩家，预言家返回「狼人」。
	mockSeer := NewMockA2AServer(StaticHandler(
		AgentCard{Name: "seer", Addr: "", Capabilities: []string{"divine"}, Status: AgentRunning, MaxLoad: 5, ProtoVersion: 2},
		&DelegateResult{TaskID: "divine-1", Status: DelegateCompleted, Content: "3 号玩家是狼人", Rounds: 2},
	))
	defer mockSeer.Close()

	card := DefaultMockCard("seer", mockSeer.Addr())
	card.Capabilities = []string{"divine"}
	mesh, cleanup := NewTestAgentMesh(mockSeer, "seer", card)
	defer cleanup()

	// 把 mock 地址同步进注册表卡片（StaticHandler 的 card.Addr 为空，由 clientFor 协商时填）。
	regCard := card
	regCard.Addr = mockSeer.Addr()
	mesh.Register(regCard)

	result, err := mesh.Delegate(context.Background(), DelegatePayload{
		Task:            "查验 3 号玩家的身份",
		PreferredAgent:  "seer",
		ParentSessionID: "night-1",
	})
	if err != nil {
		t.Fatalf("主持人委托预言家应成功, got %v", err)
	}
	if result.Status != DelegateCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
	if result.Content != "3 号玩家是狼人" {
		t.Errorf("expected 查验结果, got %s", result.Content)
	}

	// 应记录 parent→child session link
	links := 0
	_ = links
	if _, ok := mesh.GetLink("night-1.dlg.divine-1"); !ok {
		t.Error("应记录 session link (night-1.dlg.divine-1)")
	}
}

func TestDelegate_Timeout_WitchSlow(t *testing.T) {
	// 女巫磨蹭太久，主持人等不及 → 超时。
	mockWitch := NewMockA2AServer(func(method string, _ json.RawMessage) (any, error) {
		if method == "tasks/send" {
			time.Sleep(2 * time.Second) // 女巫犹豫不决
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
	regCard := card
	regCard.Addr = mockWitch.Addr()
	mesh.Register(regCard)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := mesh.Delegate(ctx, DelegatePayload{
		Task:            "用解药救 3 号",
		PreferredAgent:  "witch",
		ParentSessionID: "night-1",
	})
	if err == nil {
		t.Fatal("女巫超时应返回错误")
	}
	// 超时映射为 ErrTimeout（也可能是底层 context 错误被包装）
	var dErr *DelegateError
	if errors.As(err, &dErr) && dErr.Code != ErrTimeout {
		t.Errorf("expected ErrTimeout, got %v", dErr.Code)
	}
}

func TestDelegate_CircuitBreaker_WerewolfDown(t *testing.T) {
	// 狼人 agent 持续返回内部错误，连续失败后熔断。
	var calls atomic.Int32
	mockWolf := NewMockA2AServer(func(method string, _ json.RawMessage) (any, error) {
		if method == "tasks/send" {
			calls.Add(1)
			return nil, fmt.Errorf("internal error: 狼人窝炸了")
		}
		if method == "agents/discover" {
			return DefaultMockCard("werewolf", ""), nil
		}
		return nil, nil
	})
	defer mockWolf.Close()

	card := DefaultMockCard("werewolf", mockWolf.Addr())
	card.Capabilities = []string{"kill"}
	// 把熔断阈值设小，便于触发
	mesh, cleanup := NewTestAgentMesh(mockWolf, "werewolf", card, func(c *AgentConfig) {
		c.CircuitBreaker.FailureThreshold = 3
		c.CircuitBreaker.CooldownPeriod = 1 * time.Hour // 测试期间不进入半开
		c.Retry.MaxRetries = 0
	})
	defer cleanup()
	regCard := card
	regCard.Addr = mockWolf.Addr()
	mesh.Register(regCard)

	// 连续 3 次委托失败 → 熔断
	for i := range 3 {
		_, err := mesh.Delegate(context.Background(), DelegatePayload{
			Task: "夜杀 5 号", PreferredAgent: "werewolf", ParentSessionID: "night-1",
		})
		if err == nil {
			t.Fatalf("第 %d 次应失败", i+1)
		}
	}
	// 第 4 次应被熔断直接拒绝（不再发 HTTP）
	before := calls.Load()
	_, err := mesh.Delegate(context.Background(), DelegatePayload{
		Task: "再杀一个", PreferredAgent: "werewolf", ParentSessionID: "night-1",
	})
	if err == nil {
		t.Fatal("熔断后应拒绝")
	}
	var dErr *DelegateError
	if errors.As(err, &dErr) && dErr.Code != ErrAgentUnavail {
		t.Errorf("expected ErrAgentUnavail (熔断), got %v", dErr.Code)
	}
	after := calls.Load()
	if after != before {
		t.Errorf("熔断后不应再发起 HTTP 调用, calls before=%d after=%d", before, after)
	}
}

func TestDelegate_Fallback_SeerDeadWitchTakesOver(t *testing.T) {
	// 主持人需要「查验」能力。首选预言家已死(返回 unavailable)，
	// fallback 到备选预言家（另一个 seer2）。
	primary := NewMockA2AServer(func(method string, _ json.RawMessage) (any, error) {
		if method == "tasks/send" {
			return nil, fmt.Errorf("connection refused: 预言家已死")
		}
		if method == "agents/discover" {
			return DefaultMockCard("seer", ""), nil
		}
		return nil, nil
	})
	defer primary.Close()

	backup := NewMockA2AServer(StaticHandler(
		DefaultMockCard("seer2", ""),
		&DelegateResult{TaskID: "divine-backup", Status: DelegateCompleted, Content: "备选预言家：3 号是狼人", Rounds: 1},
	))
	defer backup.Close()

	mesh, cleanup := NewTestAgentMesh(primary, "seer", DefaultMockCard("seer", primary.Addr()))
	defer cleanup()
	mesh.Register(AgentCard{
		Name: "seer", Addr: primary.Addr(), Capabilities: []string{"divine"},
		Status: AgentRunning, MaxLoad: 5, ProtoVersion: 2,
	})
	mesh.Register(AgentCard{
		Name: "seer2", Addr: backup.Addr(), Capabilities: []string{"divine"},
		Status: AgentRunning, MaxLoad: 5, ProtoVersion: 2,
	})

	// 不指定 preferred，按能力匹配 → 候选 [seer, seer2]；seer 失败 → fallback seer2
	result, err := mesh.Delegate(context.Background(), DelegatePayload{
		Task:                 "查验 3 号",
		RequiredCapabilities: []string{"divine"},
		ParentSessionID:      "night-1",
	})
	if err != nil {
		t.Fatalf("fallback 后应成功, got %v", err)
	}
	if !contains(result.Content, "备选预言家") {
		t.Errorf("应由 seer2 兜底, got %s", result.Content)
	}
}

func TestDelegate_DepthExceeded(t *testing.T) {
	// 委托深度超过 max_delegation_depth → 直接拒绝，避免无限套娃。
	card := DefaultMockCard("seer", "seer:1")
	mesh, cleanup := NewTestAgentMesh(nil, "seer", card, func(c *AgentConfig) {
		c.MaxDelegationDepth = 2
	})
	defer cleanup()

	_, err := mesh.Delegate(context.Background(), DelegatePayload{
		Task:             "查验",
		PreferredAgent:   "seer",
		ParentSessionID:  "night-1",
		DelegationDepth:  2, // 已达上限
	})
	if err == nil {
		t.Fatal("超过最大委托深度应拒绝")
	}
	var dErr *DelegateError
	if !errors.As(err, &dErr) || dErr.Code != ErrDepthExceeded {
		t.Errorf("expected ErrDepthExceeded, got %v", err)
	}
}

func TestDelegate_DisabledMesh(t *testing.T) {
	// agents.enabled=false 时委托应直接返回错误。
	mesh := NewAgentMesh(DefaultAgentConfig(), nil, nil) // Enabled=false
	_, err := mesh.Delegate(context.Background(), DelegatePayload{
		Task: "x", PreferredAgent: "seer", ParentSessionID: "s",
	})
	if err == nil {
		t.Fatal("disabled mesh 应拒绝委托")
	}
}

func TestDelegate_BadPayload_MissingTask(t *testing.T) {
	card := DefaultMockCard("seer", "seer:1")
	mesh, cleanup := NewTestAgentMesh(nil, "seer", card)
	defer cleanup()
	_, err := mesh.Delegate(context.Background(), DelegatePayload{
		PreferredAgent: "seer", ParentSessionID: "s",
	})
	var dErr *DelegateError
	if !errors.As(err, &dErr) || dErr.Code != ErrBadPayload {
		t.Errorf("expected ErrBadPayload, got %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && stringContains(s, sub)))
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
