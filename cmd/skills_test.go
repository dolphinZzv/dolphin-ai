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
	if err := os.WriteFile(path, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeTestSkill creates a skill .md file for testing.
func writeTestSkill(t *testing.T, dir, name, desc, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n" + content
	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
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
	os.MkdirAll(filepath.Join(dir, "skills"), 0700)

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
	os.MkdirAll(filepath.Join(dir, "skills"), 0700)

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

	// Verify file was created in the temp dir
	skillPath := filepath.Join(dir, "skills", "my-new-skill.md")
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
	os.MkdirAll(filepath.Join(dir, "skills"), 0700)

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

	// Verify file was created
	skillPath := filepath.Join(dir, "skills", "no-desc.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Errorf("skill file not created at %s", skillPath)
	}
}

func TestSkillsInstall_FailsWithNoSkillDir(t *testing.T) {
	_, dir := setupSkillsTest(t)
	// No skills dir at all — install should create it

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

	skillPath := filepath.Join(dir, "skills", "auto-create.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Errorf("skill file not created at %s", skillPath)
	}
}

func TestSkillsDisable(t *testing.T) {
	_, dir := setupSkillsTest(t)
	writeTestSkill(t, filepath.Join(dir, "skills"), "to-remove", "Will be removed", "content")

	// Verify it exists before
	skillPath := filepath.Join(dir, "skills", "to-remove.md")
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
	os.MkdirAll(filepath.Join(dir, "skills"), 0700)

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
	if err := os.WriteFile(cfgPath, []byte(withRepo), 0600); err != nil {
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
	expected := map[string]bool{"list": false, "search": false, "install": false, "disable": false}
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
	cfgFile = "/nonexistent/config.yaml"
	defer func() { cfgFile = "" }()

	err := runSkillsList(nil, nil)
	if err == nil {
		t.Fatal("expected error with invalid config")
	}
}
