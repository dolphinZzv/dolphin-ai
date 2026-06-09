package permission

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"dolphin/internal/transport"
)

func TestLoad_FileNotExist(t *testing.T) {
	s, err := Load("/tmp/nonexistent_permissions.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestLoad_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "permissions.json")
	os.WriteFile(f, []byte("{bad json}"), 0644)

	_, err := Load(f)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestCheck_EmptyStore(t *testing.T) {
	s, _ := Load("/tmp/nonexistent.json")
	args := json.RawMessage(`{"command": "ls"}`)
	if r := s.Check("shell", args); r != NoMatch {
		t.Fatalf("expected NoMatch, got %s", r)
	}
}

func TestCheck_DenyMatch(t *testing.T) {
	s := &Store{rules: map[string]RuleSet{
		"shell": {
			Deny: []map[string]string{{"command": "echo *"}},
		},
	}}
	tests := []struct {
		args string
		want Result
	}{
		{`{"command": "echo secret"}`, Deny},
		{`{"command": "echo hello"}`, Deny},
		{`{"command": "ls -la"}`, NoMatch},
	}
	for _, tt := range tests {
		got := s.Check("shell", json.RawMessage(tt.args))
		if got != tt.want {
			t.Errorf("Check(%q) = %s, want %s", tt.args, got, tt.want)
		}
	}
}

func TestCheck_AllowMatch(t *testing.T) {
	s := &Store{rules: map[string]RuleSet{
		"shell": {
			Allow: []map[string]string{{"command": "ls *"}},
		},
	}}
	tests := []struct {
		args string
		want Result
	}{
		{`{"command": "ls -la"}`, Allow},
		{`{"command": "ls /tmp"}`, Allow},
		{`{"command": "echo secret"}`, NoMatch},
	}
	for _, tt := range tests {
		got := s.Check("shell", json.RawMessage(tt.args))
		if got != tt.want {
			t.Errorf("Check(%q) = %s, want %s", tt.args, got, tt.want)
		}
	}
}

func TestCheck_DenyBeforeAllow(t *testing.T) {
	// Deny rules should be checked before allow rules.
	s := &Store{rules: map[string]RuleSet{
		"shell": {
			Allow: []map[string]string{{"command": "*"}},
			Deny:  []map[string]string{{"command": "echo *"}},
		},
	}}
	if r := s.Check("shell", json.RawMessage(`{"command": "echo secret"}`)); r != Deny {
		t.Fatalf("expected Deny (deny checked before allow), got %s", r)
	}
	if r := s.Check("shell", json.RawMessage(`{"command": "ls"}`)); r != Allow {
		t.Fatalf("expected Allow, got %s", r)
	}
}

func TestCheck_DifferentTool(t *testing.T) {
	s := &Store{rules: map[string]RuleSet{
		"shell": {Deny: []map[string]string{{"command": "echo *"}}},
	}}
	if r := s.Check("FILE_UPLOAD", json.RawMessage(`{"file_path": "/etc/passwd"}`)); r != NoMatch {
		t.Fatalf("expected NoMatch for different tool, got %s", r)
	}
}

func TestCheck_MissingKey(t *testing.T) {
	s := &Store{rules: map[string]RuleSet{
		"shell": {Deny: []map[string]string{{"command": "echo *"}}},
	}}
	// Rule requires "command" key, but args don't have it — no match.
	if r := s.Check("shell", json.RawMessage(`{"other": "hello"}`)); r != NoMatch {
		t.Fatalf("expected NoMatch for missing key, got %s", r)
	}
}

// TestCheck_LsScenarios verifies the user's three scenarios for "ls":
//   - empty permissions.json → NoMatch (prompt user, not auto-pass)
//   - deny rule matches      → Deny
//   - allow rule matches     → Allow
func TestCheck_LsScenarios(t *testing.T) {
	t.Run("empty rules — ls triggers NoMatch", func(t *testing.T) {
		s := NewStore("")
		r := s.Check("shell", json.RawMessage(`{"command":"ls"}`))
		if r != NoMatch {
			t.Fatalf("empty store: expected NoMatch for ls, got %s", r)
		}
	})

	t.Run("deny rule blocks matched command but ls passes", func(t *testing.T) {
		s := &Store{rules: map[string]RuleSet{
			"shell": {Deny: []map[string]string{{"command": "echo *"}}},
		}}
		if r := s.Check("shell", json.RawMessage(`{"command":"ls"}`)); r != NoMatch {
			t.Fatalf("expected NoMatch for ls (no deny rule matches), got %s", r)
		}
		if r := s.Check("shell", json.RawMessage(`{"command":"echo secret"}`)); r != Deny {
			t.Fatalf("expected Deny for sudo command, got %s", r)
		}
	})

	t.Run("allow ls — ls is Allow", func(t *testing.T) {
		s := &Store{rules: map[string]RuleSet{
			"shell": {Allow: []map[string]string{{"command": "ls *"}}},
		}}
		if r := s.Check("shell", json.RawMessage(`{"command":"ls -la"}`)); r != Allow {
			t.Fatalf("expected Allow for ls -la, got %s", r)
		}
	})
}

func TestAddAllow(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "permissions.json")
	os.WriteFile(f, []byte(`{}`), 0644)

	s, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}

	args := json.RawMessage(`{"command": "ls -la", "timeout": "10s"}`)
	if err := s.AddAllow("shell", args); err != nil {
		t.Fatalf("AddAllow: %v", err)
	}

	// Verify it persisted.
	data, _ := os.ReadFile(f)
	var rules map[string]RuleSet
	json.Unmarshal(data, &rules)
	rs, ok := rules["shell"]
	if !ok || len(rs.Allow) != 1 {
		t.Fatalf("expected shell.allow with 1 entry, got %+v", rules)
	}
	if rs.Allow[0]["command"] != "ls -la" {
		t.Fatalf("expected command=ls -la, got %v", rs.Allow[0])
	}
}

func TestAddAllow_ConcurrentSave(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "permissions.json")
	os.WriteFile(f, []byte(`{}`), 0644)

	s, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		s.AddAllow("tool_a", json.RawMessage(`{"x": "1"}`))
		done <- struct{}{}
	}()
	go func() {
		s.AddAllow("tool_b", json.RawMessage(`{"y": "2"}`))
		done <- struct{}{}
	}()
	<-done
	<-done

	data, _ := os.ReadFile(f)
	var rules map[string]RuleSet
	json.Unmarshal(data, &rules)
	if len(rules) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(rules))
	}
}

func TestMatchRule(t *testing.T) {
	tests := []struct {
		name string
		rule map[string]string
		args map[string]any
		want bool
	}{
		{"exact match", map[string]string{"command": "ls"}, map[string]any{"command": "ls"}, true},
		{"glob match", map[string]string{"command": "ls *"}, map[string]any{"command": "ls -la"}, true},
		{"no match", map[string]string{"command": "echo *"}, map[string]any{"command": "ls"}, false},
		{"missing key", map[string]string{"command": "ls"}, map[string]any{"other": "value"}, false},
		{"multiple keys", map[string]string{"command": "ls *", "dir": "/tmp"}, map[string]any{"command": "ls -la", "dir": "/tmp"}, true},
		{"int value", map[string]string{"timeout": "30"}, map[string]any{"timeout": 30}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchRule(tt.rule, tt.args)
			if got != tt.want {
				t.Errorf("matchRule(%v, %v) = %v, want %v", tt.rule, tt.args, got, tt.want)
			}
		})
	}
}

func TestNewTestStore(t *testing.T) {
	rules := map[string]RuleSet{
		"shell": {Deny: []map[string]string{{"command": "echo *"}}},
	}
	s := NewTestStore(rules)
	if s == nil {
		t.Fatal("expected non-nil store")
	}
	if r := s.Check("shell", json.RawMessage(`{"command":"echo hi"}`)); r != Deny {
		t.Errorf("expected Deny, got %s", r)
	}
}

func TestResultString(t *testing.T) {
	if Allow.String() != "allow" {
		t.Errorf("expected 'allow', got %q", Allow.String())
	}
	if Deny.String() != "deny" {
		t.Errorf("expected 'deny', got %q", Deny.String())
	}
	if NoMatch.String() != "no_match" {
		t.Errorf("expected 'no_match', got %q", NoMatch.String())
	}
}

func TestMapResult(t *testing.T) {
	if MapResult(Allow) != transport.PermissionAlways {
		t.Error("Allow should map to PermissionAlways")
	}
	if MapResult(Deny) != transport.PermissionDenied {
		t.Error("Deny should map to PermissionDenied")
	}
	if MapResult(NoMatch) != transport.PermissionDenied {
		t.Error("NoMatch should map to PermissionDenied")
	}
}
