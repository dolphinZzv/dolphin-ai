package auth

import (
	"net"
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
		// Inject client IP into context for rate limiting / audit
		ctx := WithClientIP(r.Context(), clientIPFromRequest(r))

		// Path-based skip (no token parsing at all)
		if skip[r.URL.Path] {
			next.ServeHTTP(w, r.WithContext(ctx))
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
				ctx = WithAgentID(ctx, claims.AgentID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// clientIPFromRequest extracts the client IP from X-Forwarded-For, X-Real-IP, or RemoteAddr.
func clientIPFromRequest(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i != -1 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
