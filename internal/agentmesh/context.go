package agentmesh

import "context"

// ctxKey is an unexported context key type for agentmesh values.
type ctxKey int

const (
	keySessionID ctxKey = iota
	keyDelegationDepth
)

// WithParentSession returns a ctx carrying the parent session ID, so the
// delegate_to_agent tool can stamp DelegatePayload.ParentSessionID without
// the LLM having to pass it.
func WithParentSession(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, keySessionID, sessionID)
}

// WithDelegationDepth returns a ctx carrying the current delegation depth.
// The delegate tool reads this and increments by one for the child.
func WithDelegationDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, keyDelegationDepth, depth)
}

func sessionIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(keySessionID).(string)
	return v
}

func depthFromCtx(ctx context.Context) int {
	v, _ := ctx.Value(keyDelegationDepth).(int)
	return v
}
