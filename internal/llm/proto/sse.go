// Package proto provides stateless transport primitives shared by all LLM
// providers: SSE framing and HTTP stream/non-stream pumps.
//
// proto knows nothing about models, vendors, or protocol semantics. It does
// not parse JSON, decide auth headers, or assume any "standard" behavior.
// Each provider constructs its own *http.Request (URL, auth, body) and injects
// a decoder that owns the response semantics.
package proto

import (
	"bufio"
	"io"
	"strings"
)

// SSEReader splits an SSE byte stream into "data:" payloads.
// It is purely a transport primitive: it returns the raw data of each event
// line and signals [DONE]. Interpreting the JSON inside is the caller's job.
type SSEReader struct {
	scanner *bufio.Scanner
}

// NewSSEReader wraps r with a buffered scanner sized for large model chunks.
func NewSSEReader(r io.Reader) *SSEReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &SSEReader{scanner: scanner}
}

// Next returns the next SSE data payload.
//   - data: the raw bytes after the "data: " prefix (may be empty).
//   - done: true when a "data: [DONE]" sentinel is encountered.
//   - err:  non-nil on read failure; io.EOF when the stream ends cleanly.
//
// Non-data lines (comments, event/id fields) are skipped silently.
func (s *SSEReader) Next() (data []byte, done bool, err error) {
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		// [DONE] sentinel — some providers append JSON usage data after it
		// (e.g. "data: [DONE]{"usage":{...}}"). Extract the trailing JSON
		// when present so decoders can harvest token counts from it.
		if rest, ok := strings.CutPrefix(payload, "[DONE]"); ok {
			rest = strings.TrimSpace(rest)
			if rest != "" {
				return []byte(rest), true, nil
			}
			return nil, true, nil
		}
		return []byte(payload), false, nil
	}
	if e := s.scanner.Err(); e != nil {
		return nil, false, e
	}
	return nil, false, io.EOF
}
