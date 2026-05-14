package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"chick/internal/config"
	graphql "chick/internal/graphql"
	"chick/internal/mcp"
	"chick/internal/server"
)

// mcpSession stores per-SSE-session state
type mcpSession struct {
	agentID      uint
	allowedCIDRs []string
}

func main() {
	flag.Parse()

	cfg := config.Load()

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
		srv.FeedbackService,
		srv.NotifService,
		cfg.DefaultRequirementProjectID,
	)
	mcpServer := mcp.NewServer(mcpHandlers)

	// Start offline timeout watcher (每 60 秒检查一次，5 分钟超时)
	go srv.MatchingEngine.WatchOfflineTimeout(60*time.Second, 5*time.Minute)

	corsMW := server.CORSMiddleware(cfg.AllowedOrigins)
	authMW := srv.Authenticator.HTTPMiddleware

	// GraphQL handler with auth (except loginAgent/registerAgent) + CORS
	graphqlHandler := graphql.NewHandler(
		srv.ProjectService, srv.AgentService, srv.IssueService,
		srv.CommentService, srv.WorkflowService, srv.FeedbackService, srv.EventBus,
		cfg.AllowHumanRegistration)
	http.Handle("/graphql", corsMW(authMW(graphqlHandler)))

	// MCP SSE handler with sessions
	mcpSessions := &sync.Map{}
	mcpHandler := corsMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, "/")
		// POST /mcp/session/{id} — session-scoped JSON-RPC
		if r.Method == http.MethodPost && strings.HasPrefix(path, "/mcp/session/") {
			sessionID := strings.TrimPrefix(path, "/mcp/session/")
			handleMCPSessionPost(w, r, srv, mcpServer, mcpSessions, sessionID)
			return
		}
		handleMCP(mcpServer, srv, mcpSessions)(w, r)
	}))
	http.Handle("/mcp/", mcpHandler)
	http.Handle("/mcp", mcpHandler)

	// Health check (no auth)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// MCP OAuth discovery — not supported, return empty so inspector falls back to Bearer Token
	http.Handle("/.well-known/", corsMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"oauth not supported"}`))
	})))

	// SPA fallback (no auth — login page needs to load)
	http.Handle("/", corsMW(server.SPAHandler()))

	log.Printf("┌──────────────────────────────────────┐")
	log.Printf("│  Chick Agent Platform                 │")
	log.Printf("│  DB: %-32s │", cfg.DBDriver)
	log.Printf("│  MCP SSE: http://0.0.0.0:%s/mcp     │", cfg.Port)
	log.Printf("│  Web UI:  http://0.0.0.0:%s         │", cfg.Port)
	if len(cfg.AllowedOrigins) > 0 {
		log.Printf("│  CORS: %-30s │", cfg.AllowedOrigins[0])
	}
	if cfg.DevMode {
		log.Printf("│  MODE: DEV                                            │")
	}
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

func handleMCP(mcpServer *mcp.Server, srv *server.Server, sessions *sync.Map) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Authenticate via Bearer Token
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		agent, err := srv.AgentService.Authenticate(token)
		if err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Disabled check
		if agent.Disabled {
			http.Error(w, "Access denied: agent is disabled", http.StatusForbidden)
			return
		}

		// CIDR restriction check
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if !checkIPInCIDRs(ip, agent.AllowedCIDRs) {
			http.Error(w, "Access denied: IP not allowed", http.StatusForbidden)
			return
		}

		// Direct POST to /mcp (Streamable HTTP)
		if r.Method == http.MethodPost {
			var req mcp.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				resp := mcp.NewParseError(nil)
				json.NewEncoder(w).Encode(resp)
				return
			}
			resp := mcpServer.HandleRequest(&req, agent.ID, ip)
			data, _ := json.Marshal(resp)
			w.Header().Set("Content-Type", "application/json")
			w.Write(data)
			return
		}

		// GET /mcp — create SSE session
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		// Update IP on SSE connect
		if ip != "" {
			srv.AgentService.UpdateIP(agent.ID, ip)
		}

		sessionID := randomID()
		sessions.Store(sessionID, &mcpSession{agentID: agent.ID, allowedCIDRs: agent.AllowedCIDRs})

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: endpoint\ndata: /mcp/session/%s\n\n", sessionID)
		flusher.Flush()

		// Keep connection open
		<-r.Context().Done()
		sessions.Delete(sessionID)
	}
}

func handleMCPSessionPost(w http.ResponseWriter, r *http.Request, srv *server.Server, mcpServer *mcp.Server, sessions *sync.Map, sessionID string) {
	var req mcp.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		resp := mcp.NewParseError(nil)
		json.NewEncoder(w).Encode(resp)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	var agentID uint
	var cidrs []string
	if s, ok := sessions.Load(sessionID); ok {
		if session, ok := s.(*mcpSession); ok {
			agentID = session.agentID
			cidrs = session.allowedCIDRs
		}
	}
	if !checkIPInCIDRs(ip, cidrs) {
		http.Error(w, "Access denied: IP not allowed", http.StatusForbidden)
		return
	}
	// Check if agent is disabled
	if agentID != 0 {
		a, err := srv.AgentService.GetByID(agentID)
		if err == nil && a.Disabled {
			http.Error(w, "Access denied: agent is disabled", http.StatusForbidden)
			return
		}
	}
	resp := mcpServer.HandleRequest(&req, agentID, ip)
	data, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func randomID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// checkIPInCIDRs checks if an IP is within any of the allowed CIDR ranges.
// If allowedCIDRs is empty, no restriction is applied (allow all).
func checkIPInCIDRs(ip string, allowedCIDRs []string) bool {
	if len(allowedCIDRs) == 0 {
		return true
	}
	clientIP := net.ParseIP(ip)
	if clientIP == nil {
		return false
	}
	for _, cidr := range allowedCIDRs {
		_, subnet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if subnet.Contains(clientIP) {
			return true
		}
	}
	return false
}
