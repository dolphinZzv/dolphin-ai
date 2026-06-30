package agentmesh

import (
	"testing"
)

// 狼人杀角色 → agent 能力映射（贯穿本包测试）：
//   主持人 moderator   : orchestrator，不做具体动作
//   预言家 seer        : ["divine"]            夜晚查验身份
//   女巫   witch       : ["poison","save"]     解药/毒药
//   守卫   guard       : ["protect"]           守护一名玩家
//   猎人   hunter      : ["shoot"]             死后开枪
//   狼人   werewolf    : ["kill"]              夜杀

func TestRegistry_StaticConfig(t *testing.T) {
	// 开局：主持人静态配置了预言家与女巫两个 agent。
	reg := NewRegistry(
		[]RemoteAgent{
			{Name: "seer", Addr: "localhost:8102", Capabilities: []string{"divine"}},
			{Name: "witch", Addr: "localhost:8103", Capabilities: []string{"poison", "save"}},
		},
		nil, nil,
	)
	cards := reg.List()
	if len(cards) != 2 {
		t.Fatalf("expected 2 角色注册, got %d", len(cards))
	}
	if cards[0].Name != "seer" {
		t.Errorf("expected seer, got %s", cards[0].Name)
	}
	if cards[0].Status != AgentRunning {
		t.Errorf("expected running, got %s", cards[0].Status)
	}
}

func TestRegistry_Upsert_Dedup(t *testing.T) {
	// 同一个预言家重复宣告上线，应当去重，而不是注册两个。
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "seer", Addr: "seer:1"})
	reg.Upsert(AgentCard{Name: "seer", Addr: "seer:1"})
	if got := len(reg.List()); got != 1 {
		t.Fatalf("预言家重复宣告应去重为 1, got %d", got)
	}
}

func TestRegistry_Upsert_ConflictTieBreaker_HigherProto(t *testing.T) {
	// 两个 agent 都自称是「预言家」但地址不同：版本更高者胜出。
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "seer", Addr: "seer:1", ProtoVersion: 2})
	reg.Upsert(AgentCard{Name: "seer", Addr: "seer:2", ProtoVersion: 4}) // 新版本预言家
	c, ok := reg.Get("seer")
	if !ok {
		t.Fatal("expected seer to exist")
	}
	if c.Addr != "seer:2" {
		t.Errorf("higher-proto 预言家应胜出 (seer:2), got %s", c.Addr)
	}

	// 一个旧版本(低 proto)的同名预言家再来，应被拒绝。
	reg.Upsert(AgentCard{Name: "seer", Addr: "seer:3", ProtoVersion: 1})
	c, _ = reg.Get("seer")
	if c.Addr != "seer:2" {
		t.Errorf("低版本预言家不应顶替, expected seer:2, got %s", c.Addr)
	}
}

func TestRegistry_Upsert_ConflictTieBreaker_LoadTieBreak(t *testing.T) {
	// 两个预言家 proto 相同：负载更轻的胜出（像选空闲的预言家来查验）。
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "seer", Addr: "s-A", ProtoVersion: 2, Load: 3})
	reg.Upsert(AgentCard{Name: "seer", Addr: "s-B", ProtoVersion: 2, Load: 1})
	c, _ := reg.Get("seer")
	if c.Addr != "s-B" {
		t.Errorf("低负载预言家应胜出 (s-B), got %s", c.Addr)
	}
}

func TestRegistry_Deregister(t *testing.T) {
	// 猎人夜晚被狼人刀了 → 退出注册表。
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "hunter", Addr: "h:1"})
	reg.Deregister("hunter")
	if _, ok := reg.Get("hunter"); ok {
		t.Fatal("猎人退场后应从注册表移除")
	}
}

func TestRegistry_ListRunning_FiltersStatus(t *testing.T) {
	// 存活的角色才参与夜晚行动；已死亡(Stopped)的不应出现。
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "alive-seer", Addr: "u", Status: AgentRunning})
	reg.Upsert(AgentCard{Name: "dead-witch", Addr: "d", Status: AgentStopped})
	running := reg.ListRunning()
	if len(running) != 1 || running[0].Name != "alive-seer" {
		t.Fatalf("只应有存活角色, got %+v", running)
	}
}

// --- 通用测试用例（非狼人杀主题，覆盖原始 code-review 场景） ---

func TestRegistry_Generic_StaticConfig(t *testing.T) {
	cfg := []RemoteAgent{
		{Name: "code-reviewer", Addr: "localhost:8102", Capabilities: []string{"code-review"}},
	}
	reg := NewRegistry(cfg, nil, nil)
	cards := reg.List()
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}
	if cards[0].Name != "code-reviewer" {
		t.Errorf("expected code-reviewer, got %s", cards[0].Name)
	}
}

func TestRegistry_Generic_Upsert_Dedup(t *testing.T) {
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "A", Addr: "addr1"})
	reg.Upsert(AgentCard{Name: "A", Addr: "addr1"}) // 重复 → 忽略
	if got := len(reg.List()); got != 1 {
		t.Fatalf("expected 1 after dedup, got %d", got)
	}
}

func TestRegistry_Generic_Deregister(t *testing.T) {
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "X", Addr: "x"})
	reg.Deregister("X")
	if _, ok := reg.Get("X"); ok {
		t.Fatal("expected X to be gone")
	}
}
