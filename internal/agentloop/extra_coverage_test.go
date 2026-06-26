package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/event"
	"dolphin/internal/hook"
	"dolphin/internal/llm"
	"dolphin/internal/llm/proto"
	"dolphin/internal/signal"
	"dolphin/internal/tool"
	"dolphin/internal/types"
)

// TestNewAgentLoopEdgeCases tests uncovered branches in NewAgentLoop.
func TestNewAgentLoopEdgeCases(t *testing.T) {
	// poolSize <= 0 should default to 1
	al := NewAgentLoop(nil, nil, nil, nil, nil, 0)
	if al.poolSize != 1 {
		t.Errorf("poolSize 0 -> expected 1, got %d", al.poolSize)
	}
}

// TestLLMStageProcess_HookRegLimit tests the HookReg.DispatchCheck error path.
func TestLLMStageProcess_HookRegLimit(t *testing.T) {
	logger := zap.NewNop()
	eb := event.NewBus()
	hookReg := hook.NewRegistry()
	hookReg.Register(&testCheckHandler{err: errors.New("rate limited")})

	stage := &LLMStage{
		HookReg:  hookReg,
		EventBus: eb,
		Logger:   logger,
	}
	state := &State{
		SessionID: "s-limit",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
	}
	err := stage.Process(context.Background(), state)
	if err == nil {
		t.Fatal("expected error from HookReg limit")
	}
}

// TestLLMStageProcess_InterruptBeforeRetry tests signal Interrupt in retry loop.
func TestLLMStageProcess_InterruptBeforeRetry(t *testing.T) {
	logger := zap.NewNop()
	eb := event.NewBus()
	sigBus := signal.NewBus()

	provider := &alwaysErrorProvider{
		err: &proto.HTTPStatusError{Status: http.StatusServiceUnavailable},
	}

	stage := &LLMStage{
		Provider:   provider,
		MaxRetries: 3,
		EventBus:   eb,
		SignalBus:  sigBus,
		Logger:     logger,
	}
	state := &State{
		SessionID: "s-int",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
	}

	// Send interrupt after a short delay so the retry loop starts.
	go func() {
		time.Sleep(10 * time.Millisecond)
		sigBus.Send("s-int", signal.Interrupt)
	}()

	err := stage.Process(context.Background(), state)
	if !errors.Is(err, ErrInterrupted) {
		t.Errorf("expected ErrInterrupted, got %v", err)
	}
}

// TestLLMStageProcess_RetryExhausted tests all retries consumed.
func TestLLMStageProcess_RetryExhausted(t *testing.T) {
	logger := zap.NewNop()
	eb := event.NewBus()

	provider := &alwaysErrorProvider{
		err: &proto.HTTPStatusError{Status: http.StatusServiceUnavailable},
	}

	stage := &LLMStage{
		Provider:   provider,
		MaxRetries: 1,
		EventBus:   eb,
		Logger:     logger,
	}
	state := &State{
		SessionID: "s-exhausted",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
	}

	err := stage.Process(context.Background(), state)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if atomic.LoadInt32(&provider.calls) != 2 { // 1 initial + 1 retry
		t.Errorf("expected 2 calls, got %d", provider.calls)
	}
}

// TestTryComplete_DoneAndCacheTokens tests the Done path with cache tokens.
func TestTryComplete_DoneAndCacheTokens(t *testing.T) {
	logger := zap.NewNop()
	eb := event.NewBus()

	provider := &chunkProvider{chunks: []llm.LLMChunk{
		{Content: "hello", Done: true,
			InputTokens: 10, OutputTokens: 20,
			CacheCreationInputTokens: 5, CacheReadInputTokens: 3,
			PromptCacheHitTokens: 2, PromptCacheMissTokens: 8},
	}}

	stage := &LLMStage{
		Provider:   provider,
		MaxRetries: 0,
		EventBus:   eb,
		Logger:     logger,
	}
	state := &State{
		SessionID: "s-cache",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
	}

	err := stage.Process(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(state.Messages))
	}
	if state.Messages[1].Content != "hello" {
		t.Errorf("expected content 'hello', got %q", state.Messages[1].Content)
	}
}

// TestToolStage_MaxParallel tests the MaxParallel > 1 path.
func TestToolStage_MaxParallel(t *testing.T) {
	logger := zap.NewNop()
	eb := event.NewBus()
	toolReg := tool.NewRegistry()
	toolReg.RegisterBuiltin("echo", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		return &types.ToolResult{Content: string(args)}, nil
	})
	toolReg.RegisterBuiltin("echo2", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		return &types.ToolResult{Content: string(args)}, nil
	})

	stage := &ToolStage{
		ToolRegistry: toolReg,
		MaxParallel:  2,
		EventBus:     eb,
		Logger:       logger,
	}
	state := &State{
		SessionID: "s-parallel",
		ToolCalls: []types.ToolCall{
			{ID: "call1", Name: "echo", Arguments: `"hello"`},
			{ID: "call2", Name: "echo2", Arguments: `"world"`},
		},
	}
	err := stage.Process(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.ToolResults) != 2 {
		t.Errorf("expected 2 tool results, got %d", len(state.ToolResults))
	}
}

// TestToolStage_ErrTurnAborted tests ErrTurnAborted handling in serial path.
func TestToolStage_ErrTurnAborted(t *testing.T) {
	logger := zap.NewNop()
	eb := event.NewBus()

	toolReg := tool.NewRegistry()
	toolReg.RegisterBuiltin("shell", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		return &types.ToolResult{Content: "ok"}, nil
	})
	toolReg.RegisterBuiltin("ls", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		return &types.ToolResult{Content: "ok"}, nil
	})

	stage := &ToolStage{
		ToolRegistry: toolReg,
		EventBus:     eb,
		Logger:       logger,
	}
	state := &State{
		SessionID: "s-abort",
		ToolCalls: []types.ToolCall{
			{ID: "call1", Name: "shell", Arguments: `{"command":"rm -rf /"}`},
			{ID: "call2", Name: "ls", Arguments: `{}`},
		},
	}
	err := stage.Process(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.ToolResults) != 2 {
		t.Errorf("expected 2 tool results, got %d", len(state.ToolResults))
	}
}

// TestCompactionStage_ManualCompact_Nil tests ManualCompact with nil stage.
func TestCompactionStage_ManualCompact_Nil(t *testing.T) {
	var s *CompactionStage
	_, err := s.ManualCompact(context.Background(), "s1")
	if err == nil {
		t.Fatal("expected error for nil stage")
	}
}

// TestCompactionStage_ManualCompact_Disabled tests disabled compaction.
func TestCompactionStage_ManualCompact_Disabled(t *testing.T) {
	s := &CompactionStage{MaxThreshold: 0, KeepRounds: 0}
	_, err := s.ManualCompact(context.Background(), "s1")
	if err == nil {
		t.Fatal("expected error for disabled compaction")
	}
}

// TestCompactionStage_ManualCompact_EmptySession tests empty session.
func TestCompactionStage_ManualCompact_EmptySession(t *testing.T) {
	mem := &emptyMemory{}
	s := &CompactionStage{
		Memory:       mem,
		MaxThreshold: 100,
		KeepRounds:   3,
		MaxTokens:    100,
	}
	_, err := s.ManualCompact(context.Background(), "s-empty")
	if err == nil {
		t.Fatal("expected error for empty session")
	}
}

// TestCompactionStage_Process_Nil tests nil/disabled paths in Process.
func TestCompactionStage_Process_Nil(t *testing.T) {
	// nil stage
	var s *CompactionStage
	err := s.Process(context.Background(), &State{})
	if err != nil {
		t.Fatalf("nil stage Process should return nil, got %v", err)
	}
	// No provider
	s2 := &CompactionStage{}
	err = s2.Process(context.Background(), &State{})
	if err != nil {
		t.Fatalf("should return nil when no provider, got %v", err)
	}
}

func TestAgentLoopRun_ContextCancelled(t *testing.T) {
	logger := zap.NewNop()
	queue := make(chan *agentio.Turn, 1)
	c := NewCompositor(nil, nil, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	al := NewAgentLoop(queue, c, logger, nil, nil, 1)
	al.Run(ctx)
}

// TestTryComplete_ToolRegistryList tests the ToolRegistry.List fallback path.
func TestTryComplete_ToolRegistryList(t *testing.T) {
	logger := zap.NewNop()
	eb := event.NewBus()
	toolReg := tool.NewRegistry()
	toolReg.RegisterBuiltin("echo", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
		return &types.ToolResult{Content: string(args)}, nil
	})

	chunks := []llm.LLMChunk{
		{Content: "result", Done: true, InputTokens: 5, OutputTokens: 10},
	}
	provider := &chunkProvider{chunks: chunks}

	stage := &LLMStage{
		Provider:     provider,
		MaxRetries:   0,
		ToolRegistry: toolReg,
		EventBus:     eb,
		Logger:       logger,
	}

	state := &State{
		SessionID: "s-tools",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
		// No per-turn tools set — should fall back to ToolRegistry.List
	}

	err := stage.Process(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(state.Messages))
	}
}

// TestLLMStage_Process_PauseThenInterrupt tests Pause then Interrupt signal.
func TestLLMStage_Process_PauseThenInterrupt(t *testing.T) {
	logger := zap.NewNop()
	sigBus := signal.NewBus()

	provider := &alwaysErrorProvider{
		err: &proto.HTTPStatusError{Status: http.StatusServiceUnavailable},
	}

	stage := &LLMStage{
		Provider:   provider,
		MaxRetries: 3,
		SignalBus:  sigBus,
		EventBus:   event.NewBus(),
		Logger:     logger,
	}
	state := &State{
		SessionID: "s-pause-int",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
	}

	go func() {
		time.Sleep(5 * time.Millisecond)
		sigBus.Send("s-pause-int", signal.Pause)
		time.Sleep(10 * time.Millisecond)
		sigBus.Send("s-pause-int", signal.Interrupt)
	}()

	err := stage.Process(context.Background(), state)
	if !errors.Is(err, ErrInterrupted) {
		t.Errorf("expected ErrInterrupted, got %v", err)
	}
}

// --- Test helpers ---

type testCheckHandler struct {
	err error
}

func (h *testCheckHandler) Name() string                                  { return "test-check" }
func (h *testCheckHandler) Handle(_ context.Context, _ event.Event) error { return h.err }

type testEchoTool struct{}

func (t *testEchoTool) Name() string { return "echo" }
func (t *testEchoTool) Execute(_ context.Context, call types.ToolCall) (*types.ToolResult, error) {
	return &types.ToolResult{
		ToolCallID: call.ID,
		Content:    call.Arguments,
	}, nil
}

type emptyMemory struct{}

func (m *emptyMemory) Read(_ context.Context, _ string) ([]types.Message, error) {
	return nil, nil
}
func (m *emptyMemory) Write(_ context.Context, _ string, _ types.Message) error { return nil }
func (m *emptyMemory) Replace(_ context.Context, _ string, _ []types.Message) error {
	return errors.New("empty memory")
}

type callbackTestStage struct {
	onProcess func(context.Context, *State) error
	signalCh  chan string
}

func (s *callbackTestStage) Name() string { return "callback-test" }
func (s *callbackTestStage) Clone() Stage {
	return &callbackTestStage{onProcess: s.onProcess, signalCh: s.signalCh}
}
func (s *callbackTestStage) Process(ctx context.Context, state *State) error {
	return s.onProcess(ctx, state)
}
