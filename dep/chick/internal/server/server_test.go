package server

import (
	"testing"

	"chick/internal/config"
)

func TestNewServer(t *testing.T) {
	srv, err := New(&config.Config{
		DBDriver:  "sqlite3",
		DBDSN:     "file::memory:",
		JWTSecret: "test-secret",
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if srv.DB == nil {
		t.Fatal("expected non-nil DB")
	}
	if srv.ProjectService == nil {
		t.Fatal("expected non-nil ProjectService")
	}
	if srv.AgentService == nil {
		t.Fatal("expected non-nil AgentService")
	}
	if srv.IssueService == nil {
		t.Fatal("expected non-nil IssueService")
	}
	if srv.CommentService == nil {
		t.Fatal("expected non-nil CommentService")
	}
	if srv.WorkflowService == nil {
		t.Fatal("expected non-nil WorkflowService")
	}
	if srv.Authenticator == nil {
		t.Fatal("expected non-nil Authenticator")
	}
	if srv.NotifService == nil {
		t.Fatal("expected non-nil NotifService")
	}
	if srv.EventBus == nil {
		t.Fatal("expected non-nil EventBus")
	}
}
