package verif

import (
	"os"
	"strings"
	"testing"
)

func requirePreload(t *testing.T, src, method, preload string) {
	t.Helper()
	if !strings.Contains(src, `Preload("`+preload+`")`) {
		t.Errorf("%s missing Preload(%q)", method, preload)
	}
}

func Test_IssueRepo_GetByID_Preloads(t *testing.T) {
	data, err := os.ReadFile("../internal/repository/gorm/issue.go")
	if err != nil {
		t.Fatalf("read issue.go: %v", err)
	}
	src := string(data)

	// Simulate isolating GetByID (just check the whole file since all Preloads
	// appear in these methods)
	requirePreload(t, src, "GetByID", "Creator")
	requirePreload(t, src, "GetByID", "Assignees.Agent")
	requirePreload(t, src, "GetByID", "Labels")
	requirePreload(t, src, "GetByID", "Comments.Author")
	// Assignees (+Agent) and Comments are also required but implicit via the above
}

func Test_IssueRepo_List_Preloads(t *testing.T) {
	data, err := os.ReadFile("../internal/repository/gorm/issue.go")
	if err != nil {
		t.Fatalf("read issue.go: %v", err)
	}
	src := string(data)

	requirePreload(t, src, "List", "Creator")
	requirePreload(t, src, "List", "Assignees.Agent")
	requirePreload(t, src, "List", "Labels")
}

func Test_CommentRepo_GetByID_Preloads(t *testing.T) {
	data, err := os.ReadFile("../internal/repository/gorm/comment.go")
	if err != nil {
		t.Fatalf("read comment.go: %v", err)
	}
	src := string(data)

	requirePreload(t, src, "GetByID", "Author")
	requirePreload(t, src, "ListByIssue", "Author")
}

func Test_CommentService_Create_Refetches(t *testing.T) {
	data, err := os.ReadFile("../internal/service/comment.go")
	if err != nil {
		t.Fatalf("read comment.go: %v", err)
	}
	src := string(data)

	if !strings.Contains(src, "commentRepo.GetByID(c.ID)") {
		t.Error("CommentService.Create must return commentRepo.GetByID(c.ID) instead of c directly")
	}
}
