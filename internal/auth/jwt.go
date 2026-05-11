package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	ContextKeyAgentID contextKey = "agent_id"
)

// Claims represents JWT claims.
type Claims struct {
	AgentID uint `json:"agent_id"`
	jwt.RegisteredClaims
}

type Authenticator struct {
	secret     []byte
	bootstrap  string
	bootstrapUsed bool
}

func New(secret string, bootstrapToken string) *Authenticator {
	if secret == "" {
		secret = randomHex(32)
	}
	if bootstrapToken == "" {
		bootstrapToken = randomHex(16)
	}
	return &Authenticator{
		secret:    []byte(secret),
		bootstrap: bootstrapToken,
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// BootstrapToken returns the current bootstrap token.
func (a *Authenticator) BootstrapToken() string {
	return a.bootstrap
}

// UseBootstrapToken validates and consumes the bootstrap token.
// Returns true if valid and consumed.
func (a *Authenticator) UseBootstrapToken(token string) bool {
	if a.bootstrapUsed {
		return false
	}
	if token == a.bootstrap {
		a.bootstrapUsed = true
		return true
	}
	return false
}

// GenerateToken creates a signed JWT for the given agent ID.
func (a *Authenticator) GenerateToken(agentID uint) (string, error) {
	claims := Claims{
		AgentID: agentID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "chick",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.secret)
}

// ValidateToken parses and validates a JWT token string.
func (a *Authenticator) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

// AgentIDFromContext extracts agent ID from context.
func AgentIDFromContext(ctx context.Context) (uint, bool) {
	id, ok := ctx.Value(ContextKeyAgentID).(uint)
	return id, ok
}

// WithAgentID embeds agent ID into context.
func WithAgentID(ctx context.Context, agentID uint) context.Context {
	return context.WithValue(ctx, ContextKeyAgentID, agentID)
}
