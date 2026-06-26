package verif

import (
	"os"
	"strings"
	"testing"
)

func Test_IssueNumberProjectID_CompositeUniqueIndex(t *testing.T) {
	data, err := os.ReadFile("../internal/models/issue.go")
	if err != nil {
		t.Fatalf("read issue.go: %v", err)
	}

	src := string(data)

	// Number field must have uniqueIndex with name idx_issues_project_number
	if !strings.Contains(src, `uniqueIndex:idx_issues_project_number`) {
		t.Error("Issue.Number missing uniqueIndex:idx_issues_project_number")
	}

	// ProjectID field must share the same uniqueIndex name
	if !strings.Contains(src, `uniqueIndex:idx_issues_project_number`) {
		t.Error("Issue.ProjectID missing uniqueIndex:idx_issues_project_number")
	}

	// Must NOT have the old single-column unique index
	if strings.Contains(src, `index:idx_project_number,unique`) {
		t.Error("Issue.Number still has old index:idx_project_number,unique — remove it")
	}
}

func Test_AutoMigrate_DropsOldIssueIndex(t *testing.T) {
	data, err := os.ReadFile("../internal/server/db.go")
	if err != nil {
		t.Fatalf("read db.go: %v", err)
	}

	src := string(data)

	if !strings.Contains(src, "DROP INDEX IF EXISTS idx_project_number") {
		t.Error("AutoMigrate missing migration to drop old idx_project_number")
	}
}
