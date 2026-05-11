package auth

import (
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

// HTTPMiddleware wraps an http.Handler to require JWT authentication.
func (a *Authenticator) HTTPMiddleware(next http.Handler, skipAuth ...string) http.Handler {
	skip := make(map[string]bool)
	for _, p := range skipAuth {
		skip[p] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skip[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, bearerPrefix) {
			http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
			return
		}

		tokenStr := strings.TrimPrefix(auth, bearerPrefix)
		claims, err := a.ValidateToken(tokenStr)
		if err != nil {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}

		ctx := WithAgentID(r.Context(), claims.AgentID)
		next.ServeHTTP(w, r.WithContext(ctx))
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
