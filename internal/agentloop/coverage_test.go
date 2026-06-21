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
}
