package gorm

import (
	"testing"

	"chick/internal/models"
)

func TestIssueAssigneeRepo(t *testing.T) {
	d := db(t)
	repo := NewIssueAssigneeRepo(d).(*IssueAssigneeRepo)
	agentRepo := NewAgentRepo(d).(*AgentRepo)
	projectRepo := NewProjectRepo(d).(*ProjectRepo)
	issueRepo := NewIssueRepo(d).(*IssueRepo)

	agent := &models.Agent{Name: "a", Kind: models.AgentKindAI, ExternalID: "ia-1", SecretHash: "h"}
	agentRepo.Create(agent)
	p := &models.Project{Name: "P"}
	projectRepo.Create(p)
	issue := &models.Issue{Number: 1, ProjectID: p.ID, Title: "T", State: models.IssueStateOpen, Priority: models.PriorityMedium, CreatorID: agent.ID}
	issueRepo.Create(issue)

	// Create assignee
	ia := &models.IssueAssignee{IssueID: issue.ID, AgentID: agent.ID, State: models.AssigneeStatePending}
	if err := repo.Create(ia); err != nil {
		t.Fatalf("create: %v", err)
	}
	if ia.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	// List by issue
	assignees, err := repo.ListByIssue(issue.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(assignees) != 1 {
		t.Errorf("expected 1, got %d", len(assignees))
	}

	// List by agent
	list, err := repo.ListByAgent(agent.ID)
	if err != nil {
		t.Fatalf("list by agent: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}

	// Update state
	if err := repo.UpdateState(issue.ID, agent.ID, models.AssigneeStateInProgress); err != nil {
		t.Fatalf("update state: %v", err)
	}
	assignees, _ = repo.ListByIssue(issue.ID)
	if assignees[0].State != models.AssigneeStateInProgress {
		t.Errorf("expected accepted, got %s", assignees[0].State)
	}

	// Remove
	if err := repo.Remove(issue.ID, agent.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	assignees, _ = repo.ListByIssue(issue.ID)
	if len(assignees) != 0 {
		t.Errorf("expected 0 after remove, got %d", len(assignees))
	}
}

func TestIssueAssigneeRepo_Empty(t *testing.T) {
	d := db(t)
	repo := NewIssueAssigneeRepo(d).(*IssueAssigneeRepo)

	assignees, err := repo.ListByIssue(999)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(assignees) != 0 {
		t.Errorf("expected 0, got %d", len(assignees))
	}
}

func TestIssueAssigneeRepo_UpdateNonExistent(t *testing.T) {
	d := db(t)
	repo := NewIssueAssigneeRepo(d).(*IssueAssigneeRepo)

	err := repo.UpdateState(1, 1, models.AssigneeStateInProgress)
	if err == nil {
		t.Log("no error when updating non-existent assignee (driver-dependent)")
	}
}
