package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const frontmatterDelim = "---\n"

type FileStore struct {
	dir string
	mu  sync.RWMutex
}

func NewFileStore(dir string) *FileStore {
	os.MkdirAll(dir, 0755)
	return &FileStore{dir: dir}
}

func (s *FileStore) path(name string) string {
	return filepath.Join(s.dir, name+".md")
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
		if filepath.Ext(e.Name()) != ".md" {
			continue
		}
		skill, err := readFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		skills = append(skills, *skill)
	}
	return skills, nil
}

func (s *FileStore) Get(ctx context.Context, name string) (*Skill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return readFile(s.path(name))
}

func (s *FileStore) Save(ctx context.Context, skill Skill) error {
	if skill.Name == "" {
		return os.ErrInvalid
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return writeFile(s.path(skill.Name), &skill)
}

func (s *FileStore) Delete(ctx context.Context, name string) error {
	return os.Remove(s.path(name))
}

func (s *FileStore) Search(ctx context.Context, query string) ([]Skill, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(query)
	var results []Skill
	for _, skill := range all {
		if strings.Contains(strings.ToLower(skill.Name), q) ||
			strings.Contains(strings.ToLower(skill.Description), q) {
			results = append(results, skill)
		}
	}
	return results, nil
}

func readFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	var skill Skill
	hasFM := false

	if strings.HasPrefix(content, frontmatterDelim) {
		rest, ok := strings.CutPrefix(content, frontmatterDelim)
		if ok {
			before, after, found := strings.Cut(rest, frontmatterDelim)
			if found {
				hasFM = true
				if err := yaml.Unmarshal([]byte(before), &skill); err != nil {
					return nil, err
				}
				skill.Prompt = strings.TrimSpace(after)
			}
		}
	}

	if skill.Name == "" {
		skill.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if !hasFM && skill.Name != "" {
		skill.Prompt = strings.TrimSpace(content)
	}
	if skill.Name == "" {
		return nil, os.ErrNotExist
	}
	return &skill, nil
}

func writeFile(path string, skill *Skill) error {
	frontmatter, err := yaml.Marshal(skill)
	if err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString(frontmatterDelim)
	sb.WriteString(string(frontmatter))
	sb.WriteString(frontmatterDelim)
	sb.WriteString(skill.Prompt)
	if !strings.HasSuffix(skill.Prompt, "\n") {
		sb.WriteString("\n")
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}
