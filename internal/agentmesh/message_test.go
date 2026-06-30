package agentmesh

import (
	"encoding/json"
	"testing"
)

func TestDelegatePayload_BackwardCompat(t *testing.T) {
	// v1 版本的 payload（没有 capabilities 字段）应能正常反序列化，
	// 缺失字段为零值——保证老版本 agent 发来的委托不会因新字段而拒绝。
	v1Payload := `{"task":"test","parent_session_id":"abc"}`
	var p DelegatePayload
	if err := json.Unmarshal([]byte(v1Payload), &p); err != nil {
		t.Fatalf("v1 payload should unmarshal: %v", err)
	}
	if p.Task != "test" {
		t.Errorf("expected task=test, got %s", p.Task)
	}
	if p.ParentSessionID != "abc" {
		t.Errorf("expected parent_session_id=abc, got %s", p.ParentSessionID)
	}
	if len(p.RequiredCapabilities) != 0 {
		t.Errorf("missing capabilities should be empty slice, got %v", p.RequiredCapabilities)
	}
}

func TestDelegateError_ErrorString(t *testing.T) {
	e := &DelegateError{Code: ErrTimeout, Message: "seer 查验超时"}
	if e.Error() != "timeout: seer 查验超时" {
		t.Errorf("unexpected error string: %s", e.Error())
	}
	e2 := &DelegateError{Code: ErrAgentUnavail, Message: "witch 不可达", Cause: "connection refused"}
	if e2.Error() != "agent_unavailable: witch 不可达 (connection refused)" {
		t.Errorf("unexpected error string: %s", e2.Error())
	}
}

func TestErrorCode_IsRetryable(t *testing.T) {
	// 夜晚网络抖动导致的超时/不可达应当重试；权限拒绝与负载超限不可重试。
	if !ErrTimeout.IsRetryable() {
		t.Error("timeout 应可重试")
	}
	if !ErrAgentUnavail.IsRetryable() {
		t.Error("agent_unavailable 应可重试")
	}
	if ErrPermission.IsRetryable() {
		t.Error("permission 不应重试")
	}
	if ErrBadPayload.IsRetryable() {
		t.Error("bad_payload 不应重试")
	}
}

func TestDelegateResult_RoundTrip(t *testing.T) {
	// 一次完整的「预言家查验」结果序列化/反序列化往返。
	orig := DelegateResult{
		TaskID:  "night-1-divine",
		Status:  DelegateCompleted,
		Content: "3 号玩家是狼人",
		Rounds:  2,
		ToolCalls: []ToolCallSummary{
			{Name: "divine", Success: true, Summary: "查验 3 号"},
		},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var back DelegateResult
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Status != DelegateCompleted || back.Content != "3 号玩家是狼人" || back.Rounds != 2 {
		t.Errorf("round-trip mismatch: %+v", back)
	}
	if len(back.ToolCalls) != 1 || back.ToolCalls[0].Name != "divine" {
		t.Errorf("tool_calls round-trip mismatch: %+v", back.ToolCalls)
	}
}
