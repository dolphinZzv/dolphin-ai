package gorm

import (
	"testing"

	"chick/internal/models"
)

func TestProjectRepo_CreateAndGet(t *testing.T) {
	repo := NewProjectRepo(db(t)).(*ProjectRepo)

	p := &models.Project{Name: "Test Project", Description: "A test"}
	if err := repo.Create(p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := repo.GetByID(p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Test Project" {
		t.Errorf("expected 'Test Project', got %s", got.Name)
	}
}

func TestProjectRepo_List(t *testing.T) {
	repo := NewProjectRepo(db(t)).(*ProjectRepo)

	repo.Create(&models.Project{Name: "P1"})
	repo.Create(&models.Project{Name: "P2"})

	projects, err := repo.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("expected 2, got %d", len(projects))
	}
}

func TestProjectRepo_UpdateConfig(t *testing.T) {
	repo := NewProjectRepo(db(t)).(*ProjectRepo)

	p := &models.Project{Name: "Test"}
	if err := repo.Create(p); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Update config fields
	err := repo.Update(p.ID, map[string]interface{}{
		"allow_creator_transition":       false,
		"require_creator_close_approval": true,
	})
	if err != nil {
		t.Fatalf("update config: %v", err)
	}

	got, err := repo.GetByID(p.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.AllowCreatorTransition != false {
		t.Errorf("expected AllowCreatorTransition=false, got %v", got.AllowCreatorTransition)
	}
	if got.RequireCreatorCloseApproval != true {
		t.Errorf("expected RequireCreatorCloseApproval=true, got %v", got.RequireCreatorCloseApproval)
	}
}

func TestProjectRepo_Delete(t *testing.T) {
	repo := NewProjectRepo(db(t)).(*ProjectRepo)

	p := &models.Project{Name: "To Delete"}
	repo.Create(p)

	if err := repo.Delete(p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := repo.GetByID(p.ID)
	if err == nil {
		t.Error("expected error for deleted project")
	}
}
