package tui

import (
	"fmt"
	"strings"
	"testing"
)

// newBenchModel creates a model pre-filled with `rounds` conversation rounds.
// Each round: user message (user_text) + AI text + tool call + tool result.
// Returns total message count and the model.
func newBenchModel(rounds int) model {
	m := newModel()
	m.width = 120
	m.height = 40
	m.agentName = "Dolphin"
	m.modelName = "bench-model"
	m.showTools = true
	m.ready = true
	m.textarea.SetWidth(119)
	m.viewport.SetWidth(120)
	m.viewport.SetHeight(35)

	for r := 0; r < rounds; r++ {
		m.appendEntry(renderEntry{content: fmt.Sprintf("user question %d", r), style: "user_text"})
		m.appendEntry(renderEntry{content: fmt.Sprintf("AI response %d with some **markdown** and `code`", r), style: "text"})
		m.appendEntry(renderEntry{content: fmt.Sprintf("tool_call_%d(x, y)", r), style: "tool_call"})
		m.appendEntry(renderEntry{content: fmt.Sprintf("result data for call %d", r), style: "tool_result"})
	}

	// Reset dirty flag (set during model building, but full rebuild clears it).
	return m
}

// BenchmarkFullRebuild benchmarks full viewport rebuild with varying message counts.
func BenchmarkFullRebuild(b *testing.B) {
	sizes := []int{10, 50, 200, 500}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("msgs=%d", n*4), func(b *testing.B) {
			m := newBenchModel(n)
			b.ResetTimer()
			for b.Loop() {
				m.fullRebuild()
			}
		})
	}
}

// BenchmarkRenderIncremental_AppendNewBlock benchmarks adding a new non-text entry,
// which triggers incremental rendering of just the new entry.
func BenchmarkRenderIncremental_AppendNewBlock(b *testing.B) {
	sizes := []int{10, 50, 200}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("msgs=%d", n*4), func(b *testing.B) {
			m := newBenchModel(n)
			b.ResetTimer()
			for b.Loop() {
				m.appendEntry(renderEntry{content: "benchmark tool call", style: "tool_call"})
				// Restore message count for fair comparison across iterations.
				m.messages = m.messages[:len(m.messages)-1]
				m.fullRebuild()
			}
		})
	}
}

// BenchmarkAppendEntry_StreamingMerge benchmarks the fast streaming path
// where text is merged into the last text entry (skip glamour).
func BenchmarkAppendEntry_StreamingMerge(b *testing.B) {
	sizes := []int{10, 50, 200}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("msgs=%d", n*4), func(b *testing.B) {
			m := newBenchModel(n)
			// Add initial text entry for merging.
			m.appendEntry(renderEntry{content: "initial text", style: "text"})
			m.fullRebuild()

			b.ResetTimer()
			for b.Loop() {
				m.appendEntry(renderEntry{content: " merged chunk", style: "text"})
				// Restore state.
				m.messages[len(m.messages)-1].content = "initial text"
				m.fullRebuild()
			}
		})
	}
}

// BenchmarkAppendEntry_NewTextBlock benchmarks creating a new text block
// (no merge), which triggers full markdown rendering.
func BenchmarkAppendEntry_NewTextBlock(b *testing.B) {
	sizes := []int{10, 50, 200}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("msgs=%d", n*4), func(b *testing.B) {
			m := newBenchModel(n)
			// Ensure last entry is non-text so next text creates new block.
			b.ResetTimer()
			for b.Loop() {
				m.appendEntry(renderEntry{content: "new text block with **markdown**", style: "text"})
				// Remove the added messages (text block may have multiple entries due to newlines).
				for len(m.messages) > 0 && m.messages[len(m.messages)-1].style == "text" {
					m.messages = m.messages[:len(m.messages)-1]
				}
				m.fullRebuild()
			}
		})
	}
}

// BenchmarkRenderMarkdown benchmarks glamour markdown rendering with
// realistic LLM output sizes.
func BenchmarkRenderMarkdown(b *testing.B) {
	inputs := []struct {
		name string
		text string
	}{
		{"small_100", strings.Repeat("Hello world. ", 10)},
		{"medium_500", strings.Repeat("Here is a response with some **bold** and `code`. ", 25)},
		{"large_2000", strings.Repeat("Detailed analysis with markdown formatting including **bold**, `code`, and longer explanations. ", 80)},
		{"code_block_1000", "```go\n" + strings.Repeat("func example() {\n    fmt.Println(\"hello\")\n}\n", 20) + "```\n"},
	}
	for _, in := range inputs {
		b.Run(in.name, func(b *testing.B) {
			b.ResetTimer()
			for b.Loop() {
				renderMarkdown(in.text)
			}
		})
	}
}

// BenchmarkRenderBlocks benchmarks rendering message blocks to string,
// simulating what renderIncremental does for the tail.
func BenchmarkRenderBlocks(b *testing.B) {
	sizes := []int{10, 50, 200}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("msgs=%d", n*4), func(b *testing.B) {
			m := newBenchModel(n)
			// Render from halfway point.
			start := len(m.messages) / 2
			b.ResetTimer()
			for b.Loop() {
				m.renderBlocks(start)
			}
		})
	}
}

// BenchmarkComputeBlockOffsets benchmarks computing byte offsets for blocks.
func BenchmarkComputeBlockOffsets(b *testing.B) {
	sizes := []int{10, 50, 200}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("msgs=%d", n*4), func(b *testing.B) {
			m := newBenchModel(n)
			start := len(m.messages) / 2
			b.ResetTimer()
			for b.Loop() {
				m.computeBlockOffsets(start, 0)
			}
		})
	}
}

// BenchmarkTrimFront benchmarks the message trim operation.
func BenchmarkTrimFront(b *testing.B) {
	buildOverflowModel := func() model {
		m := newBenchModel(0)
		for i := 0; i < 600; i++ {
			m.messages = append(m.messages, renderEntry{content: fmt.Sprintf("padding %d", i), style: "system"})
		}
		m.fullRebuild()
		return m
	}

	b.ResetTimer()
	for b.Loop() {
		m := buildOverflowModel()
		m.appendEntry(renderEntry{content: "overflow trigger", style: "system"})
	}
}

// BenchmarkFullConversationRound simulates a complete conversation round:
// user input → 50 streaming chunks → tool call → tool result → more text.
func BenchmarkFullConversationRound(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		m := newModel()
		m.width = 120
		m.height = 40
		m.agentName = "Dolphin"
		m.modelName = "bench"
		m.showTools = true
		m.ready = true
		m.textarea.SetWidth(119)
		m.viewport.SetWidth(120)
		m.viewport.SetHeight(35)

		// User input.
		m.appendEntry(renderEntry{content: "What is the capital of France?", style: "user_text"})

		// 50 streaming chunks (simulating LLM output).
		for i := 0; i < 50; i++ {
			m.appendEntry(renderEntry{content: " chunk", style: "text"})
		}

		// Tool call + result.
		m.appendEntry(renderEntry{content: "🔧 search(Paris)", style: "tool_call"})
		m.appendEntry(renderEntry{content: "Paris is the capital of France", style: "tool_result"})

		// More text after tool.
		m.appendEntry(renderEntry{content: "The capital of France is Paris.", style: "text"})
	}
}

// BenchmarkRealisticStreaming simulates realistic LLM streaming: 50 small
// text chunks followed by a tool call and final text.
func BenchmarkRealisticStreaming(b *testing.B) {
	b.Run("fast_path", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			m := newModel()
			m.width = 120
			m.height = 40
			m.agentName = "Dolphin"
			m.modelName = "bench"
			m.showTools = true
			m.ready = true
			m.textarea.SetWidth(119)
			m.viewport.SetWidth(120)
			m.viewport.SetHeight(35)

			// First text entry (creates block, glamour render).
			m.appendEntry(renderEntry{content: "Let me analyze your question carefully.", style: "text"})

			// 50 streaming merges (fast path, no glamour).
			for i := 0; i < 50; i++ {
				m.appendEntry(renderEntry{content: " more analysis", style: "text"})
			}
		}
	})
}
