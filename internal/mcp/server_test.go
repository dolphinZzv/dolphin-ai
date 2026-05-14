package mcp_test

import (
	"encoding/json"
	"fmt"
	"strings"
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
	bus := events.NewBus()

	// Init services
	projectSvc := service.NewProjectService(projectRepo, memberRepo, labelRepo, milestoneRepo)
	agentSvc := service.NewAgentService(agentRepo, bus, nil, true)
	commentSvc := service.NewCommentService(db, commentRepo, timelineRepo, issueRepo, bus)
	issueSvc := service.NewIssueService(db, issueRepo, assigneeRepo, timelineRepo, projectRepo, bus)
	workflowSvc := service.NewWorkflowService(issueSvc)

	feedbackSvc := service.NewFeedbackService(feedbackRepo, bus)

	// Init MCP
	notifSvc := notifications.NewService()
	notifSvc.Subscribe(bus)
	handlers := mcp.NewHandlers(projectSvc, agentSvc, issueSvc, commentSvc, workflowSvc, feedbackSvc, notifSvc)
	mcpServer := mcp.NewServer(handlers)

	return mcpServer, projectSvc, agentSvc, issueSvc
}

// Helper: send a JSON-RPC request and decode response
func call(t *testing.T, srv *mcp.Server, method string, params interface{}, agentID uint) map[string]interface{} {
	t.Helper()
	paramsData, _ := json.Marshal(params)
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  method,
		Params:  paramsData,
	}
	resp := srv.HandleRequest(req, agentID, "")
	if resp.Error != nil {
		t.Fatalf("RPC error for %s: %s (code %d)", method, resp.Error.Message, resp.Error.Code)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		data, _ := json.Marshal(resp.Result)
		json.Unmarshal(data, &result)
	}
	// Unwrap MCP content blocks for tools/call responses
	if method == "tools/call" {
		if content, has := result["content"]; has {
			if items, ok := content.([]interface{}); ok && len(items) > 0 {
				if item, ok := items[0].(map[string]interface{}); ok {
					if text, ok := item["text"].(string); ok {
						var inner map[string]interface{}
						if err := json.Unmarshal([]byte(text), &inner); err == nil {
							return inner
						}
					}
				}
			}
		}
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
	resp := srv.HandleRequest(req, 0, "")
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
	result := call(t, srv, "tools/list", nil, 0)
	toolsData, _ := json.Marshal(result["tools"])
	var tools []map[string]interface{}
	json.Unmarshal(toolsData, &tools)
	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}

	names := make(map[string]bool)
	for _, t := range tools {
		names[t["name"].(string)] = true
	}

	required := []string{
		"create_issue", "add_comment", "assign_issue",
		"transition_issue", "search_issues", "list_agents", "get_agent_info",
		"agent_heartbeat", "check_notifications", "submit_feedback", "list_feedback",
	}
	for _, r := range required {
		if !names[r] {
			t.Errorf("missing required tool: %s", r)
		}
	}
	// Verify removed tools are gone
	removed := []string{"create_project", "register_agent", "login_agent"}
	for _, r := range removed {
		if names[r] {
			t.Errorf("removed tool should not exist: %s", r)
		}
	}
}

func TestCreateIssue(t *testing.T) {
	srv, projectSvc, agentSvc, _ := setupTest(t)

	agent, err := agentSvc.Register("test-agent", models.AgentKindAI, "test-001", "secret", []string{"CODING"}, "", "")
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}

	proj, _ := projectSvc.Create("Test Project", "A test project")
	projectSvc.AddMember(proj.ID, agent.ID, models.ProjectRoleMember)

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"title":       "Test Issue",
			"description": "Description",
		},
	}, agent.ID)
	if result["number"] == nil {
		t.Error("expected issue number")
	}
	if result["title"] != "Test Issue" {
		t.Errorf("expected 'Test Issue', got %v", result["title"])
	}
	if result["state"] != "open" {
		t.Errorf("expected state 'open', got %v", result["state"])
	}
}

func TestTransitionIssue(t *testing.T) {
	srv, projectSvc, agentSvc, _ := setupTest(t)

	agent, err := agentSvc.Register("test-agent", models.AgentKindAI, "test-002", "secret", nil, "", "")
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}

	proj, _ := projectSvc.Create("Test", "")
	projectSvc.AddMember(proj.ID, agent.ID, models.ProjectRoleMember)

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"title": "Test Issue",
		},
	}, agent.ID)
	issueID := result["id"].(string)

	result = call(t, srv, "tools/call", map[string]interface{}{
		"name": "transition_issue",
		"arguments": map[string]interface{}{
			"issueId": issueID,
			"toState": "in_progress",
		},
	}, agent.ID)
	if result["state"] != "in_progress" {
		t.Errorf("expected state 'in_progress', got %v", result["state"])
	}
}

func TestSearchIssues(t *testing.T) {
	srv, projectSvc, agentSvc, issueSvc := setupTest(t)

	agent, _ := agentSvc.Register("test-agent", models.AgentKindAI, "test-003", "secret", nil, "", "")

	proj, _ := projectSvc.Create("SearchTest", "")
	projectSvc.AddMember(proj.ID, agent.ID, models.ProjectRoleMember)

	issueSvc.Create(proj.ID, agent.ID, "Fix login bug", "Users cannot login", models.PriorityHigh, nil, nil, nil)
	issueSvc.Create(proj.ID, agent.ID, "Add tests", "Need unit tests", models.PriorityMedium, nil, nil, nil)

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "search_issues",
		"arguments": map[string]interface{}{
			"search": "login",
		},
	}, agent.ID)
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

func setupTestWithProject(t *testing.T) (*mcp.Server, *service.ProjectService, *service.AgentService, *models.Agent, *models.Project) {
	srv, projectSvc, agentSvc, _ := setupTest(t)
	agent, _ := agentSvc.Register("coder", models.AgentKindAI, "coder-001", "secret", nil, "", "")
	proj, _ := projectSvc.Create("My Project", "")
	projectSvc.AddMember(proj.ID, agent.ID, models.ProjectRoleMember)
	return srv, projectSvc, agentSvc, agent, proj
}

func TestMCPCreateIssue_HappyPath(t *testing.T) {
	srv, _, _, agent, _ := setupTestWithProject(t)

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"title":       "Fix login bug",
			"description": "Users cannot login with OAuth",
			"priority":    "high",
		},
	}, agent.ID)

	if result["id"] == "" {
		t.Error("expected issue id")
	}
	if result["title"] != "Fix login bug" {
		t.Errorf("expected 'Fix login bug', got %v", result["title"])
	}
	if result["state"] != "open" {
		t.Errorf("expected state 'open', got %v", result["state"])
	}
	if result["number"] == nil || fmt.Sprint(result["number"]) == "0" {
		t.Errorf("expected non-zero issue number, got %v", result["number"])
	}
}

func TestMCPCreateIssue_MissingRequired(t *testing.T) {
	srv, projectSvc, agentSvc, _, _ := setupTestWithProject(t)
	_ = projectSvc
	_ = agentSvc

	tests := []struct {
		name    string
		args    map[string]interface{}
		errCode int
	}{
		{
			name:    "missing title",
			args:    map[string]interface{}{},
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
			resp := srv.HandleRequest(req, 0, "")
			if resp.Error == nil {
				t.Fatal("expected error for missing required params")
			}
		})
	}
}

func TestMCPCreateIssue_DefaultPriority(t *testing.T) {
	srv, _, _, agent, _ := setupTestWithProject(t)

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"title": "Default priority",
		},
	}, agent.ID)
	if result["id"] == "" {
		t.Error("expected issue id")
	}
}

func TestMCPCreateIssue_InvalidPriority(t *testing.T) {
	srv, _, _, _, _ := setupTestWithProject(t)

	params, _ := json.Marshal(map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"title":    "Bad priority",
			"priority": "urgent",
		},
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  params,
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error == nil {
		t.Fatal("expected error for invalid priority")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("expected error code -32602, got %d", resp.Error.Code)
	}
}

func TestMCPCreateIssue_WithAssignees(t *testing.T) {
	srv, _, agentSvc, agent, _ := setupTestWithProject(t)

	agent1, _ := agentSvc.Register("assignee1", models.AgentKindAI, "ass-001", "secret", nil, "", "")
	agent2, _ := agentSvc.Register("assignee2", models.AgentKindAI, "ass-002", "secret", nil, "", "")

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"title":       "Assigned issue",
			"description": "This has assignees",
			"priority":    "critical",
			"assigneeIds": []string{fmt.Sprintf("%d", agent1.ID), fmt.Sprintf("%d", agent2.ID)},
		},
	}, agent.ID)
	if result["id"] == "" {
		t.Error("expected issue id with assignees")
	}
}

func TestUnknownTool(t *testing.T) {
	srv, _, _, _ := setupTest(t)
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"nonexistent","arguments":{}}`),
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("expected error code -32602, got %d", resp.Error.Code)
	}
}

func TestMCPCreateIssuesBatch_HappyPath(t *testing.T) {
	srv, _, _, agent, _ := setupTestWithProject(t)

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issues_batch",
		"arguments": map[string]interface{}{
			"issues": []map[string]interface{}{
				{"title": "Batch issue 1", "description": "First batch", "priority": "high"},
				{"title": "Batch issue 2", "description": "Second batch", "priority": "low"},
			},
		},
	}, agent.ID)

	items, ok := result["items"].([]interface{})
	if !ok {
		data, _ := json.Marshal(result["items"])
		json.Unmarshal(data, &items)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	for i, item := range items {
		m, _ := item.(map[string]interface{})
		if m["id"] == "" || fmt.Sprint(m["number"]) == "0" {
			t.Errorf("items[%d]: expected valid id/number, got %v", i, m)
		}
		if m["title"] != fmt.Sprintf("Batch issue %d", i+1) {
			t.Errorf("items[%d]: expected title 'Batch issue %d', got %v", i, i+1, m["title"])
		}
		if m["state"] != "open" {
			t.Errorf("items[%d]: expected state 'open', got %v", i, m["state"])
		}
	}
	if total, _ := result["total"].(float64); int(total) != 2 {
		t.Errorf("expected total 2, got %v", result["total"])
	}
}

func TestMCPCreateIssuesBatch_EmptyArray(t *testing.T) {
	srv, _, _, _, _ := setupTestWithProject(t)

	params, _ := json.Marshal(map[string]interface{}{
		"name": "create_issues_batch",
		"arguments": map[string]interface{}{
			"issues": []map[string]interface{}{},
		},
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  params,
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error == nil {
		t.Fatal("expected error for empty issues array")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("expected error code -32602, got %d", resp.Error.Code)
	}
}

func TestMCPCreateIssuesBatch_MissingTitle(t *testing.T) {
	srv, _, _, agent, _ := setupTestWithProject(t)

	params, _ := json.Marshal(map[string]interface{}{
		"name": "create_issues_batch",
		"arguments": map[string]interface{}{
			"issues": []map[string]interface{}{
				{"title": "Valid issue"},
				{"description": "Missing title"},
			},
		},
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  params,
	}
	resp := srv.HandleRequest(req, agent.ID, "")
	if resp.Error == nil {
		t.Fatal("expected error for missing title")
	}
	if !strings.Contains(resp.Error.Message, "issues[1]") {
		t.Errorf("expected error to reference issues[1], got: %s", resp.Error.Message)
	}
}

func TestMCPCreateIssuesBatch_InvalidPriority(t *testing.T) {
	srv, _, _, agent, _ := setupTestWithProject(t)

	params, _ := json.Marshal(map[string]interface{}{
		"name": "create_issues_batch",
		"arguments": map[string]interface{}{
			"issues": []map[string]interface{}{
				{"title": "Good issue", "priority": "medium"},
				{"title": "Bad priority", "priority": "urgent"},
			},
		},
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  params,
	}
	resp := srv.HandleRequest(req, agent.ID, "")
	if resp.Error == nil {
		t.Fatal("expected error for invalid priority")
	}
	if !strings.Contains(resp.Error.Message, "issues[1]") {
		t.Errorf("expected error to reference issues[1], got: %s", resp.Error.Message)
	}
}

func TestMCPCreateIssuesBatch_WithAssignees(t *testing.T) {
	srv, _, agentSvc, agent, _ := setupTestWithProject(t)

	agent1, _ := agentSvc.Register("batch-assignee-1", models.AgentKindAI, "batch-ass-001", "secret", nil, "", "")
	agent2, _ := agentSvc.Register("batch-assignee-2", models.AgentKindAI, "batch-ass-002", "secret", nil, "", "")

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issues_batch",
		"arguments": map[string]interface{}{
			"issues": []map[string]interface{}{
				{
					"title":       "Assigned batch issue",
					"priority":    "critical",
					"assigneeIds": []string{fmt.Sprintf("%d", agent1.ID), fmt.Sprintf("%d", agent2.ID)},
				},
			},
		},
	}, agent.ID)

	items, ok := result["items"].([]interface{})
	if !ok {
		data, _ := json.Marshal(result["items"])
		json.Unmarshal(data, &items)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}
