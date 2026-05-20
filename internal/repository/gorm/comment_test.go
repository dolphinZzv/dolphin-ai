package gorm

import (
	"testing"

	"chick/internal/models"
)

func TestCommentRepo_CreateAndList(t *testing.T) {
	d := db(t)
	repo := NewCommentRepo(d).(*CommentRepo)
	projectRepo := NewProjectRepo(d).(*ProjectRepo)
	agentRepo := NewAgentRepo(d).(*AgentRepo)
	issueRepo := NewIssueRepo(d).(*IssueRepo)

	agent := &models.Agent{Name: "a", Kind: models.AgentKindHuman, ExternalID: "cm-1", SecretHash: "h"}
	agentRepo.Create(agent)
	p := &models.Project{Name: "P"}
	projectRepo.Create(p)
	issueRepo.Create(&models.Issue{Number: 1, ProjectID: p.ID, Title: "T", State: models.IssueStateOpen, Priority: models.PriorityMedium, CreatorID: agent.ID})

	c := &models.Comment{
		IssueID:     uintPtr(1),
		AuthorID:    agent.ID,
		Body:        "Hello",
		ContentType: models.CommentMarkdown,
	}
	if err := repo.Create(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := repo.GetByID(c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Body != "Hello" {
		t.Errorf("expected 'Hello', got %s", got.Body)
	}

	comments, err := repo.ListByIssue(1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(comments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(comments))
	}
}

func TestCommentRepo_Replies(t *testing.T) {
	d := db(t)
	repo := NewCommentRepo(d).(*CommentRepo)
	agentRepo := NewAgentRepo(d).(*AgentRepo)
	projectRepo := NewProjectRepo(d).(*ProjectRepo)
	issueRepo := NewIssueRepo(d).(*IssueRepo)

	agent := &models.Agent{Name: "a2", Kind: models.AgentKindHuman, ExternalID: "cm-2", SecretHash: "h"}
	agentRepo.Create(agent)
	p := &models.Project{Name: "P2"}
	projectRepo.Create(p)
	issueRepo.Create(&models.Issue{Number: 1, ProjectID: p.ID, Title: "T2", State: models.IssueStateOpen, Priority: models.PriorityMedium, CreatorID: agent.ID})

	parent := &models.Comment{IssueID: uintPtr(1), AuthorID: agent.ID, Body: "Parent", ContentType: models.CommentMarkdown}
	repo.Create(parent)

	reply := &models.Comment{IssueID: uintPtr(1), AuthorID: agent.ID, Body: "Reply", ContentType: models.CommentMarkdown, ParentID: &parent.ID}
	repo.Create(reply)

	replies, err := repo.ListByParent(parent.ID)
	if err != nil {
		t.Fatalf("list replies: %v", err)
	}
	if len(replies) != 1 {
		t.Errorf("expected 1 reply, got %d", len(replies))
	}
}

func TestCommentRepo_Delete(t *testing.T) {
	d := db(t)
	repo := NewCommentRepo(d).(*CommentRepo)
	agentRepo := NewAgentRepo(d).(*AgentRepo)
	projectRepo := NewProjectRepo(d).(*ProjectRepo)
	issueRepo := NewIssueRepo(d).(*IssueRepo)

	agent := &models.Agent{Name: "a3", Kind: models.AgentKindHuman, ExternalID: "cm-3", SecretHash: "h"}
	agentRepo.Create(agent)
	p := &models.Project{Name: "P3"}
	projectRepo.Create(p)
	issueRepo.Create(&models.Issue{Number: 1, ProjectID: p.ID, Title: "T3", State: models.IssueStateOpen, Priority: models.PriorityMedium, CreatorID: agent.ID})

	c := &models.Comment{IssueID: uintPtr(1), AuthorID: agent.ID, Body: "Delete me", ContentType: models.CommentMarkdown}
	repo.Create(c)

	if err := repo.Delete(c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := repo.GetByID(c.ID)
	if err == nil {
		t.Error("expected error for deleted comment")
	}
}

func uintPtr(v uint) *uint { return &v }
