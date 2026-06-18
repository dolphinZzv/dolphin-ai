package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const frontmatterDelim = "---\n"

// AutoCommitter commits changes with a summary message.
type AutoCommitter interface {
	AutoCommit(ctx context.Context, msg string)
}

// FileStore stores each skill as a directory:
//
//	{dir}/{name}/
//	  SKILL.md        — YAML frontmatter + prompt body
//	  metadata.json   — machine-readable metadata
//	  examples/       — example files (user-managed)
//	  scripts/        — executable scripts (user-managed)
//	  resources/      — resource files (user-managed)
//	  tests/          — test files (user-managed)
//	  README.md       — usage notes (user-managed)
//	  CHANGELOG.md    — change log (user-managed)
type FileStore struct {
	dir       string
	mu        sync.RWMutex
	committer AutoCommitter
}

// SetAutoCommitter attaches a git committer for auto-committing skill changes.
func (s *FileStore) SetAutoCommitter(c AutoCommitter) {
	s.committer = c
}

func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("skill: create store dir %q: %w", dir, err)
	}
	s := &FileStore{dir: dir}
	migrateFlatFiles(dir)
	s.syncIndexLocked()
	return s, nil
}

func (s *FileStore) path(name string) string {
	return filepath.Join(s.dir, name)
}

func (s *FileStore) skillFile(name string) string {
	return filepath.Join(s.dir, name, "SKILL.md")
}

func (s *FileStore) List(ctx context.Context) ([]Skill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sk, err := readFile(s.skillFile(e.Name()))
		if err != nil {
			continue
		}
		skills = append(skills, *sk)
	}
	return skills, nil
}

func (s *FileStore) Get(ctx context.Context, name string) (*Skill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return readFile(s.skillFile(name))
}

func (s *FileStore) Save(ctx context.Context, sk Skill) error {
	if sk.Name == "" {
		return os.ErrInvalid
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.path(sk.Name)
	existing, _ := readFile(s.skillFile(sk.Name))
	verb := "add"
	if existing != nil {
		verb = "update"
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeFile(s.skillFile(sk.Name), &sk); err != nil {
		return err
	}
	writeMetaFile(filepath.Join(dir, "metadata.json"), &sk)

	s.syncIndexLocked()

	if s.committer != nil {
		s.committer.AutoCommit(ctx, "skill: "+verb+" "+sk.Name)
	}
	return nil
}

func (s *FileStore) Delete(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.RemoveAll(s.path(name)); err != nil {
		return err
	}
	s.syncIndexLocked()

	if s.committer != nil {
		s.committer.AutoCommit(ctx, "skill: delete "+name)
	}
	return nil
}

func (s *FileStore) Search(ctx context.Context, query string) ([]Skill, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(query)
	var results []Skill
	for _, sk := range all {
		if strings.Contains(strings.ToLower(sk.Name), q) ||
			strings.Contains(strings.ToLower(sk.Description), q) {
			results = append(results, sk)
		}
	}
	return results, nil
}

// syncIndexLocked writes index.md listing all skills. Caller must hold s.mu write lock.
// Also safe to call without any lock during initialization (NewFileStore).
func (s *FileStore) syncIndexLocked() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	var b strings.Builder
	b.WriteString("# Skills\n\n")
	var listed bool
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sk, err := readFile(s.skillFile(e.Name()))
		if err != nil {
			continue
		}
		if !listed {
			b.WriteString("| Name | Description | Status |\n")
			b.WriteString("|---|---|---|\n")
			listed = true
		}
		status := "enabled"
		if !sk.Enabled {
			status = "disabled"
		}
		b.WriteString("| " + sk.Name + " | " + sk.Description + " | " + status + " |\n")
	}
	if !listed {
		b.WriteString("No skills registered.\n")
	}
	os.WriteFile(filepath.Join(s.dir, "index.md"), []byte(b.String()), 0o600)
}

// ---------------------------------------------------------------------------
// Migration
// ---------------------------------------------------------------------------

// migrateFlatFiles converts flat .md skill files to the directory layout.
func migrateFlatFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "index.md" || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		skillDir := filepath.Join(dir, name)
		if _, err := os.Stat(skillDir); err == nil {
			os.Remove(filepath.Join(dir, e.Name()))
			continue
		}
		os.MkdirAll(skillDir, 0o755)
		oldPath := filepath.Join(dir, e.Name())
		newPath := filepath.Join(skillDir, "SKILL.md")
		os.Rename(oldPath, newPath)
		if sk, err := readFile(newPath); err == nil {
			writeMetaFile(filepath.Join(skillDir, "metadata.json"), sk)
		}
	}
	os.Remove(filepath.Join(dir, "index.md"))
}

// ---------------------------------------------------------------------------
// I/O helpers
// ---------------------------------------------------------------------------

func readFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	var sk Skill
	hasFM := false

	if strings.HasPrefix(content, frontmatterDelim) {
		rest, ok := strings.CutPrefix(content, frontmatterDelim)
		if ok {
			before, after, found := strings.Cut(rest, frontmatterDelim)
			if found {
				hasFM = true
				if err := yaml.Unmarshal([]byte(before), &sk); err != nil {
					return nil, err
				}
				sk.Prompt = strings.TrimSpace(after)
			}
		}
	}

	if sk.Name == "" {
		sk.Name = strings.TrimSuffix(filepath.Base(filepath.Dir(path)), ".md")
	}
	if !hasFM && sk.Name != "" {
		sk.Prompt = strings.TrimSpace(content)
	}
	if sk.Name == "" {
		return nil, os.ErrNotExist
	}
	return &sk, nil
}

func writeFile(path string, sk *Skill) error {
	prompt := sk.Prompt
	sk.Prompt = ""
	frontmatter, err := yaml.Marshal(sk)
	sk.Prompt = prompt
	if err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString(frontmatterDelim)
	sb.WriteString(string(frontmatter))
	sb.WriteString(frontmatterDelim)
	sb.WriteString(sk.Prompt)
	if !strings.HasSuffix(sk.Prompt, "\n") {
		sb.WriteString("\n")
	}

	return os.WriteFile(path, []byte(sb.String()), 0o600)
}

func writeMetaFile(path string, sk *Skill) error {
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
