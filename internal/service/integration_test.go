//go:build integration

package service_test

import (
	"os"
	"testing"

	"chick/internal/config"
	"chick/internal/events"
	"chick/internal/models"
	gormrepo "chick/internal/repository/gorm"
	"chick/internal/server"
	"chick/internal/service"
)

func pgDSN() string {
	if dsn := os.Getenv("CHICK_TEST_DSN"); dsn != "" {
		return dsn
	}
	return "host=localhost user=postgres password=postgres dbname=chick_test sslmode=disable"
}

type integrationFixture struct {
	issueSvc    *service.IssueService
	agentSvc    *service.AgentService
	projectSvc  *service.ProjectService
	commentSvc  *service.CommentService
	workflowSvc *service.WorkflowService
	creatorID   uint
}

func setupIntegration(t *testing.T) *integrationFixture {
	t.Helper()
	cfg := &config.Config{DBDriver: "postgres", DBDSN: pgDSN()}
	db, err := server.NewDB(cfg)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	if err := server.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Clean all tables before test
	t.Cleanup(func() {
		db.Exec("TRUNCATE TABLE issues, agents, projects, project_members, issue_assignees, comments, labels, timeline_events, milestones, skills, feedbacks RESTART IDENTITY CASCADE")
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
	bus := events.NewBus()

	projectSvc := service.NewProjectService(projectRepo, memberRepo, labelRepo, milestoneRepo)
	agentSvc := service.NewAgentService(agentRepo, bus, nil)
	commentSvc := service.NewCommentService(db, commentRepo, timelineRepo, issueRepo, bus)
	issueSvc := service.NewIssueService(db, issueRepo, assigneeRepo, timelineRepo, projectRepo, bus)
	workflowSvc := service.NewWorkflowService(issueSvc)

	// Create default creator agent (PostgreSQL enforces FK constraints)
	creator, err := agentSvc.Register("default-creator", models.AgentKindHuman, "default-creator-1", "pass", nil)
	if err != nil {
		t.Fatalf("create default creator: %v", err)
	}

	return &integrationFixture{
		issueSvc:    issueSvc,
		agentSvc:    agentSvc,
		projectSvc:  projectSvc,
		commentSvc:  commentSvc,
		workflowSvc: workflowSvc,
		creatorID:   creator.ID,
	}
}

func TestIntegration_CreateIssue(t *testing.T) {
	fx := setupIntegration(t)

	p, _ := fx.projectSvc.Create("PG Project", "Integration test")
	pid := p.ID

	issue1, err := fx.issueSvc.Create(pid, fx.creatorID, "First PG issue", "", models.PriorityMedium, nil, nil)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if issue1.Number != 1 {
		t.Errorf("expected number 1, got %d", issue1.Number)
	}
	if issue1.ProjectID != pid {
		t.Errorf("expected project %d, got %d", pid, issue1.ProjectID)
	}

	issue2, err := fx.issueSvc.Create(pid, fx.creatorID, "Second PG issue", "", models.PriorityHigh, nil, nil)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if issue2.Number != 2 {
		t.Errorf("expected number 2, got %d", issue2.Number)
	}
}

func TestIntegration_IssueTransitions(t *testing.T) {
	fx := setupIntegration(t)

	p, _ := fx.projectSvc.Create("Workflow Project", "")
	issue, _ := fx.issueSvc.Create(p.ID, fx.creatorID, "Transition me", "", models.PriorityMedium, nil, nil)

	// OPEN -> IN_PROGRESS
	issue, err := fx.workflowSvc.Transition(issue.ID, models.IssueStateInProgress, fx.creatorID)
	if err != nil {
		t.Fatalf("to in_progress: %v", err)
	}
	if issue.State != models.IssueStateInProgress {
		t.Errorf("expected in_progress, got %s", issue.State)
	}

	// IN_PROGRESS -> REVIEW
	issue, err = fx.workflowSvc.Transition(issue.ID, models.IssueStateReview, fx.creatorID)
	if err != nil {
		t.Fatalf("to review: %v", err)
	}
	if issue.State != models.IssueStateReview {
		t.Errorf("expected review, got %s", issue.State)
	}

	// REVIEW -> CLOSED_COMPLETED
	issue, err = fx.workflowSvc.Transition(issue.ID, models.IssueStateClosedCompleted, fx.creatorID)
	if err != nil {
		t.Fatalf("to closed: %v", err)
	}
	if issue.State != models.IssueStateClosedCompleted {
		t.Errorf("expected closed_completed, got %s", issue.State)
	}
}

func TestIntegration_InvalidTransition(t *testing.T) {
	fx := setupIntegration(t)

	p, _ := fx.projectSvc.Create("Invalid Transitions", "")
	issue, _ := fx.issueSvc.Create(p.ID, fx.creatorID, "Invalid", "", models.PriorityMedium, nil, nil)

	_, err := fx.workflowSvc.Transition(issue.ID, models.IssueStateClosedCompleted, fx.creatorID)
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}
}

func TestIntegration_AgentRegisterAndLogin(t *testing.T) {
	fx := setupIntegration(t)

	_, err := fx.agentSvc.Register("pg-bot", models.AgentKindAI, "pg-bot-1", "password", []string{"CODING"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	result, err := fx.agentSvc.Login("pg-bot-1", "password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.Agent.Name != "pg-bot" {
		t.Errorf("expected pg-bot, got %s", result.Agent.Name)
	}

	_, err = fx.agentSvc.Login("pg-bot-1", "wrong")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestIntegration_AddComment(t *testing.T) {
	fx := setupIntegration(t)

	p, _ := fx.projectSvc.Create("Comment Project", "")
	agent, _ := fx.agentSvc.Register("commenter", models.AgentKindHuman, "commenter-1", "pass", nil)
	issue, _ := fx.issueSvc.Create(p.ID, fx.creatorID, "Commentable", "", models.PriorityLow, nil, nil)

	comment, err := fx.commentSvc.Create(issue.ID, agent.ID, "PG comment body", models.CommentMarkdown, nil)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if comment.Body != "PG comment body" {
		t.Errorf("expected 'PG comment body', got %s", comment.Body)
	}
	if comment.ContentType != models.CommentMarkdown {
		t.Errorf("expected markdown, got %s", comment.ContentType)
	}

	// Verify timeline
	timeline, err := fx.issueSvc.ListTimeline(issue.ID)
	if err != nil {
		t.Fatalf("list timeline: %v", err)
	}
	if len(timeline) != 2 { // issue created + comment added
		t.Errorf("expected 2 timeline events, got %d", len(timeline))
	}
}

func TestIntegration_ListIssues(t *testing.T) {
	fx := setupIntegration(t)

	p, _ := fx.projectSvc.Create("List Project", "")
	pid := p.ID

	fx.issueSvc.Create(pid, fx.creatorID, "Alpha", "", models.PriorityHigh, nil, nil)
	fx.issueSvc.Create(pid, fx.creatorID, "Beta", "", models.PriorityMedium, nil, nil)
	fx.issueSvc.Create(pid, fx.creatorID, "Gamma", "", models.PriorityLow, nil, nil)

	issues, total, err := fx.issueSvc.List(models.IssueFilter{ProjectID: &pid})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3 issues, got %d", total)
	}
	if len(issues) != 3 {
		t.Errorf("expected 3 returned, got %d", len(issues))
	}

	// Filter by priority
	high := models.PriorityHigh
	issues, total, err = fx.issueSvc.List(models.IssueFilter{ProjectID: &pid, Priority: &high})
	if err != nil {
		t.Fatalf("list high: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 high priority, got %d", total)
	}

	// Pagination
	limit := 2
	issues, total, err = fx.issueSvc.List(models.IssueFilter{ProjectID: &pid, Limit: limit})
	if err != nil {
		t.Fatalf("list paginated: %v", err)
	}
	if len(issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(issues))
	}
	if total != 3 {
		t.Errorf("expected total 3, got %d", total)
	}
}
