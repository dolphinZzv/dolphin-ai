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
	"strings"
	"sync"
	"syscall"
	"time"

	"chick/internal/config"
	graphql "chick/internal/graphql"
	"chick/internal/mcp"
	"chick/internal/models"
	"chick/internal/server"
)

// mcpSession stores per-SSE-session state
type mcpSession struct {
	agentID uint
}

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
		srv.FeedbackService,
		srv.SkillService,
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

	// --- HTTP mode ---

	// Start offline timeout watcher (每 60 秒检查一次，5 分钟超时)
	go srv.MatchingEngine.WatchOfflineTimeout(60*time.Second, 5*time.Minute)

	corsMW := server.CORSMiddleware(cfg.AllowedOrigins)
	authMW := srv.Authenticator.HTTPMiddleware

	// GraphQL handler with auth (except loginAgent/registerAgent) + CORS
	graphqlHandler := graphql.NewHandler(
		srv.ProjectService, srv.AgentService, srv.IssueService,
		srv.CommentService, srv.WorkflowService, srv.FeedbackService, srv.EventBus,
	)
	http.Handle("/graphql", corsMW(authMW(graphqlHandler)))

	// MCP SSE handler with sessions
	mcpSessions := &sync.Map{}
	mcpHandler := corsMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, "/")
		// POST /mcp/session/{id} — session-scoped JSON-RPC
		if r.Method == http.MethodPost && strings.HasPrefix(path, "/mcp/session/") {
			sessionID := strings.TrimPrefix(path, "/mcp/session/")
			handleMCPSessionPost(w, r, mcpServer, mcpSessions, sessionID)
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
	log.Printf("│  BOOTSTRAP_TOKEN=%s     │", cfg.BootstrapToken)
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
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Direct POST to /mcp (backward compat, no session)
		if r.Method == http.MethodPost {
			var req mcp.Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				resp := mcp.NewParseError(nil)
				json.NewEncoder(w).Encode(resp)
				return
			}
			resp := mcpServer.HandleRequest(&req)
			data, _ := json.Marshal(resp)
			w.Header().Set("Content-Type", "application/json")
			w.Write(data)
			return
		}

		// GET /mcp — create SSE session
		sessionID := randomID()
		bootstrapToken := r.URL.Query().Get("bootstrapToken")

		// Auto-register agent if bootstrap token provided
		var registeredProjectID uint
		if bootstrapToken != "" {
			projectID, ok := srv.ProjectService.ValidateBootstrapToken(bootstrapToken)
			if ok {
				registeredProjectID = projectID
				extID := "mcp-bootstrap-" + bootstrapToken[:8]
				agent, err := srv.AgentService.GetByExternalID(extID)
				if err != nil {
					agent, err = srv.AgentService.Register("mcp-"+fmt.Sprint(projectID), models.AgentKindAI, extID, randomID(), nil, "", "")
					if err == nil {
						srv.ProjectService.AddMember(projectID, agent.ID, models.ProjectRoleMember)
					}
				}
				if agent != nil {
					sessions.Store(sessionID, &mcpSession{agentID: agent.ID})
				}
			}
		}

		w.WriteHeader(http.StatusOK)
		if registeredProjectID > 0 {
			fmt.Fprintf(w, "event: project\ndata: {\"projectId\":%d}\n\n", registeredProjectID)
		}
		fmt.Fprintf(w, "event: endpoint\ndata: /mcp/session/%s\n\n", sessionID)
		flusher.Flush()

		// Keep connection open
		<-r.Context().Done()
		sessions.Delete(sessionID)
	}
}

func handleMCPSessionPost(w http.ResponseWriter, r *http.Request, mcpServer *mcp.Server, sessions *sync.Map, sessionID string) {
	var req mcp.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		resp := mcp.NewParseError(nil)
		json.NewEncoder(w).Encode(resp)
		return
	}

	resp := mcpServer.HandleRequest(&req)
	data, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func randomID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
