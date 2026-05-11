package gorm

import (
	"testing"

	"chick/internal/models"

	"gorm.io/gorm"
)

func TestIssueRepo_CreateAndGet(t *testing.T) {
	d := db(t)
	repo := NewIssueRepo(d).(*IssueRepo)
	projectRepo := NewProjectRepo(d).(*ProjectRepo)
	agentRepo := NewAgentRepo(d).(*AgentRepo)

	agent := &models.Agent{Name: "creator", Kind: models.AgentKindHuman, ExternalID: "i-1", SecretHash: "h"}
	agentRepo.Create(agent)

	p := &models.Project{Name: "Issue Project"}
	projectRepo.Create(p)

	issue := &models.Issue{
		Number:    1,
		ProjectID: p.ID,
		Title:     "Test Issue",
		State:     models.IssueStateOpen,
		Priority:  models.PriorityHigh,
		CreatorID: agent.ID,
	}
	if err := repo.Create(issue); err != nil {
		t.Fatalf("create: %v", err)
	}
	if issue.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := repo.GetByID(issue.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Test Issue" {
		t.Errorf("expected 'Test Issue', got %s", got.Title)
	}
	if got.Number != 1 {
		t.Errorf("expected number 1, got %d", got.Number)
	}

	got2, err := repo.GetByNumber(p.ID, 1)
	if err != nil {
		t.Fatalf("get by number: %v", err)
	}
	if got2.ID != issue.ID {
		t.Errorf("expected issue %d, got %d", issue.ID, got2.ID)
	}
}

func TestIssueRepo_ListWithFilter(t *testing.T) {
	d := db(t)
	repo := NewIssueRepo(d).(*IssueRepo)
	projectRepo := NewProjectRepo(d).(*ProjectRepo)
	agentRepo := NewAgentRepo(d).(*AgentRepo)

	agent := &models.Agent{Name: "creator", Kind: models.AgentKindHuman, ExternalID: "i-2", SecretHash: "h"}
	agentRepo.Create(agent)

	p := &models.Project{Name: "Filter Project"}
	projectRepo.Create(p)

	repo.Create(&models.Issue{Number: 1, ProjectID: p.ID, Title: "A", State: models.IssueStateOpen, Priority: models.PriorityHigh, CreatorID: agent.ID})
	repo.Create(&models.Issue{Number: 2, ProjectID: p.ID, Title: "B", State: models.IssueStateOpen, Priority: models.PriorityMedium, CreatorID: agent.ID})
	repo.Create(&models.Issue{Number: 3, ProjectID: p.ID, Title: "C", State: models.IssueStateInProgress, Priority: models.PriorityLow, CreatorID: agent.ID})

	// Filter by project
	issues, total, err := repo.List(models.IssueFilter{ProjectID: &p.ID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3, got %d", total)
	}
	if len(issues) != 3 {
		t.Errorf("expected 3 issues, got %d", len(issues))
	}

	// Filter by multiple states
	issues, total, err = repo.List(models.IssueFilter{
		ProjectID: &p.ID,
		State:     []models.IssueState{models.IssueStateOpen, models.IssueStateInProgress},
	})
	if err != nil {
		t.Fatalf("list with states: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3 with state filter, got %d", total)
	}

	// Filter by priority
	high := models.PriorityHigh
	issues, total, err = repo.List(models.IssueFilter{ProjectID: &p.ID, Priority: &high})
	if err != nil {
		t.Fatalf("list high: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 high, got %d", total)
	}

	// Pagination
	limit := 2
	issues, total, err = repo.List(models.IssueFilter{ProjectID: &p.ID, Limit: limit})
	if err != nil {
		t.Fatalf("list limited: %v", err)
	}
	if len(issues) != 2 {
		t.Errorf("expected 2, got %d", len(issues))
	}
	if total != 3 {
		t.Errorf("expected total 3, got %d", total)
	}
}

func TestIssueRepo_UpdateState(t *testing.T) {
	d := db(t)
	repo := NewIssueRepo(d).(*IssueRepo)
	projectRepo := NewProjectRepo(d).(*ProjectRepo)
	agentRepo := NewAgentRepo(d).(*AgentRepo)

	agent := &models.Agent{Name: "c", Kind: models.AgentKindHuman, ExternalID: "i-3", SecretHash: "h"}
	agentRepo.Create(agent)
	p := &models.Project{Name: "Update"}
	projectRepo.Create(p)
	repo.Create(&models.Issue{Number: 1, ProjectID: p.ID, Title: "U", State: models.IssueStateOpen, Priority: models.PriorityMedium, CreatorID: agent.ID})

	if err := repo.UpdateState(1, models.IssueStateInProgress); err != nil {
		t.Fatalf("update state: %v", err)
	}

	got, _ := repo.GetByID(1)
	if got.State != models.IssueStateInProgress {
		t.Errorf("expected in_progress, got %s", got.State)
	}
}

func TestIssueRepo_Update(t *testing.T) {
	d := db(t)
	repo := NewIssueRepo(d).(*IssueRepo)
	projectRepo := NewProjectRepo(d).(*ProjectRepo)
	agentRepo := NewAgentRepo(d).(*AgentRepo)

	agent := &models.Agent{Name: "c2", Kind: models.AgentKindHuman, ExternalID: "i-4", SecretHash: "h"}
	agentRepo.Create(agent)
	p := &models.Project{Name: "Update2"}
	projectRepo.Create(p)
	issue := &models.Issue{Number: 1, ProjectID: p.ID, Title: "Original", State: models.IssueStateOpen, Priority: models.PriorityMedium, CreatorID: agent.ID}
	repo.Create(issue)

	if err := repo.Update(issue.ID, map[string]interface{}{"title": "Updated"}); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := repo.GetByID(issue.ID)
	if got.Title != "Updated" {
		t.Errorf("expected 'Updated', got %s", got.Title)
	}
}

func TestIssueRepo_NextNumber(t *testing.T) {
	d := db(t)
	repo := NewIssueRepo(d).(*IssueRepo)
	projectRepo := NewProjectRepo(d).(*ProjectRepo)
	agentRepo := NewAgentRepo(d).(*AgentRepo)

	agent := &models.Agent{Name: "c3", Kind: models.AgentKindHuman, ExternalID: "i-5", SecretHash: "h"}
	agentRepo.Create(agent)
	p1 := &models.Project{Name: "P1"}
	projectRepo.Create(p1)
	p2 := &models.Project{Name: "P2"}
	projectRepo.Create(p2)

	n1, err := repo.NextNumber(p1.ID)
	if err != nil {
		t.Fatalf("next number: %v", err)
	}
	if n1 != 1 {
		t.Errorf("expected 1, got %d", n1)
	}

	repo.Create(&models.Issue{Number: 1, ProjectID: p1.ID, Title: "First", State: models.IssueStateOpen, Priority: models.PriorityMedium, CreatorID: agent.ID})

	n2, err := repo.NextNumber(p1.ID)
	if err != nil {
		t.Fatalf("next number: %v", err)
	}
	if n2 != 2 {
		t.Errorf("expected 2, got %d", n2)
	}

	// Different project should also start at 1
	n3, err := repo.NextNumber(p2.ID)
	if err != nil {
		t.Fatalf("next number p2: %v", err)
	}
	if n3 != 1 {
		t.Errorf("expected 1 for p2, got %d", n3)
	}
}

func TestIssueRepo_Transaction(t *testing.T) {
	d := db(t)
	repo := NewIssueRepo(d).(*IssueRepo)

	called := false
	err := repo.Transaction(func(tx *gorm.DB) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}
	if !called {
		t.Error("expected callback to be called")
	}
}
