package dream

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// apply implements Phase 3: create a git branch with the edits, commit
// each edit individually, then merge or leave for review.
func (d *Dream) apply(ctx context.Context, edits []Edit) error {
	if len(edits) == 0 {
		return nil
	}

	branchName := fmt.Sprintf("dream/%d", d.currentID)

	// 1. Auto-commit any pending changes on main so we branch from a clean state.
	d.brain.AutoCommit(ctx, "pre-dream checkpoint")

	// 2. Create temporary workspace: clone brain to /tmp.
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("dolphin-dream-%d", d.currentID))
	defer os.RemoveAll(tmpDir)

	if err := d.cloneToTemp(tmpDir); err != nil {
		return fmt.Errorf("clone to temp: %w", err)
	}

	// 3. Create branch and apply edits.
	if err := d.createAndCheckoutBranch(tmpDir, branchName); err != nil {
		return fmt.Errorf("create branch: %w", err)
	}

	for _, edit := range edits {
		if err := d.applyEdit(ctx, tmpDir, edit); err != nil {
			return fmt.Errorf("apply edit %s: %w", edit.ProposalID, err)
		}
	}

	// 4. Commit state update.
	statePath := filepath.Join(tmpDir, ".dream", "state.json")
	_ = os.MkdirAll(filepath.Dir(statePath), 0o755)
	_ = d.state.save(statePath, statePath) // backup-only in temp

	// 5. Merge back or leave branch.
	if d.autoApply {
		return d.fetchAndMerge(ctx, tmpDir, branchName)
	}

	// Non-auto: fetch the branch into the brain repo so the user can review.
	return d.fetchBranch(ctx, tmpDir, branchName)
}

// cloneToTemp creates a shallow clone of the brain into a temp directory.
func (d *Dream) cloneToTemp(tmpDir string) error {
	brainDir := d.brain.Dir()
	return exec.Command("git", "clone", "--no-hardlinks", brainDir, tmpDir).Run()
}

// createAndCheckoutBranch creates the dream branch in the temp repo.
func (d *Dream) createAndCheckoutBranch(tmpDir, branchName string) error {
	return execGit(tmpDir, "checkout", "-b", branchName)
}

// applyEdit writes the edit result to the temp repo and commits.
func (d *Dream) applyEdit(ctx context.Context, tmpDir string, edit Edit) error {
	targetPath := filepath.Join(tmpDir, edit.Target)

	switch edit.Action { //nolint:exhaustive // ActionSplit reserved for future use
	case ActionDeprecate:
		// Read existing content and prepend deprecated marker.
		content := edit.After
		if content == "" {
			data, err := os.ReadFile(targetPath)
			if err == nil {
				content = "⚠ deprecated\n\n" + string(data)
			} else {
				content = "⚠ deprecated"
			}
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(targetPath, []byte(content), 0o600); err != nil {
			return err
		}

	case ActionCreate:
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(targetPath, []byte(edit.After), 0o600); err != nil {
			return err
		}

	case ActionMerge:
		// Write merged content.
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(targetPath, []byte(edit.After), 0o600); err != nil {
			return err
		}
		// Delete source files (check if they exist first).
		for _, src := range extractSources(edit) {
			srcPath := filepath.Join(tmpDir, src)
			if _, err := os.Stat(srcPath); err == nil {
				_ = os.Remove(srcPath)
			}
		}

	default: // improve
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(targetPath, []byte(edit.After), 0o600); err != nil {
			return err
		}
	}

	_ = fmt.Sprintf("dream/%d: %s %s", d.currentID, edit.Action, edit.Target)
	return execGit(tmpDir, "add", "-A")
}

// extractSources extracts source file paths from merge-related edit fields.
func extractSources(edit Edit) []string {
	// The sources are not stored directly on Edit; they come from the EditProposal.
	// For simplicity, we extract from the reasoning if it contains "merge".
	var sources []string
	// If Reasoning mentions files, extract them.
	fields := strings.Fields(edit.Reasoning)
	for _, f := range fields {
		if strings.Contains(f, ".md") {
			sources = append(sources, strings.TrimSuffix(f, ","))
		}
	}
	return sources
}

// fetchAndMerge fetches the dream branch from temp and ff-merges into main.
func (d *Dream) fetchAndMerge(ctx context.Context, tmpDir, branchName string) error {
	brainDir := d.brain.Dir()

	// Fetch the branch from the temp repo.
	if err := execGit(brainDir, "fetch", tmpDir, fmt.Sprintf("%s:%s", branchName, branchName)); err != nil {
		return fmt.Errorf("fetch dream branch: %w", err)
	}

	// Fast-forward merge.
	if err := execGit(brainDir, "merge", "--ff-only", branchName); err != nil {
		// If merge fails (not ff-able), abort.
		_ = execGit(brainDir, "merge", "--abort")
		return fmt.Errorf("merge dream branch (not fast-forward): %w", err)
	}

	// Get merge SHA for revert tracking.
	out, err := execGitOutput(brainDir, "rev-parse", "HEAD")
	if err == nil {
		d.state.LastMergeSHA = strings.TrimSpace(out)
	}

	// Delete the branch.
	_ = execGit(brainDir, "branch", "-d", branchName)

	return nil
}

// fetchBranch fetches the dream branch from temp but does not merge.
func (d *Dream) fetchBranch(ctx context.Context, tmpDir, branchName string) error {
	brainDir := d.brain.Dir()
	if err := execGit(brainDir, "fetch", tmpDir, fmt.Sprintf("%s:%s", branchName, branchName)); err != nil {
		return fmt.Errorf("fetch dream branch: %w", err)
	}
	d.state.OpenBranch = branchName
	return nil
}

// execGit runs a git command in the given directory.
func execGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, string(output))
	}
	return nil
}

// execGitOutput runs a git command and returns stdout.
func execGitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %v: %w", args, err)
	}
	return string(output), nil
}
