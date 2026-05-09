package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillFile_Basic(t *testing.T) {
	data := []byte(`---
name: test-skill
description: A test skill
---

# Content here`)
	skill := parseSkillFile(data, "test-skill.md")
	if skill == nil {
		t.Fatal("expected non-nil skill")
	}
	if skill.Name != "test-skill" {
		t.Errorf("name = %q, want test-skill", skill.Name)
	}
	if skill.Description != "A test skill" {
		t.Errorf("description = %q", skill.Description)
	}
	if skill.Content != "# Content here" {
		t.Errorf("content = %q", skill.Content)
	}
}

func TestParseSkillFile_NoFrontmatter(t *testing.T) {
	data := []byte("# Just content\n\nNo frontmatter here.")
	skill := parseSkillFile(data, "simple.md")
	if skill == nil {
		t.Fatal("expected non-nil skill")
	}
	if skill.Name != "simple" {
		t.Errorf("name = %q, want simple", skill.Name)
	}
	if skill.Description != "" {
		t.Errorf("expected empty description, got %q", skill.Description)
	}
	if skill.Content != "# Just content\n\nNo frontmatter here." {
		t.Errorf("content mismatch")
	}
}

func TestParseSkillFile_Empty(t *testing.T) {
	skill := parseSkillFile([]byte(""), "empty.md")
	if skill != nil {
		t.Error("expected nil for empty content")
	}
}

func TestManager_LoadAndGet(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nname: skill-a\ndescription: First skill\n---\n# A"), 0644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nname: skill-b\ndescription: Second skill\n---\n# B"), 0644)

	m := NewManager(dir)
	if err := m.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}

	s, ok := m.Get("skill-a")
	if !ok {
		t.Fatal("expected to find skill-a")
	}
	if s.Description != "First skill" {
		t.Errorf("description = %q", s.Description)
	}
}

func TestManager_List(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nname: skill-b\ndescription: B\n---\n# B"), 0644)
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nname: skill-a\ndescription: A\n---\n# A"), 0644)

	m := NewManager(dir)
	m.Load()
	list := m.List()
	if len(list) != 2 {
		t.Fatalf("got %d skills, want 2", len(list))
	}
	// Should be sorted by name
	if list[0].Name != "skill-a" || list[1].Name != "skill-b" {
		t.Errorf("expected sorted order, got %s, %s", list[0].Name, list[1].Name)
	}
}

func TestManager_TopSkills(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nname: skill-a\ndescription: A\n---\n# A"), 0644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nname: skill-b\ndescription: B\n---\n# B"), 0644)

	m := NewManager(dir)
	m.Load()

	// Record usage on skill-b twice, skill-a once
	m.RecordUsage("skill-b")
	m.RecordUsage("skill-b")
	m.RecordUsage("skill-a")

	top := m.TopSkills(2)
	if len(top) != 2 {
		t.Fatalf("got %d, want 2", len(top))
	}
	// skill-b should be first (most used)
	if top[0].Name != "skill-b" || top[1].Name != "skill-a" {
		t.Errorf("expected b first, got %s, %s", top[0].Name, top[1].Name)
	}
	if top[0].CallCount != 2 {
		t.Errorf("skill-b call count = %d, want 2", top[0].CallCount)
	}
}

func TestManager_TopSkillsLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		name := string(rune('a' + i))
		os.WriteFile(filepath.Join(dir, name+".md"), []byte("---\nname: skill-"+name+"\ndescription: "+name+"\n---\n#"), 0644)
	}

	m := NewManager(dir)
	m.Load()
	top := m.TopSkills(3)
	if len(top) != 3 {
		t.Errorf("got %d, want 3", len(top))
	}
}

func TestManager_Search(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "review.md"), []byte("---\nname: code-review\ndescription: Review code for quality\n---\n#"), 0644)
	os.WriteFile(filepath.Join(dir, "data.md"), []byte("---\nname: data-analysis\ndescription: Analyze datasets\n---\n#"), 0644)
	os.WriteFile(filepath.Join(dir, "security.md"), []byte("---\nname: security-audit\ndescription: Security audit of code\n---\n#"), 0644)

	m := NewManager(dir)
	m.Load()

	results := m.Search("security")
	if len(results) != 1 { // only "security-audit" has "security" in name
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "security-audit" {
		t.Errorf("expected security-audit, got %s", results[0].Name)
	}

	results = m.Search("data")
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'data', got %d", len(results))
	}
}

func TestManager_RecordUsage(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.md"), []byte("---\nname: test-skill\ndescription: Test\n---\n# T"), 0644)

	m := NewManager(dir)
	m.Load()

	m.RecordUsage("test-skill")
	m.RecordUsage("test-skill")

	s, _ := m.Get("test-skill")
	if s.CallCount != 2 {
		t.Errorf("call count = %d, want 2", s.CallCount)
	}
	if s.LastCalled.IsZero() {
		t.Error("expected LastCalled to be set")
	}
}

func TestManager_LoadNonExistentDir(t *testing.T) {
	m := NewManager("/nonexistent/path")
	err := m.Load()
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestManager_DefaultDir(t *testing.T) {
	m := NewManager("")
	if m.Dir() != ".dolphinzZ/skills" {
		t.Errorf("default dir = %q", m.Dir())
	}
}

func TestManager_SkipsNonMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.md"), []byte("---\nname: valid\ndescription: A skill\n---\n# Content"), 0644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a skill"), 0644)
	os.WriteFile(filepath.Join(dir, "subdir"), []byte("not a dir"), 0644)

	m := NewManager(dir)
	m.Load()
	if len(m.List()) != 1 {
		t.Errorf("expected 1 skill, got %d", len(m.List()))
	}
}

func TestParseSkillFile_QuotedValues(t *testing.T) {
	data := []byte(`---
name: "test-skill"
description: "A skill with quoted values"
---
# Content`)
	skill := parseSkillFile(data, "test.md")
	if skill == nil {
		t.Fatal("expected non-nil skill")
	}
	if skill.Name != "test-skill" {
		t.Errorf("name = %q", skill.Name)
	}
	if skill.Description != "A skill with quoted values" {
		t.Errorf("description = %q", skill.Description)
	}
}
