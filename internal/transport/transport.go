package transport

import (
	"context"
	"fmt"
	"sync"

	"dolphin/internal/common"
	"dolphin/internal/i18n"
	"dolphin/internal/session"
	"dolphin/internal/types"
)

// IO is the transport interface — all transports must implement it.
type IO interface {
	ID() string
	Context() string
	Start(ctx context.Context) error
	Read(ctx context.Context) (Input, error)
	Write(ctx context.Context, text string) error
	Flush() error
	Close() error
	Capability() Capability
	Tools() []common.ToolDesc
	NewSession(ctx context.Context) *session.Session
	Session() *session.Session
	RequestPermission(ctx context.Context, prompt string) (PermissionResult, error)
	// Confirm asks the user a yes/no question. Returns true for yes, false
	// for no. Used by session-expiry prompts and other simple confirmations
	// that don't need the Once/Always distinction of RequestPermission.
	Confirm(ctx context.Context, prompt string) (bool, error)
	WriteThinking(ctx context.Context, text string) error
	WriteToolCall(ctx context.Context, call types.ToolCall) error
	WriteToolResult(ctx context.Context, result types.ToolResult) error
}

// Input is what a transport delivers from the user. Text carries the typed
// line/command; Parts carries multimodal attachments (images/files). Most
// transports populate only Text.
type Input struct {
	Text  string
	Parts []types.ContentPart
}

// PermissionResult represents the outcome of a permission request.
type PermissionResult int

const (
	PermissionDenied PermissionResult = iota
	PermissionOnce
	PermissionAlways
	PermissionAbort
)

// Capability describes transport features.
type Capability struct {
	Interactive        bool
	Streamable         bool
	NestRead           bool
	RenderTextMarkdown string // "none" or "markdown"
}

// Info carries transport metadata through context.
type Info struct {
	ID       string
	Type     string
	ClientIP string
}

type contextKey struct{}

func WithInfo(ctx context.Context, info *Info) context.Context {
	return context.WithValue(ctx, contextKey{}, info)
}

func GetInfo(ctx context.Context) *Info {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(contextKey{}).(*Info)
	return v
}

// Builder creates a transport from config.
type Builder func(ctx context.Context, cfg map[string]any) (IO, error)

// Registry holds named transport builders.
type Registry struct {
	mu       sync.RWMutex
	builders map[string]Builder
}

func NewRegistry() *Registry {
	return &Registry{builders: make(map[string]Builder)}
}

func (r *Registry) Register(name string, builder Builder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.builders[name] = builder
}

func (r *Registry) Build(ctx context.Context, typ string, cfg map[string]any) (IO, error) {
	r.mu.RLock()
	builder, ok := r.builders[typ]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf(i18n.T("transport.unknown_type"), typ)
	}
	return builder(ctx, cfg)
}

// Global default registry.
var global = NewRegistry()

func Register(name string, builder Builder) {
	global.Register(name, builder)
}

func Build(ctx context.Context, typ string, cfg map[string]any) (IO, error) {
	return global.Build(ctx, typ, cfg)
}
