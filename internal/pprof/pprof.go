// Package pprof provides a simple Go pprof HTTP server for profiling.
package pprof

import (
	"context"
	"net/http"
	"net/http/pprof"
	"time"
)

// Start starts a pprof HTTP server on the given address.
// The returned shutdown function gracefully stops the server.
// An error channel is returned for observing startup failures (e.g. port in use).
func Start(addr string) (shutdown func(), errc <-chan error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	ch := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ch <- err
		}
		close(ch)
	}()

	shutdown = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return shutdown, ch
}
