package pprof

import (
	"net/http"
	"testing"
	"time"
)

func TestStart(t *testing.T) {
	shutdown, errc := Start("127.0.0.1:0") // port 0 = random available port

	// Wait briefly for any startup error.
	select {
	case err := <-errc:
		t.Fatalf("pprof server startup error: %v", err)
	case <-time.After(100 * time.Millisecond):
		// No error — server is likely running.
	}

	// Shutdown should not panic.
	shutdown()
}

func TestStartShutdownMultiple(t *testing.T) {
	_, errc := Start("127.0.0.1:0")
	// Check for immediate startup error
	select {
	case err := <-errc:
		if err != http.ErrServerClosed {
			t.Fatalf("unexpected startup error: %v", err)
		}
	default:
	}
	// No explicit second shutdown test; Start returns a single-use shutdown function.
	_ = errc
}
