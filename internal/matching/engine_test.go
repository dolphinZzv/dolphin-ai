package matching_test

import (
	"testing"

	"chick/internal/config"
	"chick/internal/events"
	"chick/internal/matching"
	"chick/internal/models"
	gormrepo "chick/internal/repository/gorm"
	"chick/internal/server"

	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := server.NewDB(&config.Config{DBDriver: "sqlite3", DBDSN: "file::memory:"})
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	if err := server.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestMatchByCapability(t *testing.T) {
	db := setupTestDB(t)
	bus := events.NewBus()

	agentRepo := gormrepo.NewAgentRepo(db)
	labelRepo := gormrepo.NewLabelRepo(db)
	assigneeRepo := gormrepo.NewIssueAssigneeRepo(db)
	issueRepo := gormrepo.NewIssueRepo(db)
	projectRepo := gormrepo.NewProjectRepo(db)
	memberRepo := gormrepo.NewProjectMemberRepo(db)

	// Create project
	project := &models.Project{Name: "Test"}
	if err := projectRepo.Create(project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Create agent with CODING capability
	agent := &models.Agent{
		Name:         "coder",
		Kind:         models.AgentKindAI,
		Status:       models.AgentStatusOnline,
		ExternalID:   "coder-1",
		Capabilities: models.StringSlice{"CODING"},
	}
	if err := agentRepo.Create(agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Add agent as project member
	if err := memberRepo.Add(&models.ProjectMember{
		ProjectID: project.ID,
		AgentID:   agent.ID,
		Role:      models.ProjectRoleMember,
	}); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// Create label with CODING capability
	label := &models.Label{
		ProjectID:  project.ID,
		Name:       "bug",
		Capability: models.CapCoding,
	}
	if err := labelRepo.Create(label); err != nil {
		t.Fatalf("create label: %v", err)
	}

	// Create matching engine and subscribe
	engine := matching.NewEngine(agentRepo, labelRepo, assigneeRepo, issueRepo)
	engine.Subscribe(bus)

	// Publish event matching what IssueService.Create would publish
	bus.PublishSync(events.Event{
		Type: events.EventIssueCreated,
		Payload: map[string]interface{}{
			"issueID":   uint(1),
			"projectID": project.ID,
			"creatorID": uint(1),
			"labelIDs":  []uint{label.ID},
		},
	})

	// Verify assignment was created
	assignments, err := assigneeRepo.ListByIssue(1)
	if err != nil {
		t.Fatalf("list assignments: %v", err)
	}
	if len(assignments) == 0 {
		t.Fatal("expected at least one assignment from matching engine")
	}
	found := false
	for _, a := range assignments {
		if a.AgentID == agent.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected agent %d to be assigned to issue 1", agent.ID)
	}
}
