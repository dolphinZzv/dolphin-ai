package proto

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/llm"
)

// ChunkDecoder owns the semantics of one streaming response. proto calls
// Decode repeatedly until it returns io.EOF; the decoder is responsible for
// driving its own SSEReader over the response body.
type ChunkDecoder interface {
	Decode() (llm.LLMChunk, error)
}

// ErrorDecoder parses a non-200 response body into an error. Providers inject
// their own (e.g. OpenAI's {"error":{"message":...}} shape); a nil value uses
// a generic "status N" message.
type ErrorDecoder func(status int, body []byte) error

// DoStream sends httpReq, checks the status, and pumps the SSE response through
// a fresh decoder from newDecoder. The caller owns the request (URL, auth
// headers, body); proto owns only the HTTP lifecycle and error envelope.
func DoStream(
	ctx context.Context,
	httpReq *http.Request,
	timeout time.Duration,
	newDecoder func(io.Reader) ChunkDecoder,
	errDecode ErrorDecoder,
	logger *zap.Logger,
) (<-chan llm.LLMChunk, error) {
	ch := make(chan llm.LLMChunk)

	go func() {
		defer close(ch)

		send := func(chunk llm.LLMChunk) (ok bool) {
			select {
			case ch <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}

		cl := &http.Client{Timeout: timeout}
		//nolint:gosec // G704: httpReq.URL comes from operator config (base_url), not user input.
		resp, err := cl.Do(httpReq)
		if err != nil {
			send(llm.LLMChunk{Error: fmt.Errorf("llm: request failed: %w", err)})
			return
		}
		defer func() { _ = resp.Body.Close() }()

		cleanup := context.AfterFunc(ctx, func() { resp.Body.Close() })
		defer cleanup()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			send(llm.LLMChunk{Error: decodeError(errDecode, resp.StatusCode, resp.Header, body)})
			return
		}

		dec := newDecoder(resp.Body)
		for {
			chunk, err := dec.Decode()
			if errors.Is(err, io.EOF) {
				send(llm.LLMChunk{Done: true})
				return
			}
			if err != nil {
				send(llm.LLMChunk{Error: fmt.Errorf("llm: decode: %w", err)})
				return
			}
			if !send(chunk) {
				return
			}
		}
	}()

	return ch, nil
}

// DoComplete sends httpReq and decodes the single non-streaming response body
// via decode. Used by providers whose model is configured with stream=false.
func DoComplete(
	ctx context.Context,
	httpReq *http.Request,
	timeout time.Duration,
	decode func([]byte) (llm.LLMChunk, error),
	errDecode ErrorDecoder,
) (<-chan llm.LLMChunk, error) {
	ch := make(chan llm.LLMChunk, 1)

	go func() {
		defer close(ch)

		cl := &http.Client{Timeout: timeout}
		//nolint:gosec // G704: httpReq.URL comes from operator config (base_url), not user input.
		resp, err := cl.Do(httpReq)
		if err != nil {
			ch <- llm.LLMChunk{Error: fmt.Errorf("llm: request failed: %w", err)}
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			ch <- llm.LLMChunk{Error: decodeError(errDecode, resp.StatusCode, resp.Header, body)}
			return
		}

		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			ch <- llm.LLMChunk{Error: fmt.Errorf("llm: read response: %w", err)}
			return
		}

		chunk, err := decode(raw)
		if err != nil {
			ch <- llm.LLMChunk{Error: fmt.Errorf("llm: decode response: %w", err)}
			return
		}
		chunk.Done = true
		ch <- chunk
	}()

	return ch, nil
}

// HTTPStatusError wraps a non-200 response from an LLM provider. It carries
// the status code and the parsed Retry-After hint so the retry layer can
// back off appropriately on 429 / 503. The body is included so the error
// stays human-readable in logs and the UI.
type HTTPStatusError struct {
	Status     int
	RetryAfter time.Duration // zero when absent or unparseable
	Body       string
	err        error // provider-specific decoded error, if any
}

func (e *HTTPStatusError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return fmt.Sprintf("llm: status %d (body: %s)", e.Status, e.Body)
}

func (e *HTTPStatusError) Unwrap() error { return e.err }

// IsRetryable reports whether the retry loop should retry on this status.
// 429 (rate limit) and 5xx (transient server errors) are retryable; other
// 4xx are permanent (bad request, auth, etc.) and must not be retried.
func (e *HTTPStatusError) IsRetryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// decodeError builds the error for a non-200 response. If the provider
// supplied an ErrorDecoder it is consulted first (for human-readable
// messages); the result is wrapped in an HTTPStatusError so the retry layer
// can always recover the status code and Retry-After regardless of how the
// provider chose to format the body.
func decodeError(errDecode ErrorDecoder, status int, header http.Header, body []byte) error {
	var decoded error
	if errDecode != nil {
		decoded = errDecode(status, body)
	}
	hse := &HTTPStatusError{
		Status: status,
		Body:   string(body),
		err:    decoded,
	}
	if header != nil {
		hse.RetryAfter = parseRetryAfter(header.Get("Retry-After"))
	}
	return hse
}

// parseRetryAfter parses a Retry-After header. Two forms are supported per
// RFC 7231: a non-negative integer (seconds) or an HTTP-date. Returns zero
// when the value is absent or unparseable — callers treat zero as "no hint".
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	// Seconds form.
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	// HTTP-date form.
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
		return 0
	}
	return 0
}
