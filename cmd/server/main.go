package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"chick/internal/config"
	"chick/internal/mcp"
	"chick/internal/server"
)

func main() {
	stdioMode := flag.Bool("stdio", false, "Run MCP in STDIO mode")
	flag.Parse()

	cfg := config.Load()

	// Generate bootstrap token if not set
	if cfg.BootstrapToken == "" {
		bytes := make([]byte, 16)
		if _, err := rand.Read(bytes); err != nil {
			log.Fatalf("generate bootstrap token: %v", err)
		}
		cfg.BootstrapToken = hex.EncodeToString(bytes)
	}

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	// Init MCP Server
	mcpHandlers := mcp.NewHandlers(
		srv.ProjectService,
		srv.AgentService,
		srv.IssueService,
		srv.CommentService,
		srv.WorkflowService,
		srv.NotifService,
		srv.Authenticator,
	)
	mcpServer := mcp.NewServer(mcpHandlers)

	if *stdioMode {
		// STDIO mode — suitable for Claude Code local integration
		log.Println("[mcp] running in STDIO mode")
		transport := mcp.NewSTDIOTransport()
		transport.Run(mcpServer.HandleRequest)
		return
	}

	// SSE mode — HTTP server
	http.HandleFunc("/mcp", handleSSE(mcpServer))
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("┌──────────────────────────────────────┐")
	log.Printf("│  Chick Agent Platform                 │")
	log.Printf("│  DB: %s                          │", cfg.DBDriver)
	log.Printf("│  MCP SSE: http://0.0.0.0:%s/mcp     │", cfg.Port)
	log.Printf("│  BOOTSTRAP_TOKEN=%s     │", cfg.BootstrapToken)
	log.Printf("└──────────────────────────────────────┘")

	// Start HTTP server
	httpServer := &http.Server{Addr: ":" + cfg.Port}

	go func() {
		log.Printf("[server] listening on :%s", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[server] shutting down...")
	httpServer.Close()
	sqlDB, err := srv.DB.DB()
	if err == nil {
		sqlDB.Close()
	}
}

func handleSSE(mcpServer *mcp.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Handle POST with JSON-RPC
		if r.Method == http.MethodPost {
			var req mcp.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				resp := mcp.NewParseError(nil)
				json.NewEncoder(w).Encode(resp)
				return
			}
			resp := mcpServer.HandleRequest(&req)
			data, _ := json.Marshal(resp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return
		}

		// GET — SSE stream for notifications
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: %s\n\n", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		flusher.Flush()

		<-r.Context().Done()
	}
}
