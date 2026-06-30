package agentmesh

import (
	"testing"
	"time"
)

func TestCircuitBreaker_OpenAfterThreshold(t *testing.T) {
	// 狼人 agent 连续 3 次夜杀请求失败 → 熔断打开，后续请求被直接拒绝。
	base := time.Now()
	cb := NewCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 3, CooldownPeriod: 1 * time.Second, HalfOpenMax: 1}).
		withClock(func() time.Time { return base })

	for i := range 3 {
		if !cb.Allow() {
			t.Fatalf("熔断前第 %d 次应放行", i+1)
		}
		cb.RecordFailure()
	}
	// 第 4 次应被熔断
	if cb.Allow() {
		t.Fatal("连续失败达阈值后应熔断，拒绝新请求")
	}
}

func TestCircuitBreaker_HalfOpenAfterCooldown(t *testing.T) {
	// 熔断冷却期过后，进入半开，允许一次试探。
	base := time.Now()
	cb := NewCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 2, CooldownPeriod: 1 * time.Second, HalfOpenMax: 1}).
		withClock(func() time.Time { return base })

	cb.Allow(); cb.RecordFailure()
	cb.Allow(); cb.RecordFailure()
	if cb.Allow() {
		t.Fatal("应已熔断")
	}

	// 推进时间越过冷却期 → 半开，放行一次试探
	cb.now = func() time.Time { return base.Add(2 * time.Second) }
	if !cb.Allow() {
		t.Fatal("冷却期后应进入半开放行试探")
	}
	// 半开仅允许 1 次试探，第二次应被拒
	if cb.Allow() {
		t.Fatal("半开试探名额用尽后应拒绝")
	}
}

func TestCircuitBreaker_HalfOpenSuccessCloses(t *testing.T) {
	// 半开试探成功 → 恢复 CLOSED。
	base := time.Now()
	cb := NewCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 1, CooldownPeriod: 1 * time.Second}).
		withClock(func() time.Time { return base })

	cb.Allow(); cb.RecordFailure() // 立即熔断
	cb.now = func() time.Time { return base.Add(2 * time.Second) }
	if !cb.Allow() {
		t.Fatal("半开应放行")
	}
	cb.RecordSuccess()
	// 恢复 CLOSED，可连续放行
	if !cb.Allow() {
		t.Fatal("试探成功后应恢复 CLOSED")
	}
	if !cb.Allow() {
		t.Fatal("CLOSED 状态应持续放行")
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	// 半开试探失败 → 重新熔断。
	base := time.Now()
	cb := NewCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 1, CooldownPeriod: 1 * time.Second}).
		withClock(func() time.Time { return base })

	cb.Allow(); cb.RecordFailure()
	cb.now = func() time.Time { return base.Add(2 * time.Second) }
	cb.Allow() // 半开试探
	cb.RecordFailure()
	if cb.Allow() {
		t.Fatal("半开试探失败应重新熔断")
	}
}
