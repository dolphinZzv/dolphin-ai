package common

import "testing"

func TestToolDescStruct(t *testing.T) {
	td := ToolDesc{
		Name:        "test_tool",
		Description: "A test tool",
		URL:         "https://example.com",
		Command:     "echo",
		Args:        []string{"hello"},
		Executor:    "mock_executor",
	}
	if td.Name != "test_tool" {
		t.Errorf("expected 'test_tool', got %q", td.Name)
	}
	if td.Description != "A test tool" {
		t.Errorf("expected 'A test tool', got %q", td.Description)
	}
	if td.URL != "https://example.com" {
		t.Errorf("expected 'https://example.com', got %q", td.URL)
	}
	if td.Command != "echo" {
		t.Errorf("expected 'echo', got %q", td.Command)
	}
	if len(td.Args) != 1 || td.Args[0] != "hello" {
		t.Errorf("expected ['hello'], got %v", td.Args)
	}
	if td.Executor != "mock_executor" {
		t.Errorf("expected 'mock_executor', got %v", td.Executor)
	}
}

func TestVersion(t *testing.T) {
	// Version is set at build time; default should be "dev"
	if Version != "dev" {
		t.Errorf("expected default Version 'dev', got %q", Version)
	}
}
