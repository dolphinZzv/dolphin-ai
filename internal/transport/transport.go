package transport

import (
	"context"
	"fmt"
	"sync"

	"dolphin/internal/common"
)

// IO is the transport interface — all transports must implement it.
type IO interface {
	ID() string
	Context() string
	Read(ctx context.Context) (string, error)
	Write(ctx context.Context, text string) error
	Flush() error
	Close() error
	Capability() Capability
	Tools() []common.ToolDesc
}

// Capability describes transport features.
type Capability struct {
	Interactive bool
	Streamable  bool
	NestRead    bool
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
		return nil, fmt.Errorf("unknown transport type: %s", typ)
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
