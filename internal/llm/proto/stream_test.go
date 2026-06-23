package proto

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestHTTPStatusError_IsRetryable(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{
		{http.StatusTooManyRequests, true},     // 429
		{http.StatusInternalServerError, true}, // 500
		{http.StatusBadGateway, true},          // 502
		{http.StatusServiceUnavailable, true},  // 503
		{http.StatusBadRequest, false},         // 400
		{http.StatusUnauthorized, false},       // 401
		{http.StatusForbidden, false},          // 403
		{http.StatusNotFound, false},           // 404
	}
	for _, tt := range tests {
		e := &HTTPStatusError{Status: tt.status}
		if got := e.IsRetryable(); got != tt.want {
			t.Errorf("status %d: IsRetryable=%v want %v", tt.status, got, tt.want)
		}
	}
}

func TestHTTPStatusError_Unwrap(t *testing.T) {
	inner := errors.New("provider message")
	e := &HTTPStatusError{Status: 429, err: inner}
	if !errors.Is(e, inner) {
		t.Error("errors.Is should find the wrapped provider error")
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"0", 0},
		{"5", 5 * time.Second},
		{"120", 120 * time.Second},
		{"not-a-number", 0},
		// HTTP-date form: a fixed past date yields 0 (no negative backoff).
		{"Wed, 21 Oct 2015 07:28:00 GMT", 0},
	}
	for _, tt := range tests {
		got := parseRetryAfter(tt.in)
		if got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseRetryAfter_HTTPDateFuture(t *testing.T) {
	// A date roughly 10s in the future should parse to ~10s (allow slack).
	future := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(future)
	if got <= 0 || got > 15*time.Second {
		t.Errorf("parseRetryAfter(future date %q) = %v, want ~10s", future, got)
	}
}

func TestDecodeError_WrapsStatusAndRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "30")
	err := decodeError(nil, http.StatusTooManyRequests, h, []byte(`{"error":"rate limited"}`))

	var hse *HTTPStatusError
	if !errors.As(err, &hse) {
		t.Fatalf("expected *HTTPStatusError, got %T", err)
	}
	if hse.Status != http.StatusTooManyRequests {
		t.Errorf("Status = %d, want 429", hse.Status)
	}
	if hse.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %v, want 30s", hse.RetryAfter)
	}
	if !hse.IsRetryable() {
		t.Error("429 should be retryable")
	}
}

func TestDecodeError_NoHeader(t *testing.T) {
	err := decodeError(nil, http.StatusBadRequest, nil, []byte("bad request"))
	var hse *HTTPStatusError
	if !errors.As(err, &hse) {
		t.Fatalf("expected *HTTPStatusError, got %T", err)
	}
	if hse.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v, want 0 when header absent", hse.RetryAfter)
	}
	if hse.IsRetryable() {
		t.Error("400 should not be retryable")
	}
}

func TestDecodeError_PreservesProviderMessage(t *testing.T) {
	providerErr := func(status int, body []byte) error {
		return errors.New("provider: bad model")
	}
	err := decodeError(providerErr, 400, nil, nil)
	var hse *HTTPStatusError
	if !errors.As(err, &hse) {
		t.Fatalf("expected *HTTPStatusError, got %T", err)
	}
	if err.Error() != "provider: bad model" {
		t.Errorf("Error() = %q, want provider message", err.Error())
	}
}
