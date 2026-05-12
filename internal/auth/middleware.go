package auth

import (
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

// HTTPMiddleware wraps an http.Handler to inject JWT identity into the request context.
// Unlike the old implementation, it does NOT block unauthenticated requests — the
// resolver decides whether auth is required. It only parses the token if present.
// Pass exact paths in skipPaths to bypass token parsing entirely (e.g., /health).
func (a *Authenticator) HTTPMiddleware(next http.Handler, skipPaths ...string) http.Handler {
	skip := make(map[string]bool)
	for _, p := range skipPaths {
		skip[p] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path-based skip (no token parsing at all)
		if skip[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		// If a Bearer token is present, parse it and inject the agent ID.
		// If absent or invalid, the request still passes through — the resolver
		// calls requireAuth() for operations that need authentication.
		auth := r.Header.Get("Authorization")
		if auth != "" && strings.HasPrefix(auth, bearerPrefix) {
			tokenStr := strings.TrimPrefix(auth, bearerPrefix)
			claims, err := a.ValidateToken(tokenStr)
			if err == nil {
				ctx := WithAgentID(r.Context(), claims.AgentID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// MCPTokenValidator validates MCP tool call tokens.
type MCPTokenValidator struct {
	auth *Authenticator
}

func NewMCPTokenValidator(auth *Authenticator) *MCPTokenValidator {
	return &MCPTokenValidator{auth: auth}
}

// Validate extracts agent ID from a Bearer token string.
func (v *MCPTokenValidator) Validate(tokenStr string) (uint, error) {
	if tokenStr == "" {
		return 0, nil
	}
	claims, err := v.auth.ValidateToken(tokenStr)
	if err != nil {
		return 0, err
	}
	return claims.AgentID, nil
}
