package provider

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"
)

// RetryProvider wraps a Provider with exponential backoff retry logic.
type RetryProvider struct {
	inner       Provider
	maxAttempts int
	backoffBase time.Duration
}

// NewRetryProvider creates a RetryProvider. If maxAttempts <= 0, defaults to 3.
// If backoffBase <= 0, defaults to 1 second.
func NewRetryProvider(inner Provider, maxAttempts int, backoffBase time.Duration) *RetryProvider {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if backoffBase <= 0 {
		backoffBase = time.Second
	}
	return &RetryProvider{
		inner:       inner,
		maxAttempts: maxAttempts,
		backoffBase: backoffBase,
	}
}

func (r *RetryProvider) Type() ProviderType { return r.inner.Type() }
func (r *RetryProvider) Name() string       { return r.inner.Name() }

func (r *RetryProvider) HealthCheck(ctx context.Context) error {
	return r.inner.HealthCheck(ctx)
}

// Complete calls inner.Complete with exponential backoff retry on transient errors.
func (r *RetryProvider) Complete(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
	var lastErr error
	for attempt := 0; attempt < r.maxAttempts; attempt++ {
		resp, err := r.inner.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		if !isRetryable(err) {
			return nil, err
		}
		lastErr = err
		wait := r.backoffBase * time.Duration(1<<uint(attempt))
		zap.S().Warnw("llm call failed, retrying",
			"attempt", attempt+1,
			"max", r.maxAttempts,
			"error", err,
		)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, lastErr
}

// CompleteStream calls inner.CompleteStream with exponential backoff retry on transient errors.
func (r *RetryProvider) CompleteStream(ctx context.Context, req ProviderRequest) (<-chan StreamChunk, error) {
	var lastErr error
	for attempt := 0; attempt < r.maxAttempts; attempt++ {
		ch, err := r.inner.CompleteStream(ctx, req)
		if err == nil {
			return ch, nil
		}
		if !isRetryable(err) {
			return nil, err
		}
		lastErr = err
		wait := r.backoffBase * time.Duration(1<<uint(attempt))
		zap.S().Warnw("llm call failed, retrying",
			"attempt", attempt+1,
			"max", r.maxAttempts,
			"error", err,
		)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, lastErr
}

// isRetryable returns true if the error is likely transient and worth retrying.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "429") ||
		strings.Contains(s, "500") ||
		strings.Contains(s, "502") ||
		strings.Contains(s, "503") ||
		strings.Contains(s, "connection") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "EOF")
}

// IsPlaceholderKey returns true if the API key is a test/placeholder value
// that should skip real health checks.
func IsPlaceholderKey(key string) bool {
	return key == "" || key == "test-key" || key == "sk-placeholder" || strings.HasPrefix(key, "test-")
}
