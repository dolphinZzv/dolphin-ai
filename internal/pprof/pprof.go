// Package pprof provides a simple Go pprof HTTP server for profiling.
package pprof

import (
	"context"
	"net/http"
	_ "net/http/pprof"
	"time"
)

// Start starts a pprof HTTP server on the given address.
// The returned shutdown function gracefully stops the server.
func Start(addr string) func() {
	srv := &http.Server{
		Addr:         addr,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go func() { _ = srv.ListenAndServe() }()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}
