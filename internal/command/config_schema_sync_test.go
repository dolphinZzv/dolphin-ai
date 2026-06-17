package command

import (
	"os"
	"path/filepath"
	"testing"
)

// The embedded config.schema.json must match the canonical copy at the
// repository root. This test fails if one is updated without the other,
// preventing drift between what `config init` writes and what the IDE
// loads via the $schema line in config.yaml.
func TestEmbeddedSchemaMatchesRepoRoot(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	rootSchema := filepath.Join(repoRoot, "config.schema.json")

	got, err := os.ReadFile(rootSchema)
	if err != nil {
		t.Fatalf("read repo-root schema: %v", err)
	}
	if string(got) != string(defaultConfigSchema) {
		t.Fatalf("repo-root config.schema.json differs from embedded internal/command/config.schema.json — sync both files")
	}
}
