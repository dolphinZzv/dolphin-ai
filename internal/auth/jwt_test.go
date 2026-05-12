package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateAndValidateToken(t *testing.T) {
	a := New("my-secret-key", "bootstrap-token")

	token, err := a.GenerateToken(uint(42))
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := a.ValidateToken(token)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if claims.AgentID != 42 {
		t.Errorf("expected agentID 42, got %d", claims.AgentID)
	}
}

func TestValidateInvalidToken(t *testing.T) {
	a := New("my-secret-key", "bootstrap-token")

	_, err := a.ValidateToken("invalid-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}

	_, err = a.ValidateToken("")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestValidateTokenWrongSecret(t *testing.T) {
	a1 := New("secret-1", "bootstrap")
	a2 := New("secret-2", "bootstrap")

	token, _ := a1.GenerateToken(uint(1))
	_, err := a2.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for token signed with different secret")
	}
}

func TestBootstrapToken(t *testing.T) {
	a := New("secret", "my-bootstrap-token")

	ok := a.UseBootstrapToken("my-bootstrap-token")
	if !ok {
		t.Fatal("expected bootstrap token to be accepted")
	}

	ok = a.UseBootstrapToken("my-bootstrap-token")
	if ok {
		t.Fatal("expected bootstrap token to be rejected on second use")
	}
}

func TestBootstrapTokenWrong(t *testing.T) {
	a := New("secret", "bootstrap-token")

	ok := a.UseBootstrapToken("wrong-token")
	if ok {
		t.Fatal("expected wrong bootstrap token to be rejected")
	}
}

func TestBootstrapTokenEmpty(t *testing.T) {
	a := New("secret", "")

	ok := a.UseBootstrapToken("anything")
	if ok {
		t.Fatal("expected empty bootstrap token config to reject all")
	}
}

func TestAgentIDFromContext(t *testing.T) {
	ctx := context.Background()
	_, ok := AgentIDFromContext(ctx)
	if ok {
		t.Fatal("expected no agent ID in empty context")
	}

	ctx = WithAgentID(ctx, uint(99))
	id, ok := AgentIDFromContext(ctx)
	if !ok {
		t.Fatal("expected agent ID in context")
	}
	if id != 99 {
		t.Errorf("expected 99, got %d", id)
	}
}

func TestHTTPMiddleware(t *testing.T) {
	a := New("my-secret-key", "bootstrap")
	token, _ := a.GenerateToken(uint(5))

	// Valid token — middleware should inject agent ID into context
	validHandler := a.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := AgentIDFromContext(r.Context())
		if !ok {
			t.Error("expected agent ID in context")
		}
		if id != 5 {
			t.Errorf("expected 5, got %d", id)
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	validHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	// Missing token — middleware passes through, handler runs without agent ID
	missingHandler := a.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := AgentIDFromContext(r.Context())
		if ok {
			t.Error("expected no agent ID in context for unauthenticated request")
		}
		w.WriteHeader(http.StatusOK)
	}))
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	rec2 := httptest.NewRecorder()
	missingHandler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200 (pass-through), got %d", rec2.Code)
	}

	// Invalid token — middleware passes through (doesn't set agent ID for bad tokens)
	invalidHandler := a.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := AgentIDFromContext(r.Context())
		if ok {
			t.Error("expected no agent ID in context for invalid token")
		}
		w.WriteHeader(http.StatusOK)
	}))
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.Header.Set("Authorization", "Bearer invalid-token")
	rec3 := httptest.NewRecorder()
	invalidHandler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Errorf("expected 200 (pass-through), got %d", rec3.Code)
	}
}

func TestHTTPMiddlewareSkipPaths(t *testing.T) {
	a := New("secret", "bootstrap")
	handler := a.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), "/health")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for skip path, got %d", rec.Code)
	}
}

func TestMCPTokenValidator(t *testing.T) {
	a := New("secret", "bootstrap")
	v := NewMCPTokenValidator(a)

	// Empty token
	id, err := v.Validate("")
	if err != nil {
		t.Fatalf("empty token: %v", err)
	}
	if id != 0 {
		t.Errorf("expected 0, got %d", id)
	}

	// Valid token
	token, _ := a.GenerateToken(uint(10))
	id, err = v.Validate(token)
	if err != nil {
		t.Fatalf("valid token: %v", err)
	}
	if id != 10 {
		t.Errorf("expected 10, got %d", id)
	}

	// Invalid token
	_, err = v.Validate("bad-token")
	if err == nil {
		t.Fatal("expected error for bad token")
	}
}
