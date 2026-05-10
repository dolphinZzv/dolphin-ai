package agent

import "testing"

func TestAgentKindStringUser(t *testing.T) {
	if s := AgentUser.String(); s != "user" {
		t.Errorf("AgentUser.String() = %q", s)
	}
}

func TestAgentKindStringCoord(t *testing.T) {
	if s := AgentCoord.String(); s != "temp" {
		t.Errorf("AgentCoord.String() = %q", s)
	}
}

func TestAgentKindStringUnknown(t *testing.T) {
	if s := AgentKind(99).String(); s != "unknown" {
		t.Errorf("AgentKind(99).String() = %q", s)
	}
}
