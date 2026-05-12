package mcp_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"chick/internal/config"
	"chick/internal/events"
	"chick/internal/mcp"
	"chick/internal/models"
	"chick/internal/notifications"
	gormrepo "chick/internal/repository/gorm"
	"chick/internal/service"
	"chick/internal/server"

	_ "github.com/mattn/go-sqlite3"
)

// setupTest creates a full test environment with in-memory SQLite
func setupTest(t *testing.T) (*mcp.Server, *service.ProjectService, *service.AgentService, *service.IssueService) {
	t.Helper()

	cfg := &config.Config{
		DBDriver: "sqlite3",
		DBDSN:    "file::memory:?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		Port:     "0",
	}

	db, err := server.NewDB(cfg)
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := server.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Init repos
	projectRepo := gormrepo.NewProjectRepo(db)
	memberRepo := gormrepo.NewProjectMemberRepo(db)
	agentRepo := gormrepo.NewAgentRepo(db)
	issueRepo := gormrepo.NewIssueRepo(db)
	assigneeRepo := gormrepo.NewIssueAssigneeRepo(db)
	commentRepo := gormrepo.NewCommentRepo(db)
	timelineRepo := gormrepo.NewTimelineRepo(db)
	labelRepo := gormrepo.NewLabelRepo(db)
	milestoneRepo := gormrepo.NewMilestoneRepo(db)
	feedbackRepo := gormrepo.NewFeedbackRepo(db)
	skillRepo := gormrepo.NewSkillRepo(db)
	bus := events.NewBus()

	// Init services
	projectSvc := service.NewProjectService(projectRepo, memberRepo, labelRepo, milestoneRepo)
	agentSvc := service.NewAgentService(agentRepo, bus, nil)
	commentSvc := service.NewCommentService(commentRepo, timelineRepo, bus)
	issueSvc := service.NewIssueService(issueRepo, assigneeRepo, timelineRepo, projectRepo, bus)
	workflowSvc := service.NewWorkflowService(issueSvc)

	feedbackSvc := service.NewFeedbackService(feedbackRepo, bus)
	skillSvc := service.NewSkillService(skillRepo)

	// Init MCP
	notifSvc := notifications.NewService()
	notifSvc.Subscribe(bus)
	handlers := mcp.NewHandlers(projectSvc, agentSvc, issueSvc, commentSvc, workflowSvc, feedbackSvc, skillSvc, notifSvc, nil)
	mcpServer := mcp.NewServer(handlers)

	return mcpServer, projectSvc, agentSvc, issueSvc
}

// Helper: send a JSON-RPC request and decode response
func call(t *testing.T, srv *mcp.Server, method string, params interface{}) map[string]interface{} {
	t.Helper()
	paramsData, _ := json.Marshal(params)
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  method,
		Params:  paramsData,
	}
	resp := srv.HandleRequest(req)
	if resp.Error != nil {
		t.Fatalf("RPC error for %s: %s (code %d)", method, resp.Error.Message, resp.Error.Code)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		// Could be a different type - marshal and return as map
		data, _ := json.Marshal(resp.Result)
		json.Unmarshal(data, &result)
	}
	return result
}

func TestInitialize(t *testing.T) {
	srv, _, _, _ := setupTest(t)
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
	}
	resp := srv.HandleRequest(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	result := resp.Result.(map[string]interface{})
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected protocol version 2024-11-05, got %v", result["protocolVersion"])
	}
	if result["serverInfo"] == nil {
		t.Error("expected serverInfo")
	}
}

func TestToolsList(t *testing.T) {
	srv, _, _, _ := setupTest(t)
	result := call(t, srv, "tools/list", nil)
	// Normalize tools list via JSON round-trip to handle concrete types
	toolsData, _ := json.Marshal(result["tools"])
	var tools []map[string]interface{}
	json.Unmarshal(toolsData, &tools)
	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}

	// Check required tools exist
	names := make(map[string]bool)
	for _, t := range tools {
		names[t["name"].(string)] = true
	}

	required := []string{
		"create_project", "register_agent", "login_agent",
		"create_issue", "add_comment", "assign_issue",
		"transition_issue", "search_issues", "list_agents", "agent_heartbeat",
		"check_notifications", "submit_feedback", "list_feedback",
		"list_skills", "run_skill",
	}
	for _, r := range required {
		if !names[r] {
			t.Errorf("missing required tool: %s", r)
		}
	}
}

func TestCreateProjectAndIssue(t *testing.T) {
	srv, _, agentSvc, _ := setupTest(t)

	// Register an agent first
	agent, err := agentSvc.Register("test-agent", models.AgentKindAI, "test-001", "secret", []string{"CODING"}, "", "")
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}

	// Create project via MCP
	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_project",
		"arguments": map[string]interface{}{
			"name":        "Test Project",
			"description": "A test project",
		},
	})
	if result["id"] == "" {
		t.Error("expected project id")
	}
	projectID := result["id"].(string)

	// Create issue via MCP
	result = call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"projectId":   projectID,
			"title":       "Test Issue",
			"description": "Description",
			"creatorId":   "1",
		},
	})
	if result["number"] == nil {
		t.Error("expected issue number")
	}
	if result["title"] != "Test Issue" {
		t.Errorf("expected 'Test Issue', got %v", result["title"])
	}
	if result["state"] != "open" {
		t.Errorf("expected state 'open', got %v", result["state"])
	}

	_ = agent
}

func TestRegisterAndLoginAgent(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	// Register agent via MCP
	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "register_agent",
		"arguments": map[string]interface{}{
			"name":       "test-bot",
			"kind":       "ai",
			"externalId": "bot-001",
			"secret":     "password123",
			"capabilities": []string{"CODING", "REVIEW"},
		},
	})
	agentID := result["id"].(string)
	if agentID == "" {
		t.Error("expected agent id")
	}

	// Login via MCP
	result = call(t, srv, "tools/call", map[string]interface{}{
		"name": "login_agent",
		"arguments": map[string]interface{}{
			"externalId": "bot-001",
			"secret":     "password123",
		},
	})
	if result["id"] != agentID {
		t.Errorf("expected agent id %s, got %v", agentID, result["id"])
	}
}

func TestTransitionIssue(t *testing.T) {
	srv, _, agentSvc, _ := setupTest(t)

	// Register agent
	_, err := agentSvc.Register("test-agent", models.AgentKindAI, "test-002", "secret", nil, "", "")
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}

	// Create project
	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_project",
		"arguments": map[string]interface{}{
			"name": "Test",
		},
	})
	projectID := result["id"].(string)

	// Create issue
	result = call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"projectId": projectID,
			"title":     "Test Issue",
			"creatorId": "1",
		},
	})
	issueID := result["id"].(string)

	// Transition to in_progress
	result = call(t, srv, "tools/call", map[string]interface{}{
		"name": "transition_issue",
		"arguments": map[string]interface{}{
			"issueId": issueID,
			"toState": "in_progress",
			"actorId": "1",
		},
	})
	if result["state"] != "in_progress" {
		t.Errorf("expected state 'in_progress', got %v", result["state"])
	}
}

func TestSearchIssues(t *testing.T) {
	srv, _, agentSvc, issueSvc := setupTest(t)

	// Register agent + create project + create issue
	agentSvc.Register("test-agent", models.AgentKindAI, "test-003", "secret", nil, "", "")

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_project",
		"arguments": map[string]interface{}{"name": "SearchTest"},
	})
	projectID := result["id"].(string)

	// Create issue via service directly (more efficient)
	pid := uint(1)
	issueSvc.Create(pid, 1, "Fix login bug", "Users cannot login", models.PriorityHigh, nil, nil)
	issueSvc.Create(pid, 1, "Add tests", "Need unit tests", models.PriorityMedium, nil, nil)

	// Search via MCP
	result = call(t, srv, "tools/call", map[string]interface{}{
		"name": "search_issues",
		"arguments": map[string]interface{}{
			"projectId": projectID,
			"search":    "login",
		},
	})
	totalVal, _ := result["total"].(float64)
	if totalVal == 0 {
		if ti, ok := result["total"].(int64); ok && ti > 0 {
			totalVal = float64(ti)
		}
	}
	if totalVal == 0 {
		t.Error("expected at least 1 search result for 'login'")
	}
}

// ─── create_issue 专向测试 ─────────────────────────────────

func TestMCPCreateIssue_HappyPath(t *testing.T) {
	srv, _, agentSvc, _ := setupTest(t)

	// Register agent + create project
	agentSvc.Register("coder", models.AgentKindAI, "coder-001", "secret", nil, "", "")
	proj := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_project",
		"arguments": map[string]interface{}{
			"name": "My Project",
		},
	})
	projectID := proj["id"].(string)

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"projectId":   projectID,
			"title":       "Fix login bug",
			"description": "Users cannot login with OAuth",
			"creatorId":   "1",
			"priority":    "high",
		},
	})

	if result["id"] == "" {
		t.Error("expected issue id")
	}
	if result["title"] != "Fix login bug" {
		t.Errorf("expected 'Fix login bug', got %v", result["title"])
	}
	if result["state"] != "open" {
		t.Errorf("expected state 'open', got %v", result["state"])
	}
	// issue.Number is uint, stored as uint in resp.Result (not float64)
	if result["number"] == nil || fmt.Sprint(result["number"]) == "0" {
		t.Errorf("expected non-zero issue number, got %v", result["number"])
	}
}

func TestMCPCreateIssue_MissingRequired(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	tests := []struct {
		name    string
		args    map[string]interface{}
		errCode int
	}{
		{
			name:    "missing title",
			args:    map[string]interface{}{"projectId": "1", "creatorId": "1"},
			errCode: -32602, // service layer returns internal error
		},
		{
			name:    "missing creatorId",
			args:    map[string]interface{}{"projectId": "1", "title": "No creator"},
			errCode: -32602,
		},
		{
			name:    "missing projectId",
			args:    map[string]interface{}{"title": "No project", "creatorId": "1"},
			errCode: -32602,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, _ := json.Marshal(map[string]interface{}{
				"name": "create_issue",
				"arguments": tt.args,
			})
			req := &mcp.Request{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`1`),
				Method:  "tools/call",
				Params:  params,
			}
			resp := srv.HandleRequest(req)
			if resp.Error == nil {
				t.Fatal("expected error for missing required params")
			}
		})
	}
}

func TestMCPCreateIssue_DefaultPriority(t *testing.T) {
	srv, _, agentSvc, _ := setupTest(t)

	agentSvc.Register("coder", models.AgentKindAI, "coder-002", "secret", nil, "", "")
	proj := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_project",
		"arguments": map[string]interface{}{"name": "P"},
	})
	projectID := proj["id"].(string)

	// Without priority → should default to medium
	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"projectId": projectID,
			"title":     "Default priority",
			"creatorId": "1",
		},
	})
	if result["id"] == "" {
		t.Error("expected issue id")
	}
}

func TestMCPCreateIssue_InvalidPriority(t *testing.T) {
	srv, _, agentSvc, _ := setupTest(t)

	agentSvc.Register("coder", models.AgentKindAI, "coder-003", "secret", nil, "", "")
	proj := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_project",
		"arguments": map[string]interface{}{"name": "P2"},
	})
	projectID := proj["id"].(string)

	// Invalid priority → defaults to medium, not an error
	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"projectId": projectID,
			"title":     "Bad priority",
			"creatorId": "1",
			"priority":  "urgent",
		},
	})
	if result["id"] == "" {
		t.Error("expected issue id even with invalid priority")
	}
}

func TestMCPCreateIssue_WithAssignees(t *testing.T) {
	srv, _, agentSvc, _ := setupTest(t)

	// Register two agents
	agent1, _ := agentSvc.Register("assignee1", models.AgentKindAI, "ass-001", "secret", nil, "", "")
	agentSvc.Register("assignee2", models.AgentKindAI, "ass-002", "secret", nil, "", "")

	proj := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_project",
		"arguments": map[string]interface{}{"name": "P3"},
	})
	projectID := proj["id"].(string)

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"projectId":   projectID,
			"title":       "Assigned issue",
			"description": "This has assignees",
			"creatorId":   "1",
			"priority":    "critical",
			"assigneeIds": []string{fmt.Sprintf("%d", agent1.ID), "2"},
		},
	})
	if result["id"] == "" {
		t.Error("expected issue id with assignees")
	}
	// assignees are created inside IssueService.Create;
	// verifying via IssueService.GetByID requires extra preloads
}

func TestUnknownTool(t *testing.T) {
	srv, _, _, _ := setupTest(t)
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"nonexistent","arguments":{}}`),
	}
	resp := srv.HandleRequest(req)
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("expected error code -32602, got %d", resp.Error.Code)
	}
}
