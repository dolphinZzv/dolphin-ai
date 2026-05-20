//go:build integration

package mcp_test

import (
	"os"
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

func pgDSN() string {
	if dsn := os.Getenv("CHICK_TEST_DSN"); dsn != "" {
		return dsn
	}
	return "host=localhost user=postgres password=postgres dbname=chick_test sslmode=disable"
}

// setupMCPIntegration creates a full MCP test environment with PostgreSQL.
func setupMCPIntegration(t *testing.T) (*mcp.Server, *service.ProjectService, *service.AgentService, *service.IssueService) {
	t.Helper()

	cfg := &config.Config{DBDriver: "postgres", DBDSN: pgDSN()}
	db, err := server.NewDB(cfg)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	if err := server.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	t.Cleanup(func() {
		db.Exec("TRUNCATE TABLE issues, agents, projects, project_members, issue_assignees, comments, labels, timeline_events, milestones, feedbacks RESTART IDENTITY CASCADE")
	})

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

	projectSvc := service.NewProjectService(projectRepo, memberRepo, labelRepo, milestoneRepo)
	agentSvc := service.NewAgentService(agentRepo, bus, nil, true)
	commentSvc := service.NewCommentService(db, commentRepo, timelineRepo, issueRepo, bus)
	issueSvc := service.NewIssueService(db, issueRepo, assigneeRepo, timelineRepo, projectRepo, bus)
	workflowSvc := service.NewWorkflowService(issueSvc)
	feedbackSvc := service.NewFeedbackService(feedbackRepo, bus)
	notifSvc := notifications.NewService(nil, nil)
	notifSvc.Subscribe(bus)
	handlers := mcp.NewHandlers(projectSvc, agentSvc, issueSvc, commentSvc, workflowSvc, feedbackSvc, notifSvc, 0)
	mcpServer := mcp.NewServer(handlers)

	return mcpServer, projectSvc, agentSvc, issueSvc
}

func TestIntegration_SubmitRequirement(t *testing.T) {
	_, projectSvc, agentSvc, issueSvc := setupMCPIntegration(t)

	// Create an agent
	agent, err := agentSvc.Register("int-req-agent", models.AgentKindAI, "int-req-001", "secret", nil, "", "")
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}

	// Create the designated requirement project
	proj, err := projectSvc.Create("需求池", "LLM requirement project")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	projectSvc.AddMember(proj.ID, agent.ID, models.ProjectRoleOwner)

	// Build a new server with the configured project ID
	handlers := mcp.NewHandlers(
		projectSvc, agentSvc, issueSvc,
		nil, nil, nil, nil,
		proj.ID,
	)
	srv := mcp.NewServer(handlers)

	// Submit a requirement
	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "submit_requirement",
		"arguments": map[string]interface{}{
			"title":       "Add login page",
			"description": "Users need OAuth2 login",
		},
	}, agent.ID)

	if result["title"] != "Add login page" {
		t.Errorf("expected title 'Add login page', got %v", result["title"])
	}
	if result["description"] != "Users need OAuth2 login" {
		t.Errorf("expected description 'Users need OAuth2 login', got %v", result["description"])
	}
	if result["state"] != "open" {
		t.Errorf("expected state 'open', got %v", result["state"])
	}
	if id, ok := result["id"].(string); !ok || id == "" {
		t.Errorf("expected non-empty id, got %v", id)
	}

	// Verify the issue was created in the designated project
	issue, err := issueSvc.GetByID(parseUint(result["id"].(string)))
	if err != nil {
		t.Fatalf("get created issue: %v", err)
	}
	if issue.ProjectID != proj.ID {
		t.Errorf("expected project %d, got %d", proj.ID, issue.ProjectID)
	}
}

func TestIntegration_SubmitRequirement_NoConfig(t *testing.T) {
	mcpSrv, _, agentSvc, _ := setupMCPIntegration(t)

	agent, err := agentSvc.Register("int-req-no-cfg", models.AgentKindAI, "int-req-no-cfg", "secret", nil, "", "")
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}

	// Submit without configuring a project — should fail
	_, callErr := callRaw(t, mcpSrv, "tools/call", map[string]interface{}{
		"name": "submit_requirement",
		"arguments": map[string]interface{}{
			"title":       "Should fail",
			"description": "No project configured",
		},
	}, agent.ID)
	if callErr == nil {
		t.Error("expected error when no requirement project is configured")
	}
}

func TestIntegration_CheckNotifications(t *testing.T) {
	_, projectSvc, agentSvc, issueSvc := setupMCPIntegration(t)

	// Create an agent
	agent, err := agentSvc.Register("notif-agent", models.AgentKindAI, "notif-001", "secret", nil, "", "")
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}

	// Create a project and add agent as member
	proj, err := projectSvc.Create("通知测试项目", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	projectSvc.AddMember(proj.ID, agent.ID, models.ProjectRoleOwner)

	// Create an issue with the agent as assignee — triggers notification
	issue, err := issueSvc.Create(proj.ID, "Test issue", "desc", models.PriorityMedium, agent.ID, nil, nil, 0, "", "")
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	// Assign the agent
	issueSvc.AddAssignee(issue.ID, agent.ID)

	// Build a server with notification service
	bus := events.NewBus()
	notifSvc := notifications.NewService(nil, nil)
	notifSvc.Subscribe(bus)
	handlers := mcp.NewHandlers(projectSvc, agentSvc, issueSvc, nil, nil, nil, notifSvc, 0)
	srv := mcp.NewServer(handlers)

	// Call check_notifications
	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "check_notifications",
		"arguments": map[string]interface{}{},
	}, agent.ID)

	notifs, ok := result["notifications"].([]interface{})
	if !ok {
		t.Fatal("expected notifications array")
	}
	if len(notifs) == 0 {
		t.Fatal("expected at least one notification")
	}

	n := notifs[0].(map[string]interface{})
	if n["type"] != "issue_assigned" {
		t.Errorf("expected type 'issue_assigned', got %v", n["type"])
	}
	if n["issueId"] != float64(issue.ID) {
		t.Errorf("expected issueId %d, got %v", issue.ID, n["issueId"])
	}
}

func parseUint(s string) uint {
	var n uint
	for _, c := range s {
		n = n*10 + uint(c-'0')
	}
	return n
}
