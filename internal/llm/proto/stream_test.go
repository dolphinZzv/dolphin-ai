package proto

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dolphin/internal/llm"
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

func TestHTTPStatusError_ErrorMessage(t *testing.T) {
	t.Run("wraps provider error when present", func(t *testing.T) {
		e := &HTTPStatusError{Status: 429, err: errors.New("provider: rate limited")}
		if e.Error() != "provider: rate limited" {
			t.Errorf("Error = %q, want %q", e.Error(), "provider: rate limited")
		}
	})

	t.Run("falls back to status+body when no wrapped error", func(t *testing.T) {
		e := &HTTPStatusError{Status: 400, Body: "invalid model"}
		msg := e.Error()
		if msg != "llm: status 400 (body: invalid model)" {
			t.Errorf("Error = %q, want %q", msg, "llm: status 400 (body: invalid model)")
		}
	})

	t.Run("fallback with empty body", func(t *testing.T) {
		e := &HTTPStatusError{Status: 503}
		msg := e.Error()
		if msg != "llm: status 503 (body: )" {
			t.Errorf("Error = %q", msg)
		}
	})
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

func TestSSEReader_New(t *testing.T) {
	r := NewSSEReader(strings.NewReader(""))
	if r == nil {
		t.Fatal("NewSSEReader returned nil")
	}
}

func TestSSEReader_Next(t *testing.T) {
	t.Run("returns data payload", func(t *testing.T) {
		r := NewSSEReader(strings.NewReader("data: hello\n\n"))
		data, done, err := r.Next()
		if err != nil {
			t.Fatalf("Next error: %v", err)
		}
		if done {
			t.Fatal("unexpected done")
		}
		if string(data) != "hello" {
			t.Errorf("data = %q, want %q", string(data), "hello")
		}
	})

	t.Run("returns [DONE] sentinel", func(t *testing.T) {
		r := NewSSEReader(strings.NewReader("data: [DONE]\n\n"))
		_, done, err := r.Next()
		if err != nil {
			t.Fatalf("Next error: %v", err)
		}
		if !done {
			t.Fatal("expected done")
		}
	})

	t.Run("skips non-data lines", func(t *testing.T) {
		input := "event: ping\nid: 1\ndata: payload\n\n"
		r := NewSSEReader(strings.NewReader(input))
		data, done, err := r.Next()
		if err != nil {
			t.Fatalf("Next error: %v", err)
		}
		if done {
			t.Fatal("unexpected done")
		}
		if string(data) != "payload" {
			t.Errorf("data = %q, want %q", string(data), "payload")
		}
	})

	t.Run("skips empty lines", func(t *testing.T) {
		r := NewSSEReader(strings.NewReader("\n\n\ndata: value\n\n"))
		data, _, err := r.Next()
		if err != nil {
			t.Fatalf("Next error: %v", err)
		}
		if string(data) != "value" {
			t.Errorf("data = %q, want %q", string(data), "value")
		}
	})

	t.Run("returns io.EOF at end of stream", func(t *testing.T) {
		r := NewSSEReader(strings.NewReader("data: only\n\n"))
		_, _, _ = r.Next()
		_, _, err := r.Next()
		if err != io.EOF {
			t.Errorf("expected io.EOF, got %v", err)
		}
	})

	t.Run("returns empty data payload", func(t *testing.T) {
		r := NewSSEReader(strings.NewReader("data: \n\n"))
		data, done, err := r.Next()
		if err != nil {
			t.Fatalf("Next error: %v", err)
		}
		if done {
			t.Fatal("unexpected done")
		}
		if string(data) != "" {
			t.Errorf("data = %q, want empty", string(data))
		}
	})

	t.Run("handles line without trailing newline", func(t *testing.T) {
		r := NewSSEReader(strings.NewReader("data: hello"))
		data, _, err := r.Next()
		if err != nil {
			t.Fatalf("Next error: %v", err)
		}
		if string(data) != "hello" {
			t.Errorf("data = %q", string(data))
		}
	})

		t.Run("[DONE] with trailing JSON returns data", func(t *testing.T) {
			r := NewSSEReader(strings.NewReader("data: [DONE]{\"usage\":{\"prompt_tokens\":100}}\n\n"))
			data, done, err := r.Next()
			if err != nil {
				t.Fatalf("Next error: %v", err)
			}
			if !done {
				t.Fatal("expected done")
			}
			if string(data) != "{\"usage\":{\"prompt_tokens\":100}}" {
				t.Errorf("data = %q", string(data))
			}
		})
}

func TestDoStream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: hello\n\n"))
		_, _ = w.Write([]byte("data: world\n\n"))
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	ch, err := DoStream(context.Background(), req, 5*time.Second,
		func(r io.Reader) ChunkDecoder { return &sseDecoder{r: NewSSEReader(r)} },
		nil, nil)
	if err != nil {
		t.Fatalf("DoStream error: %v", err)
	}

	var results []string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("chunk error: %v", chunk.Error)
		}
		results = append(results, chunk.Content)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 chunks (hello, world, done), got %d: %v", len(results), results)
	}
	if results[0] != "hello" || results[1] != "world" {
		t.Errorf("results = %v, want [hello world ...]", results)
	}
}

func TestDoStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	ch, err := DoStream(context.Background(), req, 5*time.Second,
		func(r io.Reader) ChunkDecoder { return &sseDecoder{r: NewSSEReader(r)} },
		func(status int, body []byte) error {
			return errors.New("provider: " + string(body))
		}, nil)
	if err != nil {
		t.Fatalf("DoStream error: %v", err)
	}

	var lastErr error
	for chunk := range ch {
		if chunk.Error != nil {
			lastErr = chunk.Error
		}
	}
	if lastErr == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(lastErr.Error(), "bad request") {
		t.Errorf("error = %q, want containing 'bad request'", lastErr.Error())
	}
}

func TestDoComplete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":"ok"}`))
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	ch, err := DoComplete(context.Background(), req, 5*time.Second,
		func(body []byte) (llm.LLMChunk, error) {
			if bytes.Contains(body, []byte("ok")) {
				return llm.LLMChunk{Content: "ok"}, nil
			}
			return llm.LLMChunk{}, errors.New("unexpected body")
		}, nil)
	if err != nil {
		t.Fatalf("DoComplete error: %v", err)
	}

	chunk := <-ch
	if chunk.Error != nil {
		t.Fatalf("chunk error: %v", chunk.Error)
	}
	if chunk.Content != "ok" {
		t.Errorf("Content = %q, want %q", chunk.Content, "ok")
	}
	if !chunk.Done {
		t.Error("expected Done=true")
	}
}

func TestDoComplete_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	ch, err := DoComplete(context.Background(), req, 5*time.Second,
		func(body []byte) (llm.LLMChunk, error) { return llm.LLMChunk{}, nil }, nil)
	if err != nil {
		t.Fatalf("DoComplete error: %v", err)
	}

	var lastErr error
	for chunk := range ch {
		if chunk.Error != nil {
			lastErr = chunk.Error
		}
	}
	if lastErr == nil {
		t.Fatal("expected error, got nil")
	}
}

type sseDecoder struct {
	r *SSEReader
}

func (d *sseDecoder) Decode() (llm.LLMChunk, error) {
	data, done, err := d.r.Next()
	if err != nil {
		return llm.LLMChunk{}, err
	}
	if done {
		return llm.LLMChunk{}, io.EOF
	}
	return llm.LLMChunk{Content: string(data)}, nil
}
