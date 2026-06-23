package context

import (
	"os"
	"path/filepath"
	"testing"
)

// The embedded workflow.schema.json must match the canonical copy at the
// repository root. This test fails if one is updated without the other,
// preventing drift between what the IDE loads via $schema in .workflow.yaml
// and the embedded copy used by the context package.
func TestEmbeddedWorkflowSchemaMatchesRepoRoot(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	rootSchema := filepath.Join(repoRoot, "workflow.schema.json")

	got, err := os.ReadFile(rootSchema)
	if err != nil {
		t.Fatalf("read repo-root schema: %v", err)
	}
	if string(got) != string(workflowSchemaJSON) {
		t.Fatalf("repo-root workflow.schema.json differs from embedded internal/context/workflow.schema.json — sync both files")
	}
}
