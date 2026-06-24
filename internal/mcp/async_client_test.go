package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"dolphin/internal/types"
)

// fakeExecutor implements ClientExecutor for tests.
type fakeExecutor struct {
	mu      sync.Mutex
	defs    []types.ToolDef
	calls   []types.ToolCall
	listErr error
	execErr string
}

func (f *fakeExecutor) List(ctx context.Context) ([]types.ToolDef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.defs, nil
}

func (f *fakeExecutor) Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	return &types.ToolResult{
		ToolCallID: call.ID,
		Content:    f.execErr,
		IsError:    f.execErr != "",
	}, nil
}

func makeConnector(defs []types.ToolDef, err error) Connector {
	return func(ctx context.Context) (ClientExecutor, []types.ToolDef, error) {
		if err != nil {
			return nil, nil, err
		}
		f := &fakeExecutor{defs: defs}
		return f, defs, nil
	}
}

func TestAsyncClient_ListReturnsEmptyBeforeConnect(t *testing.T) {
	blockCh := make(chan struct{})
	connector := func(ctx context.Context) (ClientExecutor, []types.ToolDef, error) {
		<-blockCh // block until test is done
		return nil, nil, ctx.Err()
	}

	ac := NewAsyncClient(connector)
	defer close(blockCh)

	// List should return empty immediately, not block.
	defs, err := ac.List(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected empty, got %d tools", len(defs))
	}
}

func TestAsyncClient_ListReturnsCachedAfterConnect(t *testing.T) {
	wantDefs := []types.ToolDef{
		{Name: "tool1", Description: "desc1"},
		{Name: "tool2", Description: "desc2"},
	}
	ac := NewAsyncClient(makeConnector(wantDefs, nil))

	// Wait for async connect to finish.
	for ac.State() != asyncConnected {
		time.Sleep(10 * time.Millisecond)
	}

	defs, err := ac.List(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(defs))
	}
	if defs[0].Name != "tool1" || defs[1].Name != "tool2" {
		t.Fatalf("unexpected tools: %+v", defs)
	}
}

func TestAsyncClient_ExecuteDelegatesAfterConnect(t *testing.T) {
	wantDefs := []types.ToolDef{{Name: "greet"}}
	ac := NewAsyncClient(makeConnector(wantDefs, nil))

	for ac.State() != asyncConnected {
		time.Sleep(10 * time.Millisecond)
	}

	result, err := ac.Execute(context.Background(), types.ToolCall{ID: "c1", Name: "greet"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}
}

func TestAsyncClient_ExecuteReturnsErrorBeforeConnect(t *testing.T) {
	blockCh := make(chan struct{})
	connector := func(ctx context.Context) (ClientExecutor, []types.ToolDef, error) {
		<-blockCh
		return nil, nil, ctx.Err()
	}

	ac := NewAsyncClient(connector)
	defer close(blockCh)

	result, err := ac.Execute(context.Background(), types.ToolCall{ID: "c1", Name: "greet"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when not connected")
	}
	if result.Content == "" {
		t.Fatal("expected error message, got empty")
	}
}

func TestAsyncClient_SetOnConnectFiresAfterAsyncConnect(t *testing.T) {
	wantDefs := []types.ToolDef{{Name: "a"}}
	ac := NewAsyncClient(makeConnector(wantDefs, nil))

	var (
		gotCount int
		fired    = make(chan struct{})
	)
	ac.SetOnConnect(func(count int) {
		gotCount = count
		close(fired)
	})

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("onConnect never fired")
	}

	if gotCount != 1 {
		t.Fatalf("expected count 1, got %d", gotCount)
	}
}

func TestAsyncClient_SetOnConnectFiresImmediatelyIfAlreadyConnected(t *testing.T) {
	wantDefs := []types.ToolDef{{Name: "x"}, {Name: "y"}}
	ac := NewAsyncClient(makeConnector(wantDefs, nil))

	// Wait for connect.
	for ac.State() != asyncConnected {
		time.Sleep(10 * time.Millisecond)
	}

	// SetOnConnect after already connected should fire immediately.
	var gotCount int
	ac.SetOnConnect(func(count int) {
		gotCount = count
	})

	if gotCount != 2 {
		t.Fatalf("expected count 2, got %d", gotCount)
	}
}

func TestAsyncClient_ConnectorErrorSetsFailed(t *testing.T) {
	connErr := errors.New("connection refused")
	ac := NewAsyncClient(makeConnector(nil, connErr))

	// Wait for async connect to finish (fail).
	for ac.State() == asyncConnecting || ac.State() == asyncIdle {
		time.Sleep(10 * time.Millisecond)
	}

	if ac.State() != asyncFailed {
		t.Fatalf("expected asyncFailed, got %d", ac.State())
	}

	// List should return empty, not error.
	defs, err := ac.List(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected empty, got %d", len(defs))
	}
}

func TestAsyncClient_ListReturnsCopy(t *testing.T) {
	wantDefs := []types.ToolDef{{Name: "original"}}
	ac := NewAsyncClient(makeConnector(wantDefs, nil))

	for ac.State() != asyncConnected {
		time.Sleep(10 * time.Millisecond)
	}

	defs, _ := ac.List(context.Background())
	defs[0].Name = "modified"

	// The cached copy should not be affected.
	defs2, _ := ac.List(context.Background())
	if defs2[0].Name != "original" {
		t.Fatalf("expected 'original', got '%s'", defs2[0].Name)
	}
}
