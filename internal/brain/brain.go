package brain

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

//go:embed seed
var seedFS embed.FS

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
	if b.repo != nil {
		return true
	}
	return false
}

// Init ensures the brain directory exists and is a git repository.
// If the directory does not exist it is created. If it is not yet a git repo
// git.PlainInit is called. On first init, seed files from the embedded seed/
// directory are written and committed.
func (b *Brain) Init(ctx context.Context) error {
	if err := os.MkdirAll(b.dir, 0755); err != nil {
		return fmt.Errorf("brain: mkdir: %w", err)
	}

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

	repo, err := gogit.PlainInit(b.dir, false)
	if err != nil {
		return fmt.Errorf("brain: git init: %w", err)
	}
	b.repo = repo

	// Write .gitignore.
	gitignore := "# Brain gitignore\n*.log\n.env\ntokens\n"
	if err := os.WriteFile(filepath.Join(b.dir, ".gitignore"), []byte(gitignore), 0644); err != nil {
		return fmt.Errorf("brain: write .gitignore: %w", err)
	}

	// Write seed files from embedded filesystem.
	if err := fs.WalkDir(seedFS, "seed", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := seedFS.ReadFile(path)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, "seed/")
		full := filepath.Join(b.dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return fmt.Errorf("brain: mkdir seed %s: %w", rel, err)
		}
		if err := os.WriteFile(full, data, 0644); err != nil {
			return fmt.Errorf("brain: write seed %s: %w", rel, err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("brain: seed files: %w", err)
	}

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

// ResolveMarkDownFile reads a file with model-specific override.
// If modelName is provided, it first tries basePath with @modelName inserted
// before the .md extension. Falls back to basePath if the model-specific file
// does not exist.
func (b *Brain) ResolveMarkDownFile(ctx context.Context, basePath, modelName string) (string, error) {
	if modelName != "" {
		modelPath := insertModelSuffix(basePath, modelName)
		content, err := b.Read(ctx, modelPath)
		if err == nil {
			return content, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	return b.Read(ctx, basePath)
}

func insertModelSuffix(basePath, modelName string) string {
	ext := filepath.Ext(basePath)
	base := strings.TrimSuffix(basePath, ext)
	return base + "@" + modelName + ext
}

// Write writes content to a file in the brain directory and creates a git commit.
// summary is the commit message describing the change; if empty a default is generated.
func (b *Brain) Write(ctx context.Context, path, summary, content string) error {
	full, err := b.safePath(path)
	if err != nil {
		return err
	}

	parent := filepath.Dir(full)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("brain: mkdir %s: %w", path, err)
	}

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

// Delete removes a file from the brain directory and creates a git commit.
func (b *Brain) Delete(ctx context.Context, path string) error {
	full, err := b.safePath(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(full); os.IsNotExist(err) {
		return fmt.Errorf("brain: delete %s: %w", path, os.ErrNotExist)
	}
	if err := os.Remove(full); err != nil {
		return fmt.Errorf("brain: delete %s: %w", path, err)
	}
	return b.commitPath(path, "delete "+path)
}

// List recursively lists .md files in the brain directory relative to root.
func (b *Brain) List(ctx context.Context) ([]string, error) {
	var files []string
	err := filepath.Walk(b.dir, func(fpath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
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

// ReadIndex reads root-level concept files plus index.md in each immediate
// subdirectory. Returns concatenated content.
func (b *Brain) ReadIndex(ctx context.Context) (string, error) {
	var parts []string

	for _, name := range []string{"index.md", "workflow.md", "brain.md"} {
		if _, err := os.Stat(filepath.Join(b.dir, name)); err == nil {
			data, err := os.ReadFile(filepath.Join(b.dir, name))
			if err != nil {
				return "", fmt.Errorf("brain: read root %s: %w", name, err)
			}
			parts = append(parts, "## /"+name+"\n"+string(data))
		}
	}

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
			return fmt.Errorf("enough")
		}
		commits = append(commits, GitCommit{
			Hash:    c.Hash.String(),
			Message: c.Message,
			Author:  c.Author.Name,
			Date:    c.Author.When,
		})
		return nil
	})
	if err != nil && err.Error() == "enough" {
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("brain: log iter: %w", err)
	}
	return commits, nil
}

// AutoCommit stages all changes and commits if there are any.
// msg is used as commit message; if empty, generates one from changed file list.
func (b *Brain) AutoCommit(ctx context.Context, msg string) {
	if b.repo == nil {
		return
	}
	wt, err := b.repo.Worktree()
	if err != nil {
		return
	}
	status, err := wt.Status()
	if err != nil {
		return
	}
	if status.IsClean() {
		return
	}
	if _, err := wt.Add("."); err != nil {
		return
	}
	if msg == "" {
		msg = autoCommitMsg(status)
	}
	_, err = wt.Commit(msg, &gogit.CommitOptions{
		Author: &object.Signature{
			Name: "Dolphin Brain",
			When: time.Now(),
		},
	})
	if err != nil && err != gogit.ErrEmptyCommit {
		fmt.Fprintf(os.Stderr, "brain: auto-commit: %v\n", err)
	}
}

func autoCommitMsg(status gogit.Status) string {
	var adds, updates []string
	for path, s := range status {
		if s.Worktree == gogit.Untracked {
			adds = append(adds, path)
		} else {
			updates = append(updates, path)
		}
	}
	sort.Strings(adds)
	sort.Strings(updates)

	var parts []string
	if len(adds) > 0 {
		msg := "add"
		if len(adds) == 1 {
			msg += " " + adds[0]
		} else {
			msg += " " + adds[0] + " and " + strconv.Itoa(len(adds)-1) + " more"
		}
		parts = append(parts, msg)
	}
	if len(updates) > 0 {
		msg := "update"
		if len(updates) == 1 {
			msg += " " + updates[0]
		} else {
			msg += " " + updates[0] + " and " + strconv.Itoa(len(updates)-1) + " more"
		}
		parts = append(parts, msg)
	}
	return strings.Join(parts, "; ")
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
