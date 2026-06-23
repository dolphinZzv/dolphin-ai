package proto

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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
			send(llm.LLMChunk{Error: decodeError(errDecode, resp.StatusCode, body)})
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
			ch <- llm.LLMChunk{Error: decodeError(errDecode, resp.StatusCode, body)}
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

func decodeError(errDecode ErrorDecoder, status int, body []byte) error {
	if errDecode != nil {
		if e := errDecode(status, body); e != nil {
			return e
		}
	}
	return fmt.Errorf("llm: status %d (body: %s)", status, string(body))
}
