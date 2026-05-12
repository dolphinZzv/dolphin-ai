package agent

import ctx "dolphin/internal/context"

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
