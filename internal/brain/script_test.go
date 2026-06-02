package brain

import (
	"context"
	"strings"
	"testing"
)

func TestParseScript(t *testing.T) {
	data := "---\nname: test-script\ndescription: A test script\nenabled: true\n---\necho hello"
	s, err := parseScript(data)
	if err != nil {
		t.Fatalf("parseScript failed: %v", err)
	}
	if s.Name != "test-script" {
		t.Errorf("expected name 'test-script', got %q", s.Name)
	}
	if s.Description != "A test script" {
		t.Errorf("expected description 'A test script', got %q", s.Description)
	}
	if !s.Enabled {
		t.Error("expected enabled=true")
	}
	if strings.TrimSpace(s.Content) != "echo hello" {
		t.Errorf("expected content 'echo hello', got %q", s.Content)
	}
}

func TestParseScriptNoBody(t *testing.T) {
	// serializeScript produces "---\n" before and after.
	data := "---\nname: empty-script\ndescription: no body\nenabled: true\n---\n"
	s, err := parseScript(data)
	if err != nil {
		t.Fatalf("parseScript failed: %v", err)
	}
	if s.Content != "" {
		t.Errorf("expected empty content, got %q", s.Content)
	}
}

func TestParseScriptMissingDelim(t *testing.T) {
	_, err := parseScript("no frontmatter")
	if err == nil {
		t.Fatal("expected error for missing frontmatter delimiter")
	}
}

func TestParseScriptMissingName(t *testing.T) {
	data := "---\ndescription: no name\nenabled: true\n---\nbody"
	_, err := parseScript(data)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestSerializeScript(t *testing.T) {
	s := Script{Name: "my-script", Description: "my desc", Enabled: true, Content: "run this"}
	data, err := serializeScript(s)
	if err != nil {
		t.Fatalf("serializeScript failed: %v", err)
	}
	if !strings.Contains(data, "name: my-script") {
		t.Errorf("expected name in serialized output, got: %s", data)
	}
	if !strings.Contains(data, "my desc") {
		t.Errorf("expected description in output")
	}
	if !strings.Contains(data, "run this") {
		t.Errorf("expected content in output")
	}
}

func TestScriptRoundTrip(t *testing.T) {
	original := Script{Name: "roundtrip", Description: "desc", Enabled: false, Content: "content here"}
	data, err := serializeScript(original)
	if err != nil {
		t.Fatalf("serialize failed: %v", err)
	}
	parsed, err := parseScript(data)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if parsed.Name != original.Name {
		t.Errorf("name: expected %q, got %q", original.Name, parsed.Name)
	}
	if parsed.Description != original.Description {
		t.Errorf("description: expected %q, got %q", original.Description, parsed.Description)
	}
	if parsed.Enabled != original.Enabled {
		t.Errorf("enabled: expected %v, got %v", original.Enabled, parsed.Enabled)
	}
	if strings.TrimSpace(parsed.Content) != original.Content {
		t.Errorf("content: expected %q, got %q", original.Content, parsed.Content)
	}
}

func TestReadScript(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	s := Script{Name: "my-script", Description: "desc", Enabled: true, Content: "echo hello"}
	if err := WriteScript(ctx, b, s); err != nil {
		t.Fatalf("WriteScript failed: %v", err)
	}

	got, err := ReadScript(ctx, b, "my-script")
	if err != nil {
		t.Fatalf("ReadScript failed: %v", err)
	}
	if got.Name != "my-script" {
		t.Errorf("expected name 'my-script', got %q", got.Name)
	}
	if strings.TrimSpace(got.Content) != "echo hello" {
		t.Errorf("expected content 'echo hello', got %q", got.Content)
	}
}

func TestReadScriptEmptyName(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	b.Init(ctx)

	_, err := ReadScript(ctx, b, "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestWriteScript(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	s := Script{Name: "new-script", Description: "new desc", Enabled: true, Content: "echo new"}
	if err := WriteScript(ctx, b, s); err != nil {
		t.Fatalf("WriteScript failed: %v", err)
	}

	files, err := b.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f == "scripts/new-script.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("scripts/new-script.md not found in brain listing")
	}
}

func TestWriteScriptEmptyName(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	b.Init(ctx)

	err := WriteScript(ctx, b, Script{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestListScripts(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	WriteScript(ctx, b, Script{Name: "script-a", Content: "echo a"})
	WriteScript(ctx, b, Script{Name: "script-b", Content: "echo b"})

	scripts, err := ListScripts(ctx, b)
	if err != nil {
		t.Fatalf("ListScripts failed: %v", err)
	}
	if len(scripts) != 2 {
		t.Fatalf("expected 2 scripts, got %d", len(scripts))
	}
	names := map[string]bool{}
	for _, s := range scripts {
		names[s.Name] = true
	}
	if !names["script-a"] {
		t.Error("expected script-a in list")
	}
	if !names["script-b"] {
		t.Error("expected script-b in list")
	}
}

func TestListScriptsEmpty(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	scripts, err := ListScripts(ctx, b)
	if err != nil {
		t.Fatalf("ListScripts failed: %v", err)
	}
	if len(scripts) != 0 {
		t.Errorf("expected 0 scripts, got %d", len(scripts))
	}
}

func TestDeleteScript(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	WriteScript(ctx, b, Script{Name: "delete-me", Content: "to be deleted"})
	if err := DeleteScript(ctx, b, "delete-me"); err != nil {
		t.Fatalf("DeleteScript failed: %v", err)
	}

	_, err := ReadScript(ctx, b, "delete-me")
	if err == nil {
		t.Fatal("expected error after deleting script")
	}
}

func TestDeleteScriptEmptyName(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	b.Init(ctx)

	err := DeleteScript(ctx, b, "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}
