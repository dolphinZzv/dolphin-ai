package agent

import (
	"dolphin/internal/config"
	ctx "dolphin/internal/context"
)

// ContextBuilder builds the system prompt from context files.
// Delegates to internal/context package.
type ContextBuilder struct {
	b *ctx.Builder
}

func NewContextBuilder() *ContextBuilder {
	return &ContextBuilder{b: ctx.NewBuilder()}
}

func (b *ContextBuilder) Build() (string, error) {
	return b.b.Build()
}

func (b *ContextBuilder) BuildForAgent(agentName string) (string, error) {
	return b.b.BuildForAgent(agentName)
}

// SetRenderData configures template variable injection from the application config.
func (b *ContextBuilder) SetRenderData(cfg *config.Config) {
	b.b.SetRenderData(ctx.NewRenderData(cfg))
}
