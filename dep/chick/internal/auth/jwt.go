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
	ContextKeyAgentID  contextKey = "agent_id"
	ContextKeyClientIP contextKey = "client_ip"
)

// Claims represents JWT claims.
type Claims struct {
	AgentID uint `json:"agent_id"`
	jwt.RegisteredClaims
}

type Authenticator struct {
	secret []byte
}

func New(secret string) *Authenticator {
	if secret == "" {
		secret = randomHex(32)
	}
	return &Authenticator{
		secret: []byte(secret),
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
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

// ClientIPFromContext extracts client IP from context.
func ClientIPFromContext(ctx context.Context) string {
	ip, _ := ctx.Value(ContextKeyClientIP).(string)
	return ip
}

// WithClientIP embeds client IP into context.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ContextKeyClientIP, ip)
}
