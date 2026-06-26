package gorm

import (
	"testing"

	"chick/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func db(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	d.AutoMigrate(&models.Project{}, &models.ProjectMember{}, &models.Agent{}, &models.Issue{},
		&models.IssueAssignee{}, &models.Comment{}, &models.Label{}, &models.Milestone{},
		&models.TimelineEvent{}, &models.Feedback{})
	return d
}

func TestAgentRepo_CreateAndGet(t *testing.T) {
	repo := NewAgentRepo(db(t)).(*AgentRepo)

	a := &models.Agent{
		Name:         "test-agent",
		Kind:         models.AgentKindAI,
		Status:       models.AgentStatusOnline,
		ExternalID:   "ext-123",
		SecretHash:   "hash",
		Capabilities: models.StringSlice{"CODING", "TESTING"},
	}
	if err := repo.Create(a); err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := repo.GetByID(a.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Name != "test-agent" {
		t.Errorf("expected 'test-agent', got %s", got.Name)
	}

	got2, err := repo.GetByExternalID("ext-123")
	if err != nil {
		t.Fatalf("get by external id: %v", err)
	}
	if got2.Name != "test-agent" {
		t.Errorf("expected 'test-agent', got %s", got2.Name)
	}
}

func TestAgentRepo_UpdateStatus(t *testing.T) {
	repo := NewAgentRepo(db(t)).(*AgentRepo)

	a := &models.Agent{Name: "agent", Kind: models.AgentKindAI, ExternalID: "ext-u", SecretHash: "h"}
	repo.Create(a)

	if err := repo.UpdateStatus(a.ID, models.AgentStatusBusy); err != nil {
		t.Fatalf("update status: %v", err)
	}

	got, _ := repo.GetByID(a.ID)
	if got.Status != models.AgentStatusBusy {
		t.Errorf("expected busy, got %s", got.Status)
	}
}

func TestAgentRepo_UpdateLastSeen(t *testing.T) {
	repo := NewAgentRepo(db(t)).(*AgentRepo)

	a := &models.Agent{Name: "agent", Kind: models.AgentKindAI, ExternalID: "ext-ls", SecretHash: "h"}
	repo.Create(a)

	if err := repo.UpdateLastSeen(a.ID); err != nil {
		t.Fatalf("update last seen: %v", err)
	}

	got, _ := repo.GetByID(a.ID)
	if got.LastSeenAt == nil {
		t.Error("expected last_seen_at to be set")
	}
}

func TestAgentRepo_List(t *testing.T) {
	repo := NewAgentRepo(db(t)).(*AgentRepo)

	repo.Create(&models.Agent{Name: "a1", Kind: models.AgentKindAI, ExternalID: "e1", SecretHash: "h"})
	repo.Create(&models.Agent{Name: "a2", Kind: models.AgentKindHuman, ExternalID: "e2", SecretHash: "h"})

	agents, err := repo.List(models.AgentFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("expected 2, got %d", len(agents))
	}

	kind := models.AgentKindAI
	filtered, err := repo.List(models.AgentFilter{Kind: &kind})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(filtered) != 1 {
		t.Errorf("expected 1 AI agent, got %d", len(filtered))
	}
}

func TestAgentRepo_CountByKind(t *testing.T) {
	repo := NewAgentRepo(db(t)).(*AgentRepo)

	repo.Create(&models.Agent{Name: "a1", Kind: models.AgentKindAI, ExternalID: "c1", SecretHash: "h"})
	repo.Create(&models.Agent{Name: "a2", Kind: models.AgentKindAI, ExternalID: "c2", SecretHash: "h"})
	repo.Create(&models.Agent{Name: "a3", Kind: models.AgentKindHuman, ExternalID: "c3", SecretHash: "h"})

	count, err := repo.CountByKind(models.AgentKindAI)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 AI agents, got %d", count)
	}

	count, err = repo.CountByKind(models.AgentKindHuman)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 human, got %d", count)
	}
}
