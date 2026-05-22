package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestConfig creates a minimal config for testing.
func writeTestConfig(t *testing.T, dir string) string {
	t.Helper()
	cfg := `
session:
  dir: ` + dir + `/sessions
skills:
  dir: ` + dir + `/skills
  repos: []
llm:
  api_key: test-key
  model: test-model
  type: openai
  base_url: http://localhost:9999
mcp:
  shell:
    enabled: false
  cdp:
    enabled: false
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeTestSkill creates a skill SKILL.md file in a <name> subdirectory for testing.
func writeTestSkill(t *testing.T, dir, name, desc, content string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n" + content
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// setupSkillsTest isolates the test from real config and skills by:
//   - changing HOME to a temp dir so ~/.dolphin is not found
//   - changing to a temp dir so project .dolphin/config.yaml is not found
//   - setting cfgFile to a test config pointing into the temp dir
func setupSkillsTest(t *testing.T) (origCfg string, dir string) {
	t.Helper()
	origCfg = cfgFile
	dir = t.TempDir()

	origWd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() {
		os.Chdir(origWd)
		cfgFile = origCfg
	})

	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // Windows: os.UserHomeDir checks USERPROFILE first

	cfgFile = writeTestConfig(t, dir)
	return
}

// captureOutput redirects os.Stdout during f() and returns what was written.
func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestSkillsList_Empty(t *testing.T) {
	_, dir := setupSkillsTest(t)
	os.MkdirAll(filepath.Join(dir, "skills"), 0o700)

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"list"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "No skills installed") {
		t.Errorf("expected empty message, got: %s", output)
	}
}

func TestSkillsList_WithSkills(t *testing.T) {
	_, dir := setupSkillsTest(t)
	writeTestSkill(t, filepath.Join(dir, "skills"), "test-skill", "A test skill", "content")
	writeTestSkill(t, filepath.Join(dir, "skills"), "other", "Other skill", "content")

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"list"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "test-skill") {
		t.Errorf("expected test-skill in output, got: %s", output)
	}
	if !strings.Contains(output, "other") {
		t.Errorf("expected other in output, got: %s", output)
	}
	if !strings.Contains(output, "Total: 2 skills") {
		t.Errorf("expected total count, got: %s", output)
	}
}

func TestSkillsSearch_Local(t *testing.T) {
	_, dir := setupSkillsTest(t)
	writeTestSkill(t, filepath.Join(dir, "skills"), "git-workflow", "Git operations and branching", "content")
	writeTestSkill(t, filepath.Join(dir, "skills"), "docker-deploy", "Docker deployment guide", "content")

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"search", "git"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "git-workflow") {
		t.Errorf("expected git-workflow in results, got: %s", output)
	}
	if strings.Contains(output, "docker") {
		t.Errorf("expected no docker result, got: %s", output)
	}
	if !strings.Contains(output, "local") {
		t.Errorf("expected 'local' source marker, got: %s", output)
	}
}

func TestSkillsSearch_NoResults(t *testing.T) {
	_, dir := setupSkillsTest(t)
	writeTestSkill(t, filepath.Join(dir, "skills"), "git-workflow", "Git operations", "content")

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"search", "nonexistent"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "No skills found") {
		t.Errorf("expected no results message, got: %s", output)
	}
}

func TestSkillsInstall(t *testing.T) {
	_, dir := setupSkillsTest(t)
	userSkillsDir := filepath.Join(dir, ".dolphin", "skills")
	os.MkdirAll(userSkillsDir, 0o700)

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"install", "my-new-skill", "A brand new skill"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "my-new-skill") {
		t.Errorf("expected success message, got: %s", output)
	}

	// File is created in the user-skills dir (m.dirs[0]), which is ~/.dolphin/skills.
	skillPath := filepath.Join(userSkillsDir, "my-new-skill", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Errorf("skill file not created at %s", skillPath)
	} else {
		data, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "A brand new skill") {
			t.Errorf("expected description in file, got: %s", string(data))
		}
	}
}

func TestSkillsInstall_DefaultDescription(t *testing.T) {
	_, dir := setupSkillsTest(t)
	userSkillsDir := filepath.Join(dir, ".dolphin", "skills")
	os.MkdirAll(userSkillsDir, 0o700)

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"install", "no-desc"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "no-desc") {
		t.Errorf("expected success message, got: %s", output)
	}

	// File is created in the user-skills dir (m.dirs[0]), which is ~/.dolphin/skills.
	skillPath := filepath.Join(userSkillsDir, "no-desc", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Errorf("skill file not created at %s", skillPath)
	}
}

func TestSkillsInstall_FailsWithNoSkillDir(t *testing.T) {
	_, dir := setupSkillsTest(t)
	// No skills dir at all — install should create it in user skills dir.

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"install", "auto-create", "Testing"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "auto-create") {
		t.Errorf("expected success message, got: %s", output)
	}

	// File is created in user-skills dir (~/.dolphin/skills) which gets auto-created.
	skillPath := filepath.Join(dir, ".dolphin", "skills", "auto-create", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Errorf("skill file not created at %s", skillPath)
	}
}

func TestSkillsDisable(t *testing.T) {
	_, dir := setupSkillsTest(t)
	userSkillsDir := filepath.Join(dir, ".dolphin", "skills")
	writeTestSkill(t, userSkillsDir, "to-remove", "Will be removed", "content")

	// Verify it exists before
	skillPath := filepath.Join(userSkillsDir, "to-remove", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Fatal("skill file should exist before disable")
	}

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"disable", "to-remove"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "disabled and removed") {
		t.Errorf("expected success message, got: %s", output)
	}

	// Verify file was deleted
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Errorf("skill file should be removed after disable, still exists at %s", skillPath)
	}
}

func TestSkillsDisable_NotFound(t *testing.T) {
	_, dir := setupSkillsTest(t)
	os.MkdirAll(filepath.Join(dir, "skills"), 0o700)

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"disable", "not-exist"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-existent skill")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestSkills_SearchFetchesRemote(t *testing.T) {
	_, dir := setupSkillsTest(t)
	writeTestSkill(t, filepath.Join(dir, "skills"), "local-skill", "A local skill", "content")

	// Add a repo that doesn't exist — should fail gracefully (no panic, no crash)
	cfgPath := cfgFile
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	withRepo := strings.ReplaceAll(string(data), "repos: []", "repos: [\"dolphinv/nonexistent\"]")
	if err := os.WriteFile(cfgPath, []byte(withRepo), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"search", "local"})

	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	// Should still find local results even when remote fetch fails
	if !strings.Contains(output, "local-skill") {
		t.Errorf("expected local results despite remote failure, got: %s", output)
	}
}

func TestNewSkillsCmd_HasSubcommands(t *testing.T) {
	cmd := NewSkillsCmd()
	subs := cmd.Commands()
	expected := map[string]bool{"list": false, "search": false, "install": false, "new": false, "disable": false, "enable": false, "uninstall": false}
	for _, sub := range subs {
		if _, ok := expected[sub.Name()]; !ok {
			t.Errorf("unexpected subcommand: %s", sub.Name())
		}
		expected[sub.Name()] = true
	}
	for name, found := range expected {
		if !found {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestRunSkillsList_InvalidConfig(t *testing.T) {
	// Point cfgFile at a directory — ReadFile on a directory returns an error
	// that is not fs.ErrNotExist, so config.Load will propagate it.
	cfgFile = t.TempDir()
	defer func() { cfgFile = "" }()

	err := runSkillsList(nil, nil)
	if err == nil {
		t.Fatal("expected error with invalid config")
	}
}

func TestSkillsEnable(t *testing.T) {
	_, dir := setupSkillsTest(t)
	userSkillsDir := filepath.Join(dir, ".dolphin", "skills")
	writeTestSkill(t, userSkillsDir, "to-enable", "Will be enabled", "content")

	// First disable it
	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"disable", "to-enable"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("disable: %v", err)
	}

	// Verify .disabled dir exists
	disabledDir := filepath.Join(userSkillsDir, "to-enable.disabled")
	if _, err := os.Stat(disabledDir); os.IsNotExist(err) {
		t.Fatal("disabled dir should exist after disable")
	}

	// Now enable it
	cmd2 := NewSkillsCmd()
	cmd2.SetArgs([]string{"enable", "to-enable"})
	output := captureOutput(func() {
		if err := cmd2.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "enabled") {
		t.Errorf("expected enabled message, got: %s", output)
	}

	// .disabled dir should be gone, original back
	if _, err := os.Stat(disabledDir); !os.IsNotExist(err) {
		t.Error("disabled dir should be removed after enable")
	}
	origDir := filepath.Join(userSkillsDir, "to-enable")
	if _, err := os.Stat(filepath.Join(origDir, "SKILL.md")); os.IsNotExist(err) {
		t.Error("SKILL.md should exist after enable")
	}
}

func TestSkillsEnable_NotFound(t *testing.T) {
	_, dir := setupSkillsTest(t)
	os.MkdirAll(filepath.Join(dir, "skills"), 0o700)

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"enable", "not-exist"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-existent disabled skill")
	}
}

func TestSkillsUninstall(t *testing.T) {
	_, dir := setupSkillsTest(t)
	userSkillsDir := filepath.Join(dir, ".dolphin", "skills")
	writeTestSkill(t, userSkillsDir, "to-uninstall", "Will be deleted", "content")

	// Verify it exists
	skillPath := filepath.Join(userSkillsDir, "to-uninstall", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Fatal("skill file should exist before uninstall")
	}

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"uninstall", "to-uninstall"})
	output := captureOutput(func() {
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "uninstalled") {
		t.Errorf("expected uninstalled message, got: %s", output)
	}

	// Files should be deleted (not .disabled)
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Error("skill should be deleted after uninstall")
	}
	disabledDir := filepath.Join(userSkillsDir, "to-uninstall.disabled")
	if _, err := os.Stat(disabledDir); !os.IsNotExist(err) {
		t.Error("no .disabled dir should exist after uninstall")
	}
}

func TestSkillsUninstall_NotFound(t *testing.T) {
	_, dir := setupSkillsTest(t)
	os.MkdirAll(filepath.Join(dir, "skills"), 0o700)

	cmd := NewSkillsCmd()
	cmd.SetArgs([]string{"uninstall", "not-exist"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-existent skill")
	}
}
