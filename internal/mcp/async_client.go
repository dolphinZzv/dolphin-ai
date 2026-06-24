package mcp

import (
	"context"
	"sync"
	"time"

	"dolphin/internal/types"
)

// ClientExecutor mirrors tool.Executor — any *Client or *StdioClient satisfies it.
type ClientExecutor interface {
	List(ctx context.Context) ([]types.ToolDef, error)
	Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error)
}

// Connector creates and initialises an MCP client, returning the live client
// and the cached tool list. It is called once from a background goroutine.
type Connector func(ctx context.Context) (ClientExecutor, []types.ToolDef, error)

type asyncState int

const (
	asyncIdle asyncState = iota
	asyncConnecting
	asyncConnected
	asyncFailed
)

// AsyncClient registers immediately but connects to the MCP server in a
// background goroutine. List returns an empty slice until the connection
// succeeds; Execute returns an error until the real client is available.
type AsyncClient struct {
	mu        sync.Mutex
	state     asyncState
	cached    []types.ToolDef
	real      ClientExecutor
	connector Connector
	onConnect func(count int)
}

// NewAsyncClient creates the client and starts a background goroutine that
// calls connector. Use SetOnConnect to receive a notification when the
// connection succeeds (useful for logging).
func NewAsyncClient(connector Connector) *AsyncClient {
	a := &AsyncClient{
		state:     asyncIdle,
		connector: connector,
	}
	go a.connect()
	return a
}

// SetOnConnect registers a callback that fires once the MCP server connects
// successfully. If the client has already connected the callback is invoked
// immediately under the mutex. The argument is the number of tools discovered.
func (a *AsyncClient) SetOnConnect(fn func(count int)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onConnect = fn
	if a.state == asyncConnected {
		fn(len(a.cached))
	}
}

// List returns the cached tool definitions if the connection has succeeded,
// or an empty slice otherwise. It never blocks.
func (a *AsyncClient) List(ctx context.Context) ([]types.ToolDef, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == asyncConnected {
		cp := make([]types.ToolDef, len(a.cached))
		copy(cp, a.cached)
		return cp, nil
	}
	return nil, nil
}

// Execute delegates to the real client if connected, otherwise returns an
// error result so the caller sees a clear failure rather than a silent no-op.
func (a *AsyncClient) Execute(ctx context.Context, call types.ToolCall) (*types.ToolResult, error) {
	a.mu.Lock()
	real := a.real
	a.mu.Unlock()

	if real != nil {
		return real.Execute(ctx, call)
	}
	return &types.ToolResult{
		ToolCallID: call.ID,
		Content:    "mcp server not connected yet",
		IsError:    true,
	}, nil
}

// State returns the current asyncState for inspection in tests.
func (a *AsyncClient) State() asyncState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// connect is the background goroutine entry point. It runs once and transitions
// the state machine from idle → connecting → connected | failed.
func (a *AsyncClient) connect() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	a.mu.Lock()
	a.state = asyncConnecting
	connector := a.connector
	a.mu.Unlock()

	real, defs, err := connector(ctx)

	a.mu.Lock()
	defer a.mu.Unlock()

	if err != nil {
		a.state = asyncFailed
		return
	}

	a.state = asyncConnected
	a.real = real
	a.cached = defs

	if a.onConnect != nil {
		a.onConnect(len(defs))
	}
}
