package registry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dolphin/internal/mcp"

	"github.com/spf13/cobra"
)

func TestBuildToolDef_NilCmd(t *testing.T) {
	if td := BuildToolDef(&CommandSpec{}); td != nil {
		t.Error("expected nil for nil cmd")
	}
}

func TestBuildToolDef_NoHandler(t *testing.T) {
	cmd := &cobra.Command{Use: "nop"}
	spec := &CommandSpec{Cobra: cmd}
	if td := BuildToolDef(spec); td != nil {
		t.Error("expected nil for command with no RunE/Run")
	}
}

func TestBuildToolDef_Basic(t *testing.T) {
	cmd := &cobra.Command{
		Use:   "hello",
		Short: "say hello",
		RunE: func(c *cobra.Command, args []string) error {
			_, _ = c.OutOrStdout().Write([]byte("hello\n"))
			return nil
		},
	}
	spec := &CommandSpec{Cobra: cmd}
	td := BuildToolDef(spec)
	if td == nil {
		t.Fatal("expected ToolDef")
	}
	if td.Name != "hello" {
		t.Errorf("expected name 'hello', got %q", td.Name)
	}
	if td.Description != "say hello" {
		t.Errorf("expected description 'say hello', got %q", td.Description)
	}
	if td.SelfEvolution {
		t.Error("expected SelfEvolution=false")
	}
}

func TestBuildToolDef_WithToolName(t *testing.T) {
	cmd := &cobra.Command{
		Use: "list-workflows",
		RunE: func(c *cobra.Command, args []string) error {
			return nil
		},
	}
	spec := &CommandSpec{Cobra: cmd, ToolName: "list_workflows"}
	td := BuildToolDef(spec)
	if td == nil {
		t.Fatal("expected ToolDef")
	}
	if td.Name != "list_workflows" {
		t.Errorf("expected 'list_workflows', got %q", td.Name)
	}
}

func TestBuildToolDef_Handler(t *testing.T) {
	var executed bool
	cmd := &cobra.Command{
		Use: "greet",
		RunE: func(c *cobra.Command, args []string) error {
			executed = true
			_, _ = c.OutOrStdout().Write([]byte("hi there\n"))
			return nil
		},
	}
	spec := &CommandSpec{Cobra: cmd}
	td := BuildToolDef(spec)

	input, _ := json.Marshal([]string{})
	result, err := td.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !executed {
		t.Error("handler was not executed")
	}
	if result == nil {
		t.Fatal("expected ToolResult")
	}
	if result.Content != "hi there" {
		t.Errorf("expected 'hi there', got %q", result.Content)
	}
	if result.IsError {
		t.Error("expected success")
	}
}

func TestBuildToolDef_HandlerError(t *testing.T) {
	cmd := &cobra.Command{
		Use: "fail",
		RunE: func(c *cobra.Command, args []string) error {
			return errFail
		},
	}
	spec := &CommandSpec{Cobra: cmd}
	td := BuildToolDef(spec)

	input, _ := json.Marshal([]string{})
	result, err := td.Handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected ToolResult")
	}
	if !result.IsError {
		t.Error("expected IsError")
	}
	if !strings.Contains(result.Content, "command failed") {
		t.Errorf("expected error message, got %q", result.Content)
	}
}

func TestBuildToolDef_SelfEvolution(t *testing.T) {
	cmd := &cobra.Command{
		Use: "secret",
		RunE: func(c *cobra.Command, args []string) error {
			return nil
		},
	}
	spec := &CommandSpec{Cobra: cmd, SelfEvolution: true}

	// Without self-evolution: should be excluded.
	specs := []*CommandSpec{spec}
	defs := BuildToolDefs(specs, false)
	if len(defs) != 0 {
		t.Error("expected 0 tools when self-evolution disabled")
	}

	// With self-evolution: should be included.
	defs = BuildToolDefs(specs, true)
	if len(defs) != 1 {
		t.Errorf("expected 1 tool when self-evolution enabled, got %d", len(defs))
	}
}

func TestInferSchema(t *testing.T) {
	cmd := &cobra.Command{
		Use:  "test",
		Args: cobra.ExactArgs(1),
	}
	cmd.Flags().String("name", "", "name to greet")
	cmd.Flags().Int("count", 1, "number of times")
	cmd.Flags().Bool("verbose", false, "enable verbose")

	schema := inferSchema(cmd)
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties object")
	}

	// Check positional args.
	if _, ok := props["args"]; !ok {
		t.Error("expected 'args' property for positional args")
	}

	// Check flags.
	for _, name := range []string{"name", "count", "verbose"} {
		if _, ok := props[name]; !ok {
			t.Errorf("expected flag %q in schema properties", name)
		}
	}

	// Check flag types.
	if p := props["count"]; p != nil {
		m := p.(map[string]any)
		if m["type"] != "integer" {
			t.Errorf("expected integer type for count, got %v", m["type"])
		}
	}
	if p := props["verbose"]; p != nil {
		m := p.(map[string]any)
		if m["type"] != "boolean" {
			t.Errorf("expected boolean type for verbose, got %v", m["type"])
		}
	}
}

func TestToolResultContent(t *testing.T) {
	// Verify mcp.ToolResult can hold the content we produce.
	// This is a compile-time/sanity check.
	result := &mcp.ToolResult{Content: "output", IsError: false}
	if result.Content != "output" {
		t.Error("ToolResult content mismatch")
	}
}
