package dream

import (
	"context"
	"sync"
	"time"

	"dolphin/internal/llm"
	"dolphin/internal/session"
	"dolphin/internal/types"
)

// ─────────────────────────────────────────────────────────────────
// Mock implementations
// ─────────────────────────────────────────────────────────────────

type mockMemory struct {
	mu       sync.Mutex
	messages map[string][]types.Message
}

func newMockMemory() *mockMemory { return &mockMemory{messages: make(map[string][]types.Message)} }

func (m *mockMemory) Read(_ context.Context, sessionID string) ([]types.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages[sessionID], nil
}
func (m *mockMemory) Write(_ context.Context, sessionID string, msg types.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[sessionID] = append(m.messages[sessionID], msg)
	return nil
}
func (m *mockMemory) Replace(_ context.Context, sessionID string, msgs []types.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[sessionID] = msgs
	return nil
}

type mockSessionMgr struct {
	sessions []*session.Session
}

func (m *mockSessionMgr) List(_ context.Context) ([]*session.Session, error) { return m.sessions, nil }
func (m *mockSessionMgr) Active() *session.Session {
	for _, s := range m.sessions {
		if s.Active {
			return s
		}
	}
	return nil
}

type mockBrain struct {
	files map[string]string
}

func newMockBrain() *mockBrain   { return &mockBrain{files: make(map[string]string)} }
func (b *mockBrain) Dir() string { return "/tmp/test-brain" }
func (b *mockBrain) Read(_ context.Context, path string) (string, error) {
	if v, ok := b.files[path]; ok {
		return v, nil
	}
	return "", nil
}
func (b *mockBrain) List(_ context.Context) ([]string, error) {
	var ps []string
	for p := range b.files {
		ps = append(ps, p)
	}
	return ps, nil
}
func (b *mockBrain) AutoCommit(_ context.Context, _ string) {}
func (b *mockBrain) Tree() (string, error)                  { return "", nil }

type mockProvider struct {
	output string
	err    error
}

func (p *mockProvider) Name() string { return "mock" }
func (p *mockProvider) CompleteStream(_ context.Context, _ llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	if p.err != nil {
		return nil, p.err
	}
	ch := make(chan llm.LLMChunk, 1)
	go func() { ch <- llm.LLMChunk{Content: p.output}; close(ch) }()
	return ch, nil
}
func (p *mockProvider) Models(_ context.Context) ([]llm.ModelConfig, error) { return nil, nil }

type mockAgentIO struct{ processing bool }

func (a *mockAgentIO) Processing() bool { return a.processing }

// ─────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────

func makeSession(id string, createdAt time.Time, active bool) *session.Session {
	return &session.Session{ID: id, CreatedAt: createdAt, Active: active}
}

func userMsg(c string, ts time.Time) types.Message {
	return types.Message{Role: types.RoleUser, Content: c, Timestamp: ts}
}
func asstMsg(c string, ts time.Time) types.Message {
	return types.Message{Role: types.RoleAssistant, Content: c, Timestamp: ts}
}
