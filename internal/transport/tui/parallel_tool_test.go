package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"dolphin/internal/types"
)

// TestParallelToolResultsPairByIDs verifies that when two tool calls run in
// parallel (both call entries arrive, then both results arrive), each result
// is appended to its own call rather than emitted as a loose tool_result
// block — i.e. the output stays paired: call A / result-a, call B / result-b.
func TestParallelToolResultsPairByIDs(t *testing.T) {
	m := newModel()
	m.showTools = true
	m.width = 80
	m.height = 24
	m.updateViewportHeight()

	upd := func(msg tea.Msg) {
		nm, _ := m.Update(msg)
		m = nm.(model)
	}

	// Two parallel tool calls with distinct IDs, identical command text.
	upd(toolCallMsg{call: types.ToolCall{ID: "call-A", Name: "shell", Arguments: `{"command":"sleep 20 && echo ok"}`}})
	upd(toolCallMsg{call: types.ToolCall{ID: "call-B", Name: "shell", Arguments: `{"command":"sleep 20 && echo ok"}`}})

	// Results arrive after both calls; each carries its own ToolCallID.
	upd(toolResultMsg{result: types.ToolResult{ToolCallID: "call-A", Content: "ok", IsError: false}})
	upd(toolResultMsg{result: types.ToolResult{ToolCallID: "call-B", Content: "ok", IsError: false}})

	out := m.renderedContent
	plain := ansiRe.ReplaceAllString(out, "")
	callHeaders := strings.Count(plain, "⏺ shell(")
	if callHeaders != 2 {
		t.Fatalf("expected 2 tool_call headers, got %d\noutput:\n%s", callHeaders, plain)
	}

	// The two "ok" results must each sit directly beneath a call header,
	// not be grouped together at the end. Verify by checking that between
	// the two headers there is an "ok" (paired), i.e. pattern header/ok/header/ok.
	idxA := strings.Index(plain, "⏺ shell(")
	idxB := strings.Index(plain[idxA+1:], "⏺ shell(")
	if idxB < 0 {
		t.Fatalf("second header not found\noutput:\n%s", plain)
	}
	idxB += idxA + 1

	okA := strings.Index(plain[idxA:], "ok")
	okB := strings.Index(plain[idxB:], "ok")
	if okA < 0 || okB < 0 {
		t.Fatalf("missing ok results\noutput:\n%s", plain)
	}
	okA += idxA
	okB += idxB

	// okA must come before the second header (paired with call A), and okB
	// after the second header (paired with call B). If results were loose,
	// both oks would appear after idxB.
	if okA >= idxB || okB <= idxB {
		t.Fatalf("results not paired with their calls (got loose results):\n%s", plain)
	}
}

// TestParallelToolResults_EmptyIDs verifies behavior when the LLM provider
// sends no call IDs (tc.ID == "" for every call). In that case all results
// would match the last call by empty-string ID and pile onto it. This test
// documents the current (degraded) behavior so we notice if it changes.
func TestParallelToolResults_EmptyIDs(t *testing.T) {
	m := newModel()
	m.showTools = true
	m.width = 80
	m.height = 24
	m.updateViewportHeight()
	upd := func(msg tea.Msg) {
		nm, _ := m.Update(msg)
		m = nm.(model)
	}

	upd(toolCallMsg{call: types.ToolCall{ID: "", Name: "shell", Arguments: `{"command":"a"}`}})
	upd(toolCallMsg{call: types.ToolCall{ID: "", Name: "shell", Arguments: `{"command":"b"}`}})
	upd(toolResultMsg{result: types.ToolResult{ToolCallID: "", Content: "ok-a", IsError: false}})
	upd(toolResultMsg{result: types.ToolResult{ToolCallID: "", Content: "ok-b", IsError: false}})

	out := m.renderedContent
	plain := ansiRe.ReplaceAllString(out, "")
	// Both results land on the same (last) call when IDs are empty — this is
	// the known degradation. We assert only that rendering does not crash and
	// both results appear somewhere.
	if !strings.Contains(plain, "ok-a") || !strings.Contains(plain, "ok-b") {
		t.Fatalf("missing results in output:\n%s", plain)
	}
	t.Logf("empty-ID output (results may pile onto last call):\n%s", plain)
}
