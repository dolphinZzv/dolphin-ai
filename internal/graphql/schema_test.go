//go:build !integration

package graph

import (
	"context"
	"testing"

	"chick/internal/config"
	"chick/internal/events"
	gormrepo "chick/internal/repository/gorm"
	"chick/internal/server"
	"chick/internal/service"
)

func setupTestResolver(t *testing.T) *Resolver {
	t.Helper()
	db, err := server.NewDB(&config.Config{DBDriver: "sqlite3", DBDSN: "file::memory:"})
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	server.AutoMigrate(db)

	bus := events.NewBus()
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

	projectSvc := service.NewProjectService(projectRepo, memberRepo, labelRepo, milestoneRepo)
	agentSvc := service.NewAgentService(agentRepo, bus, nil)
	commentSvc := service.NewCommentService(commentRepo, timelineRepo, bus)
	issueSvc := service.NewIssueService(issueRepo, assigneeRepo, timelineRepo, projectRepo, bus)
	workflowSvc := service.NewWorkflowService(issueSvc)
	feedbackSvc := service.NewFeedbackService(feedbackRepo, bus)

	return NewResolver(projectSvc, agentSvc, issueSvc, commentSvc, workflowSvc, feedbackSvc, bus)
}

func TestGraphQL_QueryProjects(t *testing.T) {
	r := setupTestResolver(t)
	ctx := context.Background()

	// Initially empty
	projects, err := r.Query().Projects(ctx)
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}

	// Create a project via mutation
	p, err := r.Mutation().CreateProject(ctx, "Test", strPtr("desc"))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if p.Name != "Test" {
		t.Errorf("expected 'Test', got %s", p.Name)
	}
	if *p.Description != "desc" {
		t.Errorf("expected 'desc', got %s", *p.Description)
	}

	// Query again
	projects, err = r.Query().Projects(ctx)
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	if len(projects) != 1 {
		t.Errorf("expected 1 project, got %d", len(projects))
	}

	// Get by ID
	got, err := r.Query().Project(ctx, p.ID)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if got.Name != "Test" {
		t.Errorf("expected 'Test', got %s", got.Name)
	}
}

func TestGraphQL_RegisterAndLoginAgent(t *testing.T) {
	r := setupTestResolver(t)
	ctx := context.Background()

	agent, err := r.Mutation().RegisterAgent(ctx, "test-bot", AgentKindAi, "ext-1", "secret", []string{"CODING"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if agent.Name != "test-bot" {
		t.Errorf("expected 'test-bot', got %s", agent.Name)
	}

	loginResult, err := r.Mutation().LoginAgent(ctx, "ext-1", "secret")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if loginResult.Agent.Name != "test-bot" {
		t.Errorf("expected 'test-bot', got %s", loginResult.Agent.Name)
	}
	// Token may be empty since no token generator in test setup

	// Wrong password
	_, err = r.Mutation().LoginAgent(ctx, "ext-1", "wrong")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestGraphQL_QueryAgents(t *testing.T) {
	r := setupTestResolver(t)
	ctx := context.Background()

	r.Mutation().RegisterAgent(ctx, "bot1", AgentKindAi, "ext-a", "secret", []string{"CODING"})
	r.Mutation().RegisterAgent(ctx, "bot2", AgentKindAi, "ext-b", "secret", []string{"TESTING"})

	agents, err := r.Query().Agents(ctx, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("agents: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(agents))
	}
}

func TestGraphQL_AgentKindEnum(t *testing.T) {
	if AgentKindAi != "ai" || AgentKindHuman != "human" || AgentKindHybrid != "hybrid" {
		t.Error("AgentKind enum values mismatch")
	}
}

func TestGraphQL_AgentStatusEnum(t *testing.T) {
	if AgentStatusOnline != "online" || AgentStatusBusy != "busy" {
		t.Error("AgentStatus enum values mismatch")
	}
}

func TestGraphQL_IssueIntegration(t *testing.T) {
	r := setupTestResolver(t)
	ctx := context.Background()

	project, _ := r.Mutation().CreateProject(ctx, "P1", nil)
	agent, _ := r.Mutation().RegisterAgent(ctx, "creator", AgentKindHuman, "cr-1", "pass", nil)

	issue, err := r.Mutation().CreateIssue(ctx, project.ID, agent.ID, "Test Issue", strPtr("body"), PriorityMedium, nil, nil)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if issue.Title != "Test Issue" {
		t.Errorf("expected 'Test Issue', got %s", issue.Title)
	}
	if issue.State != IssueStateOpen {
		t.Errorf("expected open, got %s", issue.State)
	}

	// Get issue by ID
	got, err := r.Query().Issue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if got.Title != "Test Issue" {
		t.Errorf("expected 'Test Issue', got %s", got.Title)
	}

	// Issue list
	conn, err := r.Query().Issues(ctx, project.ID, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("issues: %v", err)
	}
	if conn.Total != 1 {
		t.Errorf("expected 1 issue, got %d", conn.Total)
	}
}

func TestGraphQL_ValidTransitions(t *testing.T) {
	r := setupTestResolver(t)
	ctx := context.Background()

	transitions, err := r.Query().ValidTransitions(ctx, IssueStateOpen)
	if err != nil {
		t.Fatalf("valid transitions: %v", err)
	}
	found := false
	for _, s := range transitions {
		if s == IssueStateInProgress {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected in_progress as valid transition from open")
	}
}

func TestGraphQL_TransitionIssue(t *testing.T) {
	r := setupTestResolver(t)
	ctx := context.Background()

	project, _ := r.Mutation().CreateProject(ctx, "P1", nil)
	agent, _ := r.Mutation().RegisterAgent(ctx, "creator", AgentKindHuman, "cr-2", "pass", nil)
	issue, _ := r.Mutation().CreateIssue(ctx, project.ID, agent.ID, "Transitions", nil, PriorityLow, nil, nil)

	// OPEN -> IN_PROGRESS
	updated, err := r.Mutation().TransitionIssue(ctx, issue.ID, IssueStateInProgress, agent.ID)
	if err != nil {
		t.Fatalf("transition: %v", err)
	}
	if updated.State != IssueStateInProgress {
		t.Errorf("expected in_progress, got %s", updated.State)
	}

	// IN_PROGRESS -> REVIEW
	updated, err = r.Mutation().TransitionIssue(ctx, issue.ID, IssueStateReview, agent.ID)
	if err != nil {
		t.Fatalf("transition: %v", err)
	}
	if updated.State != IssueStateReview {
		t.Errorf("expected review, got %s", updated.State)
	}
}

func TestGraphQL_AddComment(t *testing.T) {
	r := setupTestResolver(t)
	ctx := context.Background()

	project, _ := r.Mutation().CreateProject(ctx, "P", nil)
	agent, _ := r.Mutation().RegisterAgent(ctx, "user", AgentKindHuman, "u-1", "pass", nil)
	issue, _ := r.Mutation().CreateIssue(ctx, project.ID, agent.ID, "C", nil, PriorityLow, nil, nil)

	comment, err := r.Mutation().AddComment(ctx, issue.ID, agent.ID, "Hello", CommentContentTypeMarkdown, nil)
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if comment.Body != "Hello" {
		t.Errorf("expected 'Hello', got %s", comment.Body)
	}
	if comment.ContentType != CommentContentTypeMarkdown {
		t.Errorf("expected markdown, got %s", comment.ContentType)
	}
}

func TestGraphQL_Timeline(t *testing.T) {
	r := setupTestResolver(t)
	ctx := context.Background()

	project, _ := r.Mutation().CreateProject(ctx, "P", nil)
	agent, _ := r.Mutation().RegisterAgent(ctx, "u", AgentKindHuman, "u-2", "pass", nil)
	issue, _ := r.Mutation().CreateIssue(ctx, project.ID, agent.ID, "T", nil, PriorityLow, nil, nil)

	events, err := r.Query().Timeline(ctx, issue.ID)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(events) < 1 {
		t.Errorf("expected at least 1 timeline event, got %d", len(events))
	}
}

func TestGraphQL_ProjectMembers(t *testing.T) {
	r := setupTestResolver(t)
	ctx := context.Background()

	project, _ := r.Mutation().CreateProject(ctx, "P", nil)
	agent, _ := r.Mutation().RegisterAgent(ctx, "member1", AgentKindAi, "m-1", "pass", nil)

	member, err := r.Mutation().AddProjectMember(ctx, project.ID, agent.ID, ProjectRoleMember)
	if err != nil {
		t.Fatalf("add member: %v", err)
	}
	if member.Role != ProjectRoleMember {
		t.Errorf("expected member role, got %s", member.Role)
	}

	removed, err := r.Mutation().RemoveProjectMember(ctx, project.ID, agent.ID)
	if err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if !removed {
		t.Error("expected true for removal")
	}
}

func TestGraphQL_CommentsQuery(t *testing.T) {
	r := setupTestResolver(t)
	ctx := context.Background()

	project, _ := r.Mutation().CreateProject(ctx, "P", nil)
	agent, _ := r.Mutation().RegisterAgent(ctx, "u", AgentKindHuman, "u-3", "pass", nil)
	issue, _ := r.Mutation().CreateIssue(ctx, project.ID, agent.ID, "C2", nil, PriorityLow, nil, nil)

	r.Mutation().AddComment(ctx, issue.ID, agent.ID, "First", CommentContentTypeMarkdown, nil)
	r.Mutation().AddComment(ctx, issue.ID, agent.ID, "Second", CommentContentTypeMarkdown, nil)

	comments, err := r.Query().Comments(ctx, issue.ID)
	if err != nil {
		t.Fatalf("comments: %v", err)
	}
	if len(comments) != 2 {
		t.Errorf("expected 2 comments, got %d", len(comments))
	}
}

func TestGraphQL_NewHandlerCreatesHTTPHandler(t *testing.T) {
	r := setupTestResolver(t)
	h := NewHandler(r.ProjectSvc, r.AgentSvc, r.IssueSvc, r.CommentSvc, r.WorkflowSvc, r.FeedbackSvc, r.EventBus)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}
