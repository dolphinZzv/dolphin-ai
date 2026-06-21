package agentloop

import (
	"context"
	"testing"
	"time"
)

func TestManualCompact_EdgeCases(t *testing.T) {
	var s *CompactionStage
	_, err := s.ManualCompact(context.Background(), "s1")
	if err == nil {
		t.Fatal("expected error for nil stage")
	}
	s2 := &CompactionStage{}
	_, err = s2.ManualCompact(context.Background(), "s1")
	if err == nil {
		t.Fatal("expected error for empty stage")
	}
}

func TestIsSafeShellCommand(t *testing.T) {
	cases := []struct {
		tool, args string
		want       bool
	}{
		{"shell", `{"command":"ls"}`, true},
		{"shell", `{"command":"pwd"}`, true},
		{"shell", `{"command":"grep x"}`, true},
		{"shell", `{"command":"rm -rf"}`, false},
		{"shell", ``, false},
		{"other", `{}`, false},
		{"shell", `ls`, true},
		{"shell", `{"command":"ls|grep"}`, false},
		{"shell", `{"command":"echo $HOME"}`, false},
		{"shell", `{"command":""}`, false},
		{"shell", `{"command":"$HOME"}`, false},
		{"shell", `{"command":"(echo)"}`, false},
		{"shell", "{\"command\":\"`cmd`\"}", false},
		{"shell", `{"command":"a&b"}`, false},
		{"shell", `{"command":"a;b"}`, false},
	}
	for _, c := range cases {
		if got := isSafeShellCommand(c.tool, c.args); got != c.want {
			t.Errorf("%q %q: got %v want %v", c.tool, c.args, got, c.want)
		}
	}
}

func TestAgentLoopSetSessionGcInterval(t *testing.T) {
	a := NewAgentLoop(nil, nil, nil, nil, nil, 1)
	a.SetSessionGcInterval(time.Minute)
	if a.gcInterval != time.Minute {
		t.Error("gcInterval not set")
	}
	// SetDumpRecorder should be callable (just exercises the setter path)
	a.SetDumpRecorder(nil)
}

func TestCompositor_Setters(t *testing.T) {
	c := NewCompositor(nil, nil, 10)
	c.SetTurnTimeout(time.Second)
	c.SetIdleTimeout(0)
	c.SetFeedMinInterval(0)
}

func TestCompactionStage_NameClone(t *testing.T) {
	s := &CompactionStage{MaxTokens: 100, KeepRounds: 3}
	if s.Name() != "compaction" {
		t.Errorf("Name() = %q want \"compaction\"", s.Name())
	}
	c2 := s.Clone().(*CompactionStage)
	if c2.MaxTokens != 100 || c2.KeepRounds != 3 {
		t.Errorf("Clone did not copy fields: %+v", c2)
	}
}

func TestCompactionStage_ActiveModel(t *testing.T) {
	s := &CompactionStage{Model: "gpt-4"}
	if got := s.activeModel(); got != "gpt-4" {
		t.Errorf("activeModel() = %q want %q", got, "gpt-4")
	}
	// nil Provider fallback
	s2 := &CompactionStage{}
	if got := s2.activeModel(); got != "" {
		t.Errorf("activeModel() with no provider = %q want \"\"", got)
	}
}
