package agentmesh

import (
	"testing"
	"time"
)

func TestRateLimiter_AllowBurst(t *testing.T) {
	// 预言家每秒可被委托 2 次，突发 5 次。
	// 主持人连发 5 次查验请求应全部放行，第 6 次被限流。
	rl := NewRateLimiter(2, 5)
	addr := "seer:1"
	start := time.Now()
	for i := range 5 {
		if !rl.AllowAt(addr, start) {
			t.Fatalf("突发 5 次内第 %d 次应放行", i+1)
		}
	}
	if rl.AllowAt(addr, start) {
		t.Fatal("第 6 次应被限流")
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	// 500ms 后应回补 1 个 token（2 req/s）。
	rl := NewRateLimiter(2, 5)
	addr := "seer:1"
	start := time.Now()
	for range 5 {
		rl.AllowAt(addr, start)
	}
	if rl.AllowAt(addr, start) {
		t.Fatal("突发耗尽后应被限流")
	}
	if !rl.AllowAt(addr, start.Add(510*time.Millisecond)) {
		t.Fatal("510ms 后应回补至少 1 个 token")
	}
}

func TestRateLimiter_PerAgentIsolation(t *testing.T) {
	// 预言家的限流不影响女巫。
	rl := NewRateLimiter(2, 1)
	if !rl.AllowAt("seer:1", time.Now()) {
		t.Fatal("seer 应放行")
	}
	if rl.AllowAt("seer:1", time.Now()) {
		t.Fatal("seer 突发耗尽应限流")
	}
	if !rl.AllowAt("witch:1", time.Now()) {
		t.Fatal("witch 限流独立，应放行")
	}
}
