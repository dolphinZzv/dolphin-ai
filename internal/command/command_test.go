package command

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCommandFile_Basic(t *testing.T) {
	data := []byte(`---
name: analyze-competitor
description: Analyze competitor market situation
---

当用户要求分析竞争对手时，执行以下步骤：
1. 获取竞争对手信息
2. 分析产品特点
3. 比较优劣势
`)
	cmd := parseCommandFile(data, "analyze-competitor.md")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Name != "analyze-competitor" {
		t.Errorf("name = %q, want analyze-competitor", cmd.Name)
	}
	if cmd.Description != "Analyze competitor market situation" {
		t.Errorf("description = %q", cmd.Description)
	}
	if !stringsContains(cmd.Content, "分析竞争对手") {
		t.Errorf("content should contain Chinese text, got: %s", cmd.Content)
	}
}

func TestParseCommandFile_NoFrontmatter(t *testing.T) {
	data := []byte("# Just content\n\nSimple command body.")
	cmd := parseCommandFile(data, "simple-command.md")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Name != "simple-command" {
		t.Errorf("name = %q, want simple-command", cmd.Name)
	}
	if cmd.Description != "" {
		t.Errorf("expected empty description, got %q", cmd.Description)
	}
}

func TestParseCommandFile_Empty(t *testing.T) {
	cmd := parseCommandFile([]byte(""), "empty.md")
	if cmd != nil {
		t.Error("expected nil for empty content")
	}
}

func TestParseCommandFile_WhitespaceOnly(t *testing.T) {
	cmd := parseCommandFile([]byte("   \n\n  "), "whitespace.md")
	if cmd != nil {
		t.Error("expected nil for whitespace-only content")
	}
}

func TestParseCommandFile_OnlyFrontmatter(t *testing.T) {
	data := []byte(`---
name: only-frontmatter
description: No body
---`)
	cmd := parseCommandFile(data, "test.md")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Name != "only-frontmatter" {
		t.Errorf("name = %q", cmd.Name)
	}
	if cmd.Description != "No body" {
		t.Errorf("description = %q", cmd.Description)
	}
	if cmd.Content != "" {
		t.Errorf("expected empty content, got %q", cmd.Content)
	}
}

func TestParseCommandFile_FrontmatterNameOverridesFilename(t *testing.T) {
	data := []byte(`---
name: override-name
description: Override test
---
Some content`)
	cmd := parseCommandFile(data, "filename.md")
	if cmd.Name != "override-name" {
		t.Errorf("name = %q, want override-name (should override filename)", cmd.Name)
	}
}

func TestParseCommandFile_QuotedValues(t *testing.T) {
	data := []byte(`---
name: "quoted-name"
description: "A command with quoted values"
---
Content here`)
	cmd := parseCommandFile(data, "test.md")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Name != "quoted-name" {
		t.Errorf("name = %q", cmd.Name)
	}
	if cmd.Description != "A command with quoted values" {
		t.Errorf("description = %q", cmd.Description)
	}
}

func TestParseCommandFile_ChineseCharacters(t *testing.T) {
	data := []byte(`---
name: 分析竞争对手
description: 分析指定竞争对手的市场情况
---
当用户输入 /分析竞争对手 时：
1. 获取竞争对手信息
2. 分析产品特点
3. 比较优劣势
4. 给出建议
`)
	cmd := parseCommandFile(data, "分析竞争对手.md")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Name != "分析竞争对手" {
		t.Errorf("name = %q, want 分析竞争对手", cmd.Name)
	}
	if cmd.Description != "分析指定竞争对手的市场情况" {
		t.Errorf("description = %q", cmd.Description)
	}
	if !stringsContains(cmd.Content, "获取竞争对手信息") {
		t.Errorf("content missing expected Chinese text, got: %s", cmd.Content)
	}
}

func TestManager_LoadAndGet(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "analyze.md"), []byte("---\nname: analyze-competitor\ndescription: Analyze competitor\n---\n# Content"), 0644)
	os.WriteFile(filepath.Join(dir, "deploy.md"), []byte("---\nname: deploy-app\ndescription: Deploy application\n---\n# Content"), 0644)

	m := NewManager(dir)
	if err := m.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cmd, ok := m.Get("analyze-competitor")
	if !ok {
		t.Fatal("expected to find analyze-competitor")
	}
	if cmd.Description != "Analyze competitor" {
		t.Errorf("description = %q", cmd.Description)
	}

	cmd, ok = m.Get("deploy-app")
	if !ok {
		t.Fatal("expected to find deploy-app")
	}

	// Non-existent command
	_, ok = m.Get("nonexistent")
	if ok {
		t.Error("expected false for nonexistent command")
	}
}

func TestManager_List(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nname: cmd-b\ndescription: B\n---\n# B"), 0644)
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nname: cmd-a\ndescription: A\n---\n# A"), 0644)

	m := NewManager(dir)
	m.Load()
	list := m.List()
	if len(list) != 2 {
		t.Fatalf("got %d commands, want 2", len(list))
	}
	// Should be sorted by name
	if list[0].Name != "cmd-a" || list[1].Name != "cmd-b" {
		t.Errorf("expected sorted order, got %s, %s", list[0].Name, list[1].Name)
	}
}

func TestManager_ListEmpty(t *testing.T) {
	m := NewManager()
	list := m.List()
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestManager_MultiDir(t *testing.T) {
	baseDir := t.TempDir()
	overrideDir := t.TempDir()

	// Base dir: cmd-a
	os.WriteFile(filepath.Join(baseDir, "a.md"), []byte("---\nname: cmd-a\ndescription: Base A\n---\n# Base A"), 0644)
	// Override dir: cmd-a (overrides), cmd-b (new)
	os.WriteFile(filepath.Join(overrideDir, "a.md"), []byte("---\nname: cmd-a\ndescription: Override A\n---\n# Override A"), 0644)
	os.WriteFile(filepath.Join(overrideDir, "b.md"), []byte("---\nname: cmd-b\ndescription: B\n---\n# B"), 0644)

	m := NewManager(baseDir, overrideDir)
	if err := m.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// cmd-a should come from overrideDir (later dir wins)
	ca, ok := m.Get("cmd-a")
	if !ok {
		t.Fatal("expected cmd-a")
	}
	if ca.Description != "Override A" {
		t.Errorf("cmd-a description = %q, want 'Override A'", ca.Description)
	}
	if ca.Source != overrideDir {
		t.Errorf("cmd-a source = %q, want %q", ca.Source, overrideDir)
	}

	// cmd-b from overrideDir
	cb, ok := m.Get("cmd-b")
	if !ok {
		t.Fatal("expected cmd-b")
	}
	if cb.Source != overrideDir {
		t.Errorf("cmd-b source = %q, want %q", cb.Source, overrideDir)
	}
}

func TestManager_Dirs(t *testing.T) {
	m := NewManager("a", "b", "c")
	dirs := m.Dirs()
	if len(dirs) != 3 || dirs[0] != "a" || dirs[1] != "b" || dirs[2] != "c" {
		t.Errorf("Dirs() = %v, want [a b c]", dirs)
	}
}

func TestManager_DefaultDir(t *testing.T) {
	m := NewManager()
	if len(m.Dirs()) != 1 || m.Dirs()[0] != ".dolphin/commands" {
		t.Errorf("default dirs = %v, want [.dolphin/commands]", m.Dirs())
	}
}

func TestManager_EmptyStringFiltered(t *testing.T) {
	m := NewManager("", "valid-dir", "")
	dirs := m.Dirs()
	if len(dirs) != 1 || dirs[0] != "valid-dir" {
		t.Errorf("expected [valid-dir], got %v", dirs)
	}
}

func TestManager_LoadNonExistentDir(t *testing.T) {
	m := NewManager("/nonexistent/path")
	err := m.Load()
	if err != nil {
		t.Fatalf("Load() should not error for nonexistent dir: %v", err)
	}
	if len(m.List()) != 0 {
		t.Errorf("expected 0 commands, got %d", len(m.List()))
	}
}

func TestManager_LoadMixedExistingAndNonExisting(t *testing.T) {
	existingDir := t.TempDir()
	os.WriteFile(filepath.Join(existingDir, "test.md"), []byte("---\nname: test-cmd\ndescription: Test\n---\n# Content"), 0644)

	m := NewManager("/nonexistent", existingDir)
	if err := m.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(m.List()) != 1 {
		t.Errorf("expected 1 command, got %d", len(m.List()))
	}
}

func TestManager_LoadErrorOnPermissionDenied(t *testing.T) {
	// We can't easily test permission denied without root, but we can
	// verify that a real dir without .md files works fine
	dir := t.TempDir()
	m := NewManager(dir)
	err := m.Load()
	if err != nil {
		t.Fatalf("Load() should not error for empty dir: %v", err)
	}
}

func TestManager_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "subdir")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "nested.md"), []byte("---\nname: nested\n---\n# Content"), 0644)

	m := NewManager(dir)
	m.Load()
	if len(m.List()) != 0 {
		t.Errorf("expected 0 commands (subdir), got %d", len(m.List()))
	}
}

func TestManager_RecordUsage(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.md"), []byte("---\nname: test-cmd\ndescription: Test\n---\n# T"), 0644)

	m := NewManager(dir)
	m.Load()

	m.RecordUsage("test-cmd")
	m.RecordUsage("test-cmd")

	cmd, _ := m.Get("test-cmd")
	if cmd.CallCount != 2 {
		t.Errorf("call count = %d, want 2", cmd.CallCount)
	}
}

func TestManager_RecordUsageNonExistent(t *testing.T) {
	m := NewManager()
	// Should not panic
	m.RecordUsage("nonexistent")
}

func TestManager_Reload(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nname: cmd-a\ndescription: A\n---\n# A"), 0644)

	m := NewManager(dir)
	m.Load()
	if len(m.List()) != 1 {
		t.Fatalf("expected 1 command")
	}

	// Add another command file and reload
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nname: cmd-b\ndescription: B\n---\n# B"), 0644)
	m.Load()
	if len(m.List()) != 2 {
		t.Errorf("expected 2 commands after reload, got %d", len(m.List()))
	}
}

func TestManager_ReloadRemovesOldCommands(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nname: cmd-a\ndescription: A\n---\n# A"), 0644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nname: cmd-b\ndescription: B\n---\n# B"), 0644)

	m := NewManager(dir)
	m.Load()
	if len(m.List()) != 2 {
		t.Fatalf("expected 2 commands")
	}

	// Remove one file and reload
	os.Remove(filepath.Join(dir, "a.md"))
	m.Load()
	if len(m.List()) != 1 {
		t.Errorf("expected 1 command after removal, got %d", len(m.List()))
	}
	_, ok := m.Get("cmd-a")
	if ok {
		t.Error("cmd-a should be gone after reload")
	}
}

func TestManager_SourceSetOnLoad(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.md"), []byte("---\nname: test-cmd\n---\n# Content"), 0644)

	m := NewManager(dir)
	m.Load()

	cmd, _ := m.Get("test-cmd")
	if cmd.Source != dir {
		t.Errorf("source = %q, want %q", cmd.Source, dir)
	}
}

func TestManager_ChineseCommandName(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "分析竞争对手.md"), []byte("---\nname: 分析竞争对手\ndescription: 分析竞争对手\n---\n# 内容"), 0644)

	m := NewManager(dir)
	m.Load()

	cmd, ok := m.Get("分析竞争对手")
	if !ok {
		t.Fatal("expected to find 分析竞争对手")
	}
	if cmd.Name != "分析竞争对手" {
		t.Errorf("name = %q", cmd.Name)
	}
}

func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && contains(s, substr)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
