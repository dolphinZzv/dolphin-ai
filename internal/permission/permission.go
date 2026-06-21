package permission

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"dolphin/internal/transport"
)

// RuleSet holds allow/deny rules for a single tool.
type RuleSet struct {
	Allow []map[string]string `json:"allow,omitempty"`
	Deny  []map[string]string `json:"deny,omitempty"`
}

// Store loads and evaluates permission rules from a JSON file.
type Store struct {
	mu    sync.Mutex
	file  string
	rules map[string]RuleSet // keyed by tool name
}

// NewStore creates an empty Store with initialized rules.
// If file is non-empty, save() will write to that path.
func NewStore(file string) *Store {
	return &Store{
		file:  file,
		rules: make(map[string]RuleSet),
	}
}

// Load opens a permissions.json file and returns a Store.
// If the file does not exist, an empty store is returned (no error).
// If the file is malformed, an error is returned and the store is empty.
func Load(file string) (*Store, error) {
	s := &Store{
		file:  file,
		rules: make(map[string]RuleSet),
	}
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, fmt.Errorf("permission: read %s: %w", file, err)
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.rules); err != nil {
		return s, fmt.Errorf("permission: parse %s: %w", file, err)
	}
	return s, nil
}

// Result is the outcome of a permission check.
type Result int

const (
	Allow   Result = iota // rule matched allow
	Deny                  // rule matched deny
	NoMatch               // no rule matched
)

// Check evaluates a tool call against the rules.
// Deny rules are checked first; if a deny rule matches, Deny is returned immediately.
// Then allow rules are checked.
func (s *Store) Check(toolName string, args json.RawMessage) Result {
	s.mu.Lock()
	defer s.mu.Unlock()

	rs, ok := s.rules[toolName]
	if !ok {
		return NoMatch
	}

	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return NoMatch
	}

	for _, rule := range rs.Deny {
		if matchRule(rule, argsMap) {
			return Deny
		}
	}

	for _, rule := range rs.Allow {
		if matchRule(rule, argsMap) {
			return Allow
		}
	}

	return NoMatch
}

// AddAllow adds an allow rule for the given tool with the given arg patterns,
// then saves. Use AddAllowTool to allow the tool for all parameter values.
func (s *Store) AddAllow(toolName string, args json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return fmt.Errorf("permission: unmarshal args: %w", err)
	}

	rule := make(map[string]string)
	for k, v := range argsMap {
		switch val := v.(type) {
		case string:
			rule[k] = val
		default:
			b, _ := json.Marshal(val)
			rule[k] = string(b)
		}
	}

	rs := s.rules[toolName]
	rs.Allow = append(rs.Allow, rule)
	s.rules[toolName] = rs

	return s.save()
}

// AddAllowTool adds an allow rule that matches ALL parameter values for the
// tool. Equivalent to the user saying "always allow this tool".
func (s *Store) AddAllowTool(toolName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rs := s.rules[toolName]
	// Empty allow rule matches all parameter values.
	rs.Allow = append(rs.Allow, map[string]string{})
	s.rules[toolName] = rs

	return s.save()
}

// AddDenyDefaults merges deny rules from an external source (e.g. config.yaml).
// Existing deny rules are preserved; new ones are appended.
func (s *Store) AddDenyDefaults(defaults map[string][]map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for toolName, rules := range defaults {
		rs := s.rules[toolName]
		rs.Deny = append(rs.Deny, rules...)
		s.rules[toolName] = rs
	}
}

// save writes the current rules to the JSON file.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.rules, "", "  ")
	if err != nil {
		return fmt.Errorf("permission: marshal: %w", err)
	}
	if err := os.WriteFile(s.file, data, 0o600); err != nil {
		return fmt.Errorf("permission: write %s: %w", s.file, err)
	}
	return nil
}

// NewTestStore creates a store with the given rules for testing.
func NewTestStore(rules map[string]RuleSet) *Store {
	return &Store{rules: rules}
}

// matchRule checks if a single rule entry matches the tool call arguments.
func matchRule(rule map[string]string, args map[string]any) bool {
	for key, pattern := range rule {
		val, ok := args[key]
		if !ok {
			return false
		}
		var valStr string
		switch v := val.(type) {
		case string:
			valStr = v
		default:
			b, _ := json.Marshal(v)
			valStr = string(b)
		}
		if !globMatch(pattern, valStr) {
			return false
		}
	}
	return true
}

// globMatch is a simple glob matcher that supports * and ? without path separator restrictions.
func globMatch(pattern, s string) bool {
	px := 0
	sx := 0
	nextPx := -1
	nextSx := -1

	for sx < len(s) {
		// Conditions are compound and vary per branch (not equality on a
		// single value), so an if/else chain reads better than a switch.
		if px < len(pattern) && pattern[px] == '*' { //nolint:gocritic // ifElseChain
			nextPx = px
			nextSx = sx + 1
			px++
		} else if px < len(pattern) && (pattern[px] == '?' || pattern[px] == s[sx]) {
			px++
			sx++
		} else if nextPx != -1 {
			px = nextPx + 1
			sx = nextSx
			nextSx++
		} else {
			return false
		}
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}

var _ fmt.Stringer = Result(0)

func (r Result) String() string {
	switch r { //nolint:exhaustive // default covers NoMatch
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	default:
		return "no_match"
	}
}

// MapResult maps a Result to a transport.PermissionResult.
func MapResult(r Result) transport.PermissionResult {
	switch r { //nolint:exhaustive // NoMatch falls through to default (Deny)
	case Allow:
		return transport.PermissionAlways
	case Deny:
		return transport.PermissionDenied
	default:
		return transport.PermissionDenied // caller should treat NoMatch specially
	}
}
