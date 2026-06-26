package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_AllowSpecificOrigin(t *testing.T) {
	mw := CORSMiddleware([]string{"http://example.com"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
		t.Error("expected CORS origin to be set for allowed origin")
	}
}

func TestCORS_DenyOriginNotInList(t *testing.T) {
	mw := CORSMiddleware([]string{"http://example.com"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("expected no CORS headers for disallowed origin")
	}
}

func TestCORS_AllowWildcard(t *testing.T) {
	mw := CORSMiddleware([]string{"*"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://any-origin.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "http://any-origin.com" {
		t.Error("expected CORS origin to echo any origin when wildcard is set")
	}
}

func TestCORS_DenyWhenEmpty(t *testing.T) {
	mw := CORSMiddleware([]string{})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("expected no CORS headers when allowed origins list is empty")
	}
}

func TestCORS_OptionsRequest(t *testing.T) {
	mw := CORSMiddleware([]string{"http://example.com"})
	var served bool
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS, got %d", rec.Code)
	}
	if served {
		t.Error("expected next handler NOT to be called for OPTIONS request")
	}
}

func TestCORS_HeadersSetCorrectly(t *testing.T) {
	mw := CORSMiddleware([]string{"http://example.com"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("expected Allow-Credentials header")
	}
	if rec.Header().Get("Access-Control-Allow-Headers") != "Authorization, Content-Type" {
		t.Error("expected Allow-Headers header")
	}
	if rec.Header().Get("Access-Control-Allow-Methods") != "GET, POST, OPTIONS" {
		t.Error("expected Allow-Methods header")
	}
}
