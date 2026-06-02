package brain

import (
	"context"
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	data := "---\nname: test-cmd\ndescription: A test\nenabled: true\n---\necho hello"
	cmd, err := parseCommand(data)
	if err != nil {
		t.Fatalf("parseCommand failed: %v", err)
	}
	if cmd.Name != "test-cmd" {
		t.Errorf("expected name 'test-cmd', got %q", cmd.Name)
	}
	if cmd.Description != "A test" {
		t.Errorf("expected description 'A test', got %q", cmd.Description)
	}
	if !cmd.Enabled {
		t.Error("expected enabled=true")
	}
	if strings.TrimSpace(cmd.Content) != "echo hello" {
		t.Errorf("expected content 'echo hello', got %q", cmd.Content)
	}
}

func TestParseCommandNoBody(t *testing.T) {
	// serializeCommand produces "---\n" before and after, with optional body.
	// The closing delimiter always has a trailing newline.
	data := "---\nname: empty-cmd\ndescription: no body\nenabled: true\n---\n"
	cmd, err := parseCommand(data)
	if err != nil {
		t.Fatalf("parseCommand failed: %v", err)
	}
	if cmd.Name != "empty-cmd" {
		t.Errorf("expected name 'empty-cmd', got %q", cmd.Name)
	}
	if cmd.Content != "" {
		t.Errorf("expected empty content, got %q", cmd.Content)
	}
}

func TestParseCommandMissingDelim(t *testing.T) {
	_, err := parseCommand("no frontmatter here")
	if err == nil {
		t.Fatal("expected error for missing frontmatter delimiter")
	}
}

func TestParseCommandMissingName(t *testing.T) {
	data := "---\ndescription: no name\nenabled: true\n---\nbody"
	_, err := parseCommand(data)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestSerializeCommand(t *testing.T) {
	cmd := Command{Name: "my-cmd", Description: "my desc", Enabled: true, Content: "run this"}
	data, err := serializeCommand(cmd)
	if err != nil {
		t.Fatalf("serializeCommand failed: %v", err)
	}
	if !strings.Contains(data, "name: my-cmd") {
		t.Errorf("expected name in serialized output, got: %s", data)
	}
	if !strings.Contains(data, "my desc") {
		t.Errorf("expected description in output")
	}
	if !strings.Contains(data, "run this") {
		t.Errorf("expected content in output")
	}
}

func TestCommandRoundTrip(t *testing.T) {
	original := Command{Name: "roundtrip", Description: "desc", Enabled: false, Content: "content here"}
	data, err := serializeCommand(original)
	if err != nil {
		t.Fatalf("serialize failed: %v", err)
	}
	parsed, err := parseCommand(data)
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

func TestReadCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	cmd := Command{Name: "my-cmd", Description: "desc", Enabled: true, Content: "echo hello"}
	if err := WriteCommand(ctx, b, cmd); err != nil {
		t.Fatalf("WriteCommand failed: %v", err)
	}

	got, err := ReadCommand(ctx, b, "my-cmd")
	if err != nil {
		t.Fatalf("ReadCommand failed: %v", err)
	}
	if got.Name != "my-cmd" {
		t.Errorf("expected name 'my-cmd', got %q", got.Name)
	}
	if strings.TrimSpace(got.Content) != "echo hello" {
		t.Errorf("expected content 'echo hello', got %q", got.Content)
	}
}

func TestReadCommandNotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	_, err := ReadCommand(ctx, b, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
}

func TestReadCommandEmptyName(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	b.Init(ctx)

	_, err := ReadCommand(ctx, b, "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestWriteCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	cmd := Command{Name: "new-cmd", Description: "new desc", Enabled: true, Content: "echo new"}
	if err := WriteCommand(ctx, b, cmd); err != nil {
		t.Fatalf("WriteCommand failed: %v", err)
	}

	// Verify file was created in brain.
	files, err := b.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f == "commands/new-cmd.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("commands/new-cmd.md not found in brain listing")
	}
}

func TestWriteCommandEmptyName(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	b.Init(ctx)

	err := WriteCommand(ctx, b, Command{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestListCommands(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	WriteCommand(ctx, b, Command{Name: "cmd-a", Content: "echo a"})
	WriteCommand(ctx, b, Command{Name: "cmd-b", Content: "echo b"})

	cmds, err := ListCommands(ctx, b)
	if err != nil {
		t.Fatalf("ListCommands failed: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
	names := map[string]bool{}
	for _, c := range cmds {
		names[c.Name] = true
	}
	if !names["cmd-a"] {
		t.Error("expected cmd-a in list")
	}
	if !names["cmd-b"] {
		t.Error("expected cmd-b in list")
	}
}

func TestListCommandsEmpty(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	cmds, err := ListCommands(ctx, b)
	if err != nil {
		t.Fatalf("ListCommands failed: %v", err)
	}
	if len(cmds) != 0 {
		t.Errorf("expected 0 commands, got %d", len(cmds))
	}
}

func TestDeleteCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	WriteCommand(ctx, b, Command{Name: "delete-me", Content: "to be deleted"})

	if err := DeleteCommand(ctx, b, "delete-me"); err != nil {
		t.Fatalf("DeleteCommand failed: %v", err)
	}

	_, err := ReadCommand(ctx, b, "delete-me")
	if err == nil {
		t.Fatal("expected error after deleting command")
	}
}

func TestDeleteCommandEmptyName(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	b.Init(ctx)

	err := DeleteCommand(ctx, b, "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}
