package service_test

import (
	"testing"

	"chick/internal/config"
	"chick/internal/events"
	"chick/internal/models"
	gormrepo "chick/internal/repository/gorm"
	"chick/internal/server"
	"chick/internal/service"
)

func setupIssueTest(t *testing.T) (*service.IssueService, *service.AgentService, *service.ProjectService, *service.WorkflowService) {
	t.Helper()
	db, err := server.NewDB(&config.Config{DBDriver: "sqlite3", DBDSN: "file::memory:"})
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	if err := server.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

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
	agentSvc := service.NewAgentService(agentRepo, bus, nil, true)
	commentSvc := service.NewCommentService(db, commentRepo, timelineRepo, issueRepo, bus)
	issueSvc := service.NewIssueService(db, issueRepo, assigneeRepo, timelineRepo, projectRepo, bus)
	workflowSvc := service.NewWorkflowService(issueSvc)

	_ = commentSvc

	return issueSvc, agentSvc, projectSvc, workflowSvc
}

func TestCreateIssue_AutoNumber(t *testing.T) {
	issueSvc, _, projectSvc, _ := setupIssueTest(t)

	p, _ := projectSvc.Create("Test", "")
	pid := p.ID

	issue1, err := issueSvc.Create(pid, 1, "First", "", models.PriorityMedium, nil, nil, nil)
	if err != nil {
		t.Fatalf("create first issue: %v", err)
	}
	if issue1.Number != 1 {
		t.Errorf("expected number 1, got %d", issue1.Number)
	}

	issue2, err := issueSvc.Create(pid, 1, "Second", "", models.PriorityMedium, nil, nil, nil)
	if err != nil {
		t.Fatalf("create second issue: %v", err)
	}
	if issue2.Number != 2 {
		t.Errorf("expected number 2, got %d", issue2.Number)
	}
}

func TestCreateIssue_WithAssignees(t *testing.T) {
	issueSvc, agentSvc, projectSvc, _ := setupIssueTest(t)

	p, _ := projectSvc.Create("Test", "")
	agent, _ := agentSvc.Register("coder", models.AgentKindAI, "coder-1", "secret", []string{"CODING"}, "", "")

	issue, err := issueSvc.Create(p.ID, 1, "Task", "", models.PriorityHigh, []uint{agent.ID}, nil, nil)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if len(issue.Assignees) != 1 {
		t.Errorf("expected 1 assignee, got %d", len(issue.Assignees))
	}
	if issue.Assignees[0].State != models.AssigneeStatePending {
		t.Errorf("expected pending state, got %s", issue.Assignees[0].State)
	}
}

func TestTransitionIssue_Valid(t *testing.T) {
	issueSvc, _, projectSvc, workflowSvc := setupIssueTest(t)
	p, _ := projectSvc.Create("Test", "")

	issue, _ := issueSvc.Create(p.ID, 1, "Test", "", models.PriorityMedium, nil, nil, nil)

	// OPEN -> IN_PROGRESS
	issue, err := workflowSvc.Transition(issue.ID, models.IssueStateInProgress, 1, nil)
	if err != nil {
		t.Fatalf("transition to in_progress: %v", err)
	}
	if issue.State != models.IssueStateInProgress {
		t.Errorf("expected in_progress, got %s", issue.State)
	}

	// IN_PROGRESS -> REVIEW
	issue, err = workflowSvc.Transition(issue.ID, models.IssueStateReview, 1, nil)
	if err != nil {
		t.Fatalf("transition to review: %v", err)
	}
	if issue.State != models.IssueStateReview {
		t.Errorf("expected review, got %s", issue.State)
	}

	// REVIEW -> CLOSED_COMPLETED
	issue, err = workflowSvc.Transition(issue.ID, models.IssueStateClosedCompleted, 1, nil)
	if err != nil {
		t.Fatalf("transition to closed: %v", err)
	}
	if issue.State != models.IssueStateClosedCompleted {
		t.Errorf("expected closed_completed, got %s", issue.State)
	}
}

func TestTransitionIssue_Invalid(t *testing.T) {
	issueSvc, _, projectSvc, workflowSvc := setupIssueTest(t)
	p, _ := projectSvc.Create("Test", "")

	issue, _ := issueSvc.Create(p.ID, 1, "Test", "", models.PriorityMedium, nil, nil, nil)

	// OPEN -> CLOSED (invalid, must go through IN_PROGRESS -> REVIEW)
	_, err := workflowSvc.Transition(issue.ID, models.IssueStateClosedCompleted, 1, nil)
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}
}

func TestAddComment(t *testing.T) {
	db, err := server.NewDB(&config.Config{DBDriver: "sqlite3", DBDSN: "file::memory:"})
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	server.AutoMigrate(db)

	issueRepo := gormrepo.NewIssueRepo(db)
	assigneeRepo := gormrepo.NewIssueAssigneeRepo(db)
	projectRepo := gormrepo.NewProjectRepo(db)
	memberRepo := gormrepo.NewProjectMemberRepo(db)
	agentRepo := gormrepo.NewAgentRepo(db)
	commentRepo := gormrepo.NewCommentRepo(db)
	timelineRepo := gormrepo.NewTimelineRepo(db)
	labelRepo := gormrepo.NewLabelRepo(db)
	milestoneRepo := gormrepo.NewMilestoneRepo(db)
	bus := events.NewBus()

	projectSvc := service.NewProjectService(projectRepo, memberRepo, labelRepo, milestoneRepo)
	agentSvc := service.NewAgentService(agentRepo, bus, nil, true)
	issueSvc := service.NewIssueService(db, issueRepo, assigneeRepo, timelineRepo, projectRepo, bus)
	commentSvc := service.NewCommentService(db, commentRepo, timelineRepo, issueRepo, bus)

	p, _ := projectSvc.Create("Test", "")
	agent, _ := agentSvc.Register("user", models.AgentKindHuman, "user-1", "secret", nil, "", "")
	issue, _ := issueSvc.Create(p.ID, agent.ID, "Issue", "", models.PriorityMedium, nil, nil, nil)

	comment, err := commentSvc.Create(issue.ID, agent.ID, "Hello world", models.CommentMarkdown, nil)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if comment.Body != "Hello world" {
		t.Errorf("expected 'Hello world', got %s", comment.Body)
	}
	if comment.ContentType != models.CommentMarkdown {
		t.Errorf("expected markdown content type")
	}

	// Verify timeline event was created
	events, err := timelineRepo.ListByIssue(issue.ID)
	if err != nil {
		t.Fatalf("list timeline: %v", err)
	}
	if len(events) != 2 { // issue created + comment added
		t.Errorf("expected 2 timeline events, got %d", len(events))
	}
}

func TestListIssues(t *testing.T) {
	issueSvc, _, projectSvc, _ := setupIssueTest(t)
	p, _ := projectSvc.Create("Test", "")

	issueSvc.Create(p.ID, 1, "Alpha", "", models.PriorityHigh, nil, nil, nil)
	issueSvc.Create(p.ID, 1, "Beta", "", models.PriorityMedium, nil, nil, nil)
	issueSvc.Create(p.ID, 1, "Gamma", "", models.PriorityLow, nil, nil, nil)

	// List all
	pid := p.ID
	issues, total, err := issueSvc.List(models.IssueFilter{ProjectID: &pid})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3 issues, got %d", total)
	}

	// Pagination
	limit := 2
	issues, total, err = issueSvc.List(models.IssueFilter{ProjectID: &pid, Limit: limit})
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

func TestAgentRegisterAndLogin(t *testing.T) {
	db, _ := server.NewDB(&config.Config{DBDriver: "sqlite3", DBDSN: "file::memory:"})
	server.AutoMigrate(db)
	agentRepo := gormrepo.NewAgentRepo(db)
	bus := events.NewBus()
	agentSvc := service.NewAgentService(agentRepo, bus, nil, true)

	_, err := agentSvc.Register("bot", models.AgentKindAI, "bot-1", "mypass", []string{"CODING"}, "", "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Login with correct password
	agent, err := agentSvc.Login("bot-1", "mypass")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if agent.Agent.Name != "bot" {
		t.Errorf("expected 'bot', got %s", agent.Agent.Name)
	}

	// Login with wrong password
	_, err = agentSvc.Login("bot-1", "wrongpass")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

// ─── Workflow config tests ───────────────────────────────────────

func TestTransitionIssue_CreatorRestricted(t *testing.T) {
	issueSvc, agentSvc, projectSvc, workflowSvc := setupIssueTest(t)
	p, _ := projectSvc.Create("Test", "")

	creator, _ := agentSvc.Register("creator", models.AgentKindHuman, "cr-1", "secret", nil, "", "")
	projectSvc.AddMember(p.ID, creator.ID, models.ProjectRoleMember)

	ac := false
	projectSvc.UpdateConfig(p.ID, &ac, nil)

	issue, _ := issueSvc.Create(p.ID, creator.ID, "Test", "", models.PriorityMedium, nil, nil, nil)

	// Creator is a regular member, not owner/maintainer, not an assignee — should be denied
	_, err := workflowSvc.Transition(issue.ID, models.IssueStateInProgress, creator.ID, nil)
	if err == nil {
		t.Fatal("expected error: creator should not be allowed to transition when AllowCreatorTransition=false")
	}
}

func TestTransitionIssue_CreatorRestricted_AsOwner(t *testing.T) {
	issueSvc, agentSvc, projectSvc, workflowSvc := setupIssueTest(t)
	p, _ := projectSvc.Create("Test", "")

	creator, _ := agentSvc.Register("creator", models.AgentKindHuman, "co-1", "secret", nil, "", "")
	projectSvc.AddMember(p.ID, creator.ID, models.ProjectRoleOwner)

	ac := false
	projectSvc.UpdateConfig(p.ID, &ac, nil)

	issue, _ := issueSvc.Create(p.ID, creator.ID, "Test", "", models.PriorityMedium, nil, nil, nil)

	// Creator is owner — should be allowed
	issue, err := workflowSvc.Transition(issue.ID, models.IssueStateInProgress, creator.ID, nil)
	if err != nil {
		t.Fatalf("owner should be allowed to transition: %v", err)
	}
	if issue.State != models.IssueStateInProgress {
		t.Errorf("expected in_progress, got %s", issue.State)
	}
}

func TestTransitionIssue_CreatorRestricted_AsMaintainer(t *testing.T) {
	issueSvc, agentSvc, projectSvc, workflowSvc := setupIssueTest(t)
	p, _ := projectSvc.Create("Test", "")

	creator, _ := agentSvc.Register("creator", models.AgentKindHuman, "cma-1", "secret", nil, "", "")
	projectSvc.AddMember(p.ID, creator.ID, models.ProjectRoleMaintainer)

	ac := false
	projectSvc.UpdateConfig(p.ID, &ac, nil)

	issue, _ := issueSvc.Create(p.ID, creator.ID, "Test", "", models.PriorityMedium, nil, nil, nil)

	issue, err := workflowSvc.Transition(issue.ID, models.IssueStateInProgress, creator.ID, nil)
	if err != nil {
		t.Fatalf("maintainer should be allowed to transition: %v", err)
	}
	if issue.State != models.IssueStateInProgress {
		t.Errorf("expected in_progress, got %s", issue.State)
	}
}

func TestTransitionIssue_CreatorRestricted_AsAssignee(t *testing.T) {
	issueSvc, agentSvc, projectSvc, workflowSvc := setupIssueTest(t)
	p, _ := projectSvc.Create("Test", "")

	creator, _ := agentSvc.Register("creator", models.AgentKindHuman, "ca-1", "secret", nil, "", "")
	projectSvc.AddMember(p.ID, creator.ID, models.ProjectRoleMember)

	ac := false
	projectSvc.UpdateConfig(p.ID, &ac, nil)

	// Creator is also an assignee of the issue
	issue, _ := issueSvc.Create(p.ID, creator.ID, "Test", "", models.PriorityMedium, []uint{creator.ID}, nil, nil)

	issue, err := workflowSvc.Transition(issue.ID, models.IssueStateInProgress, creator.ID, nil)
	if err != nil {
		t.Fatalf("creator who is an assignee should be allowed to transition: %v", err)
	}
	if issue.State != models.IssueStateInProgress {
		t.Errorf("expected in_progress, got %s", issue.State)
	}
}

func TestTransitionIssue_RequireCreatorCloseApproval(t *testing.T) {
	issueSvc, agentSvc, projectSvc, workflowSvc := setupIssueTest(t)
	p, _ := projectSvc.Create("Test", "")

	creator, _ := agentSvc.Register("creator", models.AgentKindHuman, "ca-close", "secret", nil, "", "")
	projectSvc.AddMember(p.ID, creator.ID, models.ProjectRoleMember)
	other, _ := agentSvc.Register("other", models.AgentKindHuman, "other-close", "secret", nil, "", "")

	rc := true
	projectSvc.UpdateConfig(p.ID, nil, &rc)

	issue, _ := issueSvc.Create(p.ID, creator.ID, "Test", "", models.PriorityMedium, nil, nil, nil)

	// Other agent transitions: open -> in_progress -> review
	issue, err := workflowSvc.Transition(issue.ID, models.IssueStateInProgress, other.ID, nil)
	if err != nil {
		t.Fatalf("other agent should be able to transition to in_progress: %v", err)
	}
	issue, err = workflowSvc.Transition(issue.ID, models.IssueStateReview, other.ID, nil)
	if err != nil {
		t.Fatalf("other agent should be able to transition to review: %v", err)
	}

	// Other agent tries to close — should be blocked
	_, err = workflowSvc.Transition(issue.ID, models.IssueStateClosedCompleted, other.ID, nil)
	if err == nil {
		t.Fatal("expected error: non-creator should not close when RequireCreatorCloseApproval=true")
	}

	// Creator can still close
	issue, err = workflowSvc.Transition(issue.ID, models.IssueStateClosedCompleted, creator.ID, nil)
	if err != nil {
		t.Fatalf("creator should be allowed to close: %v", err)
	}
	if issue.State != models.IssueStateClosedCompleted {
		t.Errorf("expected closed_completed, got %s", issue.State)
	}
}

func TestHeartbeat(t *testing.T) {
	db, _ := server.NewDB(&config.Config{DBDriver: "sqlite3", DBDSN: "file::memory:"})
	server.AutoMigrate(db)
	agentRepo := gormrepo.NewAgentRepo(db)
	bus := events.NewBus()
	agentSvc := service.NewAgentService(agentRepo, bus, nil, true)

	agent, _ := agentSvc.Register("bot", models.AgentKindAI, "bot-hb", "pass", nil, "", "")
	if agent.LastSeenAt != nil {
		t.Logf("last_seen_at after register: %v", agent.LastSeenAt)
	}

	// Heartbeat
	err := agentSvc.Heartbeat(agent.ID)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	// Verify last_seen_at was updated
	updated, _ := agentSvc.GetByID(agent.ID)
	if updated.LastSeenAt == nil {
		t.Error("expected last_seen_at to be set after heartbeat")
	}
}
