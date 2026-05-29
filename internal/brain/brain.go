package brain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// GitCommit represents a single commit entry from GitLog.
type GitCommit struct {
	Hash    string    `json:"hash"`
	Message string    `json:"message"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
}

// Brain manages a git-versioned knowledge directory on the filesystem.
type Brain struct {
	dir  string
	repo *gogit.Repository
}

// New creates a Brain instance. Does not touch the filesystem.
func New(dir string) *Brain {
	return &Brain{dir: dir}
}

// Dir returns the brain root directory.
func (b *Brain) Dir() string { return b.dir }

// IsInitialized returns true if the brain directory is already a git repository.
func (b *Brain) IsInitialized() bool {
	if _, err := os.Stat(filepath.Join(b.dir, ".git", "HEAD")); err == nil {
		return true
	}
	// Also check if we have an open repo handle.
	if b.repo != nil {
		return true
	}
	return false
}

// Init ensures the brain directory exists and is a git repository.
// If the directory does not exist it is created. If it is not yet a git repo
// git.PlainInit is called. On first init, seed files (introduction.md,
// workflow.md) are written and committed.
func (b *Brain) Init(ctx context.Context) error {
	// 1. Ensure directory exists.
	if err := os.MkdirAll(b.dir, 0755); err != nil {
		return fmt.Errorf("brain: mkdir: %w", err)
	}

	// 2. Check if already a git repo.
	isRepo := false
	if _, err := os.Stat(filepath.Join(b.dir, ".git", "HEAD")); err == nil {
		isRepo = true
	}

	if isRepo {
		repo, err := gogit.PlainOpen(b.dir)
		if err != nil {
			return fmt.Errorf("brain: open repo: %w", err)
		}
		b.repo = repo
		return nil
	}

	// 3. Fresh init.
	repo, err := gogit.PlainInit(b.dir, false)
	if err != nil {
		return fmt.Errorf("brain: git init: %w", err)
	}
	b.repo = repo

	// 4. Write .gitignore.
	gitignore := "# Brain gitignore\n*.log\n.env\ntokens\n"
	if err := os.WriteFile(filepath.Join(b.dir, ".gitignore"), []byte(gitignore), 0644); err != nil {
		return fmt.Errorf("brain: write .gitignore: %w", err)
	}

	// 5. Write seed files (root-level).
	rootSeeds := map[string]string{
		"index.md": `# Brain Index

## Files

- introduction.md: Identity and purpose
- workflow.md: Standard operating workflow

## Directories

See subdirectory index.md files for details.
`,
		"introduction.md": `# Introduction

I am Dolphin, an AI assistant.

## Identity

- Name: Dolphin
- Purpose: General-purpose AI assistant

## Capabilities

- Answer questions and engage in dialogue
- Execute tools and MCP servers
- Manage skills and sessions
- Learn from interactions
`,
		"workflow.md": `# Workflow

## Interaction Flow

1. Receive user input
2. Understand intent and context
3. Formulate response or execute tools
4. Present results to user

## Guidelines

- Be concise and accurate
- Use tools when appropriate
- Learn from feedback
`,
	}
	for name, content := range rootSeeds {
		if err := os.WriteFile(filepath.Join(b.dir, name), []byte(content), 0644); err != nil {
			return fmt.Errorf("brain: write seed %s: %w", name, err)
		}
	}

	// 5b. Write seed subdirectories with index.md.
	subSeeds := map[string]string{
		"rules/index.md": `# Rules

This directory contains rule files for the AI assistant.

## Files

- (add rule files here with naming convention: *.md)
`,
		"knowledge/index.md": `# Knowledge

This directory stores domain knowledge and reference materials.

## Files

- (add knowledge files here)
`,
		"meta/index.md": `# Meta

This directory contains metadata about the brain itself and its structure.

## Files

- (add meta files here)
`,
	}
	for name, content := range subSeeds {
		fullPath := filepath.Join(b.dir, name)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("brain: mkdir seed %s: %w", name, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("brain: write seed %s: %w", name, err)
		}
	}

	// 6. First commit.
	if err := b.commitAll("chore: init brain"); err != nil {
		return fmt.Errorf("brain: initial commit: %w", err)
	}

	return nil
}

// safePath validates that the resolved path stays within the brain directory.
func (b *Brain) safePath(path string) (string, error) {
	full := filepath.Join(b.dir, path)
	cleanFull := filepath.Clean(full)
	cleanDir := filepath.Clean(b.dir)
	if !strings.HasPrefix(cleanFull, cleanDir+string(filepath.Separator)) && cleanFull != cleanDir {
		return "", fmt.Errorf("brain: path traversal denied: %s", path)
	}
	return cleanFull, nil
}

// Read reads a file from the brain directory.
func (b *Brain) Read(ctx context.Context, path string) (string, error) {
	full, err := b.safePath(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("brain: read %s: %w", path, err)
	}
	return string(data), nil
}

// Write writes content to a file in the brain directory and creates a git commit.
// summary is the commit message describing the change; if empty a default is generated.
func (b *Brain) Write(ctx context.Context, path, summary, content string) error {
	full, err := b.safePath(path)
	if err != nil {
		return err
	}

	// Ensure parent directory exists.
	parent := filepath.Dir(full)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("brain: mkdir %s: %w", path, err)
	}

	// Check if file existed before write.
	exists := true
	if _, err := os.Stat(full); os.IsNotExist(err) {
		exists = false
	}

	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		return fmt.Errorf("brain: write %s: %w", path, err)
	}

	msg := summary
	if msg == "" {
		action := "update"
		if !exists {
			action = "create"
		}
		msg = action + " " + path
	}
	return b.commitPath(path, msg)
}

// List recursively lists .md files in the brain directory relative to root.
func (b *Brain) List(ctx context.Context) ([]string, error) {
	var files []string
	err := filepath.Walk(b.dir, func(fpath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip .git directory.
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(b.dir, fpath)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("brain: list: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

// ReadIndex reads index files two levels deep: root-level index.md (or introduction.md)
// plus index.md in each immediate subdirectory. Returns concatenated content.
func (b *Brain) ReadIndex(ctx context.Context) (string, error) {
	var parts []string

	// Level 1: root index.md (concise summary, not full introduction).
	for _, name := range []string{"index.md"} {
		if _, err := os.Stat(filepath.Join(b.dir, name)); err == nil {
			data, err := os.ReadFile(filepath.Join(b.dir, name))
			if err != nil {
				return "", fmt.Errorf("brain: read root index: %w", err)
			}
			parts = append(parts, "## /"+name+"\n"+string(data))
			break
		}
	}

	// Level 2: index.md in each immediate subdirectory.
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return "", fmt.Errorf("brain: read dir: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() == ".git" {
			continue
		}
		idxPath := filepath.Join(b.dir, entry.Name(), "index.md")
		if _, err := os.Stat(idxPath); err != nil {
			continue
		}
		data, err := os.ReadFile(idxPath)
		if err != nil {
			return "", fmt.Errorf("brain: read %s/index.md: %w", entry.Name(), err)
		}
		parts = append(parts, "## "+entry.Name()+"/index.md\n"+string(data))
	}

	return strings.Join(parts, "\n---\n"), nil
}

// GitLog returns the last n commits.
func (b *Brain) GitLog(ctx context.Context, n int) ([]GitCommit, error) {
	if b.repo == nil {
		return nil, fmt.Errorf("brain: not initialized")
	}
	iter, err := b.repo.Log(&gogit.LogOptions{})
	if err != nil {
		return nil, fmt.Errorf("brain: log: %w", err)
	}
	defer iter.Close()

	var commits []GitCommit
	err = iter.ForEach(func(c *object.Commit) error {
		if len(commits) >= n {
			return fmt.Errorf("enough") // break
		}
		commits = append(commits, GitCommit{
			Hash:    c.Hash.String(),
			Message: c.Message,
			Author:  c.Author.Name,
			Date:    c.Author.When,
		})
		return nil
	})
	// "enough" is our sentinel to stop iteration.
	if err != nil && err.Error() == "enough" {
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("brain: log iter: %w", err)
	}
	return commits, nil
}

// commitAll stages all files and creates a commit.
func (b *Brain) commitAll(msg string) error {
	if b.repo == nil {
		return fmt.Errorf("brain: not initialized")
	}
	wt, err := b.repo.Worktree()
	if err != nil {
		return fmt.Errorf("brain: worktree: %w", err)
	}
	if _, err := wt.Add("."); err != nil {
		return fmt.Errorf("brain: git add: %w", err)
	}
	_, err = wt.Commit(msg, &gogit.CommitOptions{
		Author: &object.Signature{
			Name: "Dolphin Brain",
			When: time.Now(),
		},
	})
	if err != nil && err != gogit.ErrEmptyCommit {
		return fmt.Errorf("brain: commit: %w", err)
	}
	return nil
}

// commitPath stages a single file and creates a commit.
func (b *Brain) commitPath(path, msg string) error {
	if b.repo == nil {
		return fmt.Errorf("brain: not initialized")
	}
	wt, err := b.repo.Worktree()
	if err != nil {
		return fmt.Errorf("brain: worktree: %w", err)
	}
	if _, err := wt.Add(path); err != nil {
		return fmt.Errorf("brain: git add: %w", err)
	}
	_, err = wt.Commit(msg, &gogit.CommitOptions{
		Author: &object.Signature{
			Name: "Dolphin Brain",
			When: time.Now(),
		},
	})
	if err != nil && err != gogit.ErrEmptyCommit {
		return fmt.Errorf("brain: commit: %w", err)
	}
	return nil
}
