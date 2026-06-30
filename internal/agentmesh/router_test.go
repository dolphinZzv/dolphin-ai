package agentmesh

import (
	"errors"
	"testing"
)

func TestRouter_MatchByCapability_ExactAndFilterBusy(t *testing.T) {
	// 主持人需要找能「查验」的预言家。守卫虽存活但不具备 divine 能力；
	// 另一个预言家「满载」（已查验过本轮）应被过滤。
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "seer", Addr: "seer:1", Capabilities: []string{"divine", "villager"}, Load: 0, MaxLoad: 5, Status: AgentRunning})
	reg.Upsert(AgentCard{Name: "guard", Addr: "guard:1", Capabilities: []string{"protect"}, Load: 0, MaxLoad: 5, Status: AgentRunning})
	reg.Upsert(AgentCard{Name: "busy-seer", Addr: "bs:1", Capabilities: []string{"divine"}, Load: 5, MaxLoad: 5, Status: AgentRunning})

	r := NewRouter(reg, FallbackConfig{Enabled: true, MaxFallback: 2}, nil)

	cards := r.MatchByCapability([]string{"divine"})
	if len(cards) != 1 {
		t.Fatalf("只应有 1 个可用预言家 (满载者被过滤), got %d", len(cards))
	}
	if cards[0].Name != "seer" {
		t.Errorf("expected seer, got %s", cards[0].Name)
	}
}

func TestRouter_MatchByCapability_NoMatch(t *testing.T) {
	// 白天想找能「自爆」的角色，本局没有狼王，无匹配。
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "seer", Addr: "seer:1", Capabilities: []string{"divine"}, Status: AgentRunning})
	r := NewRouter(reg, FallbackConfig{}, nil)
	cards := r.MatchByCapability([]string{"self-destruct"})
	if len(cards) != 0 {
		t.Fatalf("无狼王应无匹配, got %+v", cards)
	}
}

func TestRouter_Route_PreferredAgent(t *testing.T) {
	// 主持人点名「女巫」来用解药，直接路由到女巫。
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "witch", Addr: "witch:1", Capabilities: []string{"save", "poison"}, Status: AgentRunning, MaxLoad: 5})
	r := NewRouter(reg, FallbackConfig{}, nil)

	got, err := r.Route(DelegatePayload{PreferredAgent: "witch", Task: "用解药救 3 号"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "witch" {
		t.Fatalf("expected [witch], got %+v", got)
	}
}

func TestRouter_Route_PreferredAgentNotFound(t *testing.T) {
	// 主持人想点名「丘比特」，但本局没有这个角色。
	reg := NewRegistry(nil, nil, nil)
	r := NewRouter(reg, FallbackConfig{}, nil)
	_, err := r.Route(DelegatePayload{PreferredAgent: "cupid", Task: "连情侣"})
	if err == nil {
		t.Fatal("expected error for unknown role")
	}
	var dErr *DelegateError
	if !errors.As(err, &dErr) || dErr.Code != ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}
}

func TestRouter_Route_PreferredAgentBusy(t *testing.T) {
	// 预言家本轮已查验过 (Load==MaxLoad)，再点名应返回 busy。
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "seer", Addr: "seer:1", Load: 5, MaxLoad: 5, Status: AgentRunning})
	r := NewRouter(reg, FallbackConfig{}, nil)
	_, err := r.Route(DelegatePayload{PreferredAgent: "seer", Task: "再查一次"})
	var dErr *DelegateError
	if !errors.As(err, &dErr) || dErr.Code != ErrAgentBusy {
		t.Errorf("expected ErrAgentBusy, got %v", err)
	}
}

func TestRouter_MatchByCapability_RanksByScoreThenLoad(t *testing.T) {
	// 主持人要找一个能「毒+救」的女巫 (需 poison & save)。
	// 三个候选：一个只会毒(部分匹配)，两个都会但负载不同 → 全匹配且低负载者排第一。
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "poisoner-only", Addr: "p:1", Capabilities: []string{"poison"}, Load: 0, MaxLoad: 5, Status: AgentRunning})
	reg.Upsert(AgentCard{Name: "full-witch-hi", Addr: "fh:1", Capabilities: []string{"poison", "save"}, Load: 3, MaxLoad: 5, Status: AgentRunning})
	reg.Upsert(AgentCard{Name: "full-witch-lo", Addr: "fl:1", Capabilities: []string{"poison", "save"}, Load: 1, MaxLoad: 5, Status: AgentRunning})
	r := NewRouter(reg, FallbackConfig{Enabled: true, MaxFallback: 2}, nil)

	cards := r.MatchByCapability([]string{"poison", "save"})
	if len(cards) != 3 {
		t.Fatalf("expected 3 候选, got %d", len(cards))
	}
	if cards[0].Name != "full-witch-lo" {
		t.Errorf("全匹配且低负载的女巫应排第一, got %s", cards[0].Name)
	}
}

// --- 通用测试用例（非狼人杀主题，覆盖原始 code-review 场景） ---

func TestRouter_Generic_MatchByCapability(t *testing.T) {
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "cr", Addr: "cr:1", Capabilities: []string{"code-review", "golang"}, Load: 0, MaxLoad: 5, Status: AgentRunning})
	reg.Upsert(AgentCard{Name: "ss", Addr: "ss:1", Capabilities: []string{"security"}, Load: 0, MaxLoad: 5, Status: AgentRunning})
	reg.Upsert(AgentCard{Name: "busy", Addr: "b:1", Capabilities: []string{"code-review"}, Load: 5, MaxLoad: 5, Status: AgentRunning})

	r := NewRouter(reg, FallbackConfig{Enabled: true, MaxFallback: 2}, nil)

	cards := r.MatchByCapability([]string{"code-review"})
	if len(cards) != 1 {
		t.Fatalf("expected 1 candidate (busy filtered), got %d", len(cards))
	}
	if cards[0].Name != "cr" {
		t.Errorf("expected cr, got %s", cards[0].Name)
	}
}

func TestRouter_Generic_NoMatch(t *testing.T) {
	reg := NewRegistry(nil, nil, nil)
	reg.Upsert(AgentCard{Name: "cr", Addr: "cr:1", Capabilities: []string{"code-review"}, Status: AgentRunning})
	r := NewRouter(reg, FallbackConfig{}, nil)
	cards := r.MatchByCapability([]string{"data-analysis"})
	if len(cards) != 0 {
		t.Fatalf("expected no match, got %+v", cards)
	}
}

func TestRouter_Generic_PreferredNotFound(t *testing.T) {
	reg := NewRegistry(nil, nil, nil)
	r := NewRouter(reg, FallbackConfig{}, nil)
	_, err := r.Route(DelegatePayload{PreferredAgent: "ghost", Task: "x"})
	if err == nil {
		t.Fatal("expected error for unknown preferred agent")
	}
	var dErr *DelegateError
	if !errors.As(err, &dErr) || dErr.Code != ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}
}
