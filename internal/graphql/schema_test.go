//go:build !integration

package graph

import (
	"context"
	"testing"

	"chick/internal/auth"
	"chick/internal/config"
	"chick/internal/events"
	gormrepo "chick/internal/repository/gorm"
	"chick/internal/server"
	"chick/internal/service"
)

// authedCtx returns a context with the given agent ID for testing resolvers that require auth.
func authedCtx(agentID uint) context.Context {
	return auth.WithAgentID(context.Background(), agentID)
}

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
	commentSvc := service.NewCommentService(db, commentRepo, timelineRepo, issueRepo, bus)
	issueSvc := service.NewIssueService(db, issueRepo, assigneeRepo, timelineRepo, projectRepo, bus)
	workflowSvc := service.NewWorkflowService(issueSvc)
	feedbackSvc := service.NewFeedbackService(feedbackRepo, bus)

	return NewResolver(projectSvc, agentSvc, issueSvc, commentSvc, workflowSvc, feedbackSvc, bus)
}

// registerSystemAgent registers a human agent via the public registerAgent resolver and returns its parsed ID.
func registerSystemAgent(t *testing.T, r *Resolver, suffix string) uint {
	t.Helper()
	agent, err := r.Mutation().RegisterAgent(context.Background(), "system-"+suffix, AgentKindHuman, "sys-"+suffix, "pass", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("register system agent: %v", err)
	}
	return parseID(agent.Agent.ID)
}

func TestGraphQL_QueryProjects(t *testing.T) {
	r := setupTestResolver(t)
	sysID := registerSystemAgent(t, r, "qp")
	ctx := authedCtx(sysID)

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

	regResult, err := r.Mutation().RegisterAgent(ctx, "test-bot", AgentKindAi, "ext-1", "secret", []string{"CODING"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if regResult.Agent.Name != "test-bot" {
		t.Errorf("expected 'test-bot', got %s", regResult.Agent.Name)
	}

	loginResult, err := r.Mutation().LoginAgent(ctx, "ext-1", "secret")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if loginResult.Agent.Name != "test-bot" {
		t.Errorf("expected 'test-bot', got %s", loginResult.Agent.Name)
	}

	// Wrong password
	_, err = r.Mutation().LoginAgent(ctx, "ext-1", "wrong")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestGraphQL_QueryAgents(t *testing.T) {
	r := setupTestResolver(t)
	sysID := registerSystemAgent(t, r, "qa")
	ctx := authedCtx(sysID)

	r.Mutation().RegisterAgent(ctx, "bot1", AgentKindAi, "ext-a", "secret", []string{"CODING"}, nil, nil, nil)
	r.Mutation().RegisterAgent(ctx, "bot2", AgentKindAi, "ext-b", "secret", []string{"TESTING"}, nil, nil, nil)

	agents, err := r.Query().Agents(ctx, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("agents: %v", err)
	}
	if len(agents) != 3 {
		t.Errorf("expected 3 agents, got %d", len(agents))
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
	sysID := registerSystemAgent(t, r, "ii")
	ctx := authedCtx(sysID)

	project, err := r.Mutation().CreateProject(ctx, "P1", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	issue, err := r.Mutation().CreateIssue(ctx, project.ID, formatID(sysID), "Test Issue", strPtr("body"), PriorityMedium, nil, nil)
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
	sysID := registerSystemAgent(t, r, "tr")
	ctx := authedCtx(sysID)

	project, err := r.Mutation().CreateProject(ctx, "P1", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issue, err := r.Mutation().CreateIssue(ctx, project.ID, formatID(sysID), "Transitions", nil, PriorityLow, nil, nil)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// OPEN -> IN_PROGRESS
	updated, err := r.Mutation().TransitionIssue(ctx, issue.ID, IssueStateInProgress, formatID(sysID))
	if err != nil {
		t.Fatalf("transition: %v", err)
	}
	if updated.State != IssueStateInProgress {
		t.Errorf("expected in_progress, got %s", updated.State)
	}

	// IN_PROGRESS -> REVIEW
	updated, err = r.Mutation().TransitionIssue(ctx, issue.ID, IssueStateReview, formatID(sysID))
	if err != nil {
		t.Fatalf("transition: %v", err)
	}
	if updated.State != IssueStateReview {
		t.Errorf("expected review, got %s", updated.State)
	}
}

func TestGraphQL_AddComment(t *testing.T) {
	r := setupTestResolver(t)
	sysID := registerSystemAgent(t, r, "cm")
	ctx := authedCtx(sysID)

	project, err := r.Mutation().CreateProject(ctx, "P", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issue, err := r.Mutation().CreateIssue(ctx, project.ID, formatID(sysID), "C", nil, PriorityLow, nil, nil)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	comment, err := r.Mutation().AddComment(ctx, issue.ID, formatID(sysID), "Hello", CommentContentTypeMarkdown, nil)
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
	sysID := registerSystemAgent(t, r, "tl")
	ctx := authedCtx(sysID)

	project, err := r.Mutation().CreateProject(ctx, "P", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issue, err := r.Mutation().CreateIssue(ctx, project.ID, formatID(sysID), "T", nil, PriorityLow, nil, nil)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

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
	sysID := registerSystemAgent(t, r, "pm")
	ctx := authedCtx(sysID)

	project, err := r.Mutation().CreateProject(ctx, "P", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Register a separate member agent
	memberAgent, err := r.Mutation().RegisterAgent(context.Background(), "member1", AgentKindAi, "m-1", "pass", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("register member: %v", err)
	}

	member, err := r.Mutation().AddProjectMember(ctx, project.ID, memberAgent.Agent.ID, ProjectRoleMember)
	if err != nil {
		t.Fatalf("add member: %v", err)
	}
	if member.Role != ProjectRoleMember {
		t.Errorf("expected member role, got %s", member.Role)
	}

	removed, err := r.Mutation().RemoveProjectMember(ctx, project.ID, memberAgent.Agent.ID)
	if err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if !removed {
		t.Error("expected true for removal")
	}
}

func TestGraphQL_CommentsQuery(t *testing.T) {
	r := setupTestResolver(t)
	sysID := registerSystemAgent(t, r, "cq")
	ctx := authedCtx(sysID)

	project, err := r.Mutation().CreateProject(ctx, "P", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issue, err := r.Mutation().CreateIssue(ctx, project.ID, formatID(sysID), "C2", nil, PriorityLow, nil, nil)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	r.Mutation().AddComment(ctx, issue.ID, formatID(sysID), "First", CommentContentTypeMarkdown, nil)
	r.Mutation().AddComment(ctx, issue.ID, formatID(sysID), "Second", CommentContentTypeMarkdown, nil)

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
