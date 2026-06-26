package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"chick/internal/config"
	"chick/internal/events"
	graphql "chick/internal/graphql"
	"chick/internal/mcp"
	"chick/internal/models"
	"chick/internal/server"
)

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
		srv.ProposalService,
		srv.TaskService,
		srv.WorkflowService,
		srv.FeedbackService,
		srv.NotifService,
		cfg.DefaultRequirementProjectID,
	)
	mcpServer := mcp.NewServer(mcpHandlers)

	// Start offline timeout watcher
	go srv.MatchingEngine.WatchOfflineTimeout(60*time.Second, 5*time.Minute)

	corsMW := server.CORSMiddleware(cfg.AllowedOrigins)
	authMW := srv.Authenticator.HTTPMiddleware

	// GraphQL handler with auth + CORS
	graphqlHandler := graphql.NewHandler(
		srv.ProjectService, srv.AgentService, srv.IssueService,
		srv.CommentService, srv.ProposalService, srv.TaskService, srv.WorkflowService, srv.FeedbackService, srv.NotifService, srv.EventBus,
		cfg.AllowHumanRegistration)
	http.Handle("/graphql", corsMW(authMW(graphqlHandler)))

	// MCP — Streamable HTTP (POST) + SSE events (GET)
	http.Handle("/mcp", corsMW(handleMCP(srv, mcpServer)))
	http.Handle("/mcp/events", corsMW(handleMCPEvents(srv)))

	// Health check (no auth)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// MCP OAuth discovery — not supported
	http.Handle("/.well-known/", corsMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"oauth not supported"}`))
	})))

	// SPA fallback (no auth — login page needs to load)
	http.Handle("/", corsMW(server.SPAHandler()))

	log.Printf("┌──────────────────────────────────────┐")
	log.Printf("│  Chick Agent Platform                 │")
	log.Printf("│  DB: %-32s │", cfg.DBDriver)
	log.Printf("│  MCP:  http://0.0.0.0:%s/mcp       │", cfg.Port)
	log.Printf("│  MCP Events SSE: http://0.0.0.0:%s/mcp/events │", cfg.Port)
	log.Printf("│  Web UI:  http://0.0.0.0:%s         │", cfg.Port)
	if len(cfg.AllowedOrigins) > 0 {
		log.Printf("│  CORS: %-30s │", cfg.AllowedOrigins[0])
	}
	if cfg.DevMode {
		log.Printf("│  MODE: DEV                                            │")
	}
	if cfg.PprofEnabled {
		log.Printf("│  pprof: /debug/pprof/                               │")
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

// authenticateMCP extracts and validates the Bearer token for MCP endpoints.
func authenticateMCP(r *http.Request, srv *server.Server) (*models.Agent, string, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		return nil, "", fmt.Errorf("missing or invalid Authorization header")
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	agent, err := srv.AgentService.Authenticate(token)
	if err != nil {
		return nil, "", fmt.Errorf("invalid token")
	}
	if agent.Disabled {
		return nil, "", fmt.Errorf("access denied: agent is disabled")
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !checkIPInCIDRs(ip, agent.AllowedCIDRs) {
		return nil, "", fmt.Errorf("access denied: IP not allowed")
	}
	return agent, ip, nil
}

// handleMCP handles MCP requests:
//   - GET  /mcp — SSE transport initial connection (endpoint discovery)
//   - POST /mcp — JSON-RPC (Streamable HTTP or SSE transport messages)
func handleMCP(srv *server.Server, mcpServer *mcp.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// MCP SSE transport: return an SSE stream with the endpoint URL.
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "Streaming not supported", http.StatusInternalServerError)
				return
			}

			// Must authenticate even for SSE setup — the token is validated here.
			// The long-lived SSE connection is tied to this agent.
			if _, _, err := authenticateMCP(r, srv); err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}

			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)

			// Send the endpoint event — tells the client where to POST JSON-RPC messages.
			fmt.Fprintf(w, "event: endpoint\ndata: /mcp\n\n")
			flusher.Flush()

			// Keep the connection open. The existing /mcp/events and POST handler
			// carry the actual JSON-RPC traffic.
			<-r.Context().Done()
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		agent, ip, err := authenticateMCP(r, srv)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		if ip != "" {
			srv.AgentService.UpdateIP(agent.ID, ip)
		}

		var req mcp.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			resp := mcp.NewParseError(nil)
			json.NewEncoder(w).Encode(resp)
			return
		}

		resp := mcpServer.HandleRequest(&req, agent.ID, ip)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// handleMCPEvents provides an SSE stream for real-time notifications (GET /mcp/events).
func handleMCPEvents(srv *server.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		agent, ip, err := authenticateMCP(r, srv)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		if ip != "" {
			srv.AgentService.UpdateIP(agent.ID, ip)
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		// Get agent's project memberships for filtering
		projects, _ := srv.ProjectService.ListByAgent(agent.ID)
		projectSet := make(map[uint]bool)
		for _, p := range projects {
			projectSet[p.ID] = true
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		// Channel for events (buffered, drops if full)
		notifChan := make(chan []byte, 64)

		// Subscribe to all relevant event types
		var cancels []func()

		cancels = append(cancels, srv.EventBus.Subscribe(events.EventProposalCreated, func(evt events.Event) {
			p, ok := evt.Payload.(events.ProposalCreatedPayload)
			if !ok || !projectSet[p.ProjectID] {
				return
			}
			data, _ := json.Marshal(map[string]interface{}{
				"type": "proposal.created",
				"payload": map[string]interface{}{
					"proposalId": fmt.Sprintf("%d", p.ProposalID),
					"projectId":  fmt.Sprintf("%d", p.ProjectID),
					"authorId":   fmt.Sprintf("%d", p.AuthorID),
				},
			})
			select {
			case notifChan <- data:
			default:
			}
		}))

		cancels = append(cancels, srv.EventBus.Subscribe(events.EventProposalStateChanged, func(evt events.Event) {
			p, ok := evt.Payload.(events.ProposalStateChangedPayload)
			if !ok || !projectSet[p.ProjectID] {
				return
			}
			data, _ := json.Marshal(map[string]interface{}{
				"type": "proposal.state_changed",
				"payload": map[string]interface{}{
					"proposalId": fmt.Sprintf("%d", p.ProposalID),
					"projectId":  fmt.Sprintf("%d", p.ProjectID),
					"from":       p.From,
					"to":         p.To,
					"actorId":    fmt.Sprintf("%d", p.ActorID),
				},
			})
			select {
			case notifChan <- data:
			default:
			}
		}))

		cancels = append(cancels, srv.EventBus.Subscribe(events.EventTaskCreated, func(evt events.Event) {
			p, ok := evt.Payload.(events.TaskCreatedPayload)
			if !ok || !projectSet[p.ProjectID] {
				return
			}
			data, _ := json.Marshal(map[string]interface{}{
				"type": "task.created",
				"payload": map[string]interface{}{
					"taskId":     fmt.Sprintf("%d", p.TaskID),
					"proposalId": fmt.Sprintf("%d", p.ProposalID),
					"projectId":  fmt.Sprintf("%d", p.ProjectID),
				},
			})
			select {
			case notifChan <- data:
			default:
			}
		}))

		cancels = append(cancels, srv.EventBus.Subscribe(events.EventTaskStateChanged, func(evt events.Event) {
			p, ok := evt.Payload.(events.TaskStateChangedPayload)
			if !ok || !projectSet[p.ProjectID] {
				return
			}
			data, _ := json.Marshal(map[string]interface{}{
				"type": "task.state_changed",
				"payload": map[string]interface{}{
					"taskId":     fmt.Sprintf("%d", p.TaskID),
					"proposalId": fmt.Sprintf("%d", p.ProposalID),
					"projectId":  fmt.Sprintf("%d", p.ProjectID),
					"from":       p.From,
					"to":         p.To,
					"actorId":    fmt.Sprintf("%d", p.ActorID),
				},
			})
			select {
			case notifChan <- data:
			default:
			}
		}))

		cancels = append(cancels, srv.EventBus.Subscribe(events.EventIssueCreated, func(evt events.Event) {
			p, ok := evt.Payload.(events.IssueCreatedPayload)
			if !ok || !projectSet[p.ProjectID] {
				return
			}
			data, _ := json.Marshal(map[string]interface{}{
				"type": "issue.created",
				"payload": map[string]interface{}{
					"issueId":   fmt.Sprintf("%d", p.IssueID),
					"projectId": fmt.Sprintf("%d", p.ProjectID),
					"creatorId": fmt.Sprintf("%d", p.CreatorID),
				},
			})
			select {
			case notifChan <- data:
			default:
			}
		}))

		cancels = append(cancels, srv.EventBus.Subscribe(events.EventIssueStateChanged, func(evt events.Event) {
			p, ok := evt.Payload.(events.IssueStateChangedPayload)
			if !ok || !projectSet[p.ProjectID] {
				return
			}
			data, _ := json.Marshal(map[string]interface{}{
				"type": "issue.state_changed",
				"payload": map[string]interface{}{
					"issueId":   fmt.Sprintf("%d", p.IssueID),
					"projectId": fmt.Sprintf("%d", p.ProjectID),
					"from":      p.From,
					"to":        p.To,
					"actorId":   fmt.Sprintf("%d", p.ActorID),
				},
			})
			select {
			case notifChan <- data:
			default:
			}
		}))

		cancels = append(cancels, srv.EventBus.Subscribe(events.EventIssueAssigneeChanged, func(evt events.Event) {
			p, ok := evt.Payload.(events.IssueAssigneeChangedPayload)
			if !ok || !projectSet[p.ProjectID] {
				return
			}
			data, _ := json.Marshal(map[string]interface{}{
				"type": "issue.assignee_changed",
				"payload": map[string]interface{}{
					"issueId":   fmt.Sprintf("%d", p.IssueID),
					"projectId": fmt.Sprintf("%d", p.ProjectID),
					"agentId":   fmt.Sprintf("%d", p.AgentID),
					"action":    p.Action,
				},
			})
			select {
			case notifChan <- data:
			default:
			}
		}))

		cancels = append(cancels, srv.EventBus.Subscribe(events.EventCommentAdded, func(evt events.Event) {
			p, ok := evt.Payload.(events.CommentAddedPayload)
			if !ok || !projectSet[p.ProjectID] {
				return
			}
			data, _ := json.Marshal(map[string]interface{}{
				"type": "comment.added",
				"payload": map[string]interface{}{
					"commentId": fmt.Sprintf("%d", p.CommentID),
					"issueId":   fmt.Sprintf("%d", p.IssueID),
					"projectId": fmt.Sprintf("%d", p.ProjectID),
					"authorId":  fmt.Sprintf("%d", p.AuthorID),
				},
			})
			select {
			case notifChan <- data:
			default:
			}
		}))

		// Cleanup subscriptions on disconnect
		defer func() {
			for _, c := range cancels {
				c()
			}
		}()

		// Event loop: forward events as SSE or wait for disconnect
		for {
			select {
			case data := <-notifChan:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
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
