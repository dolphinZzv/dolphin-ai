package workflow

import (
	"testing"
)

// BenchmarkCompileTemplate measures the performance of $step.field → Go template compilation.
func BenchmarkCompileTemplate(b *testing.B) {
	prompts := []string{
		"hello $step.field world",
		"$audit[*].result by $audit[*].severity",
		"step $step.id did $step.action on $step.target",
		"",
		"no template variables here, just plain text that is quite long and represents a typical prompt",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prompt := prompts[i%len(prompts)]
		_, err := compile(prompt)
		if err != nil {
			b.Fatalf("compile failed for %q: %v", prompt, err)
		}
	}
	b.StopTimer()
}

// BenchmarkRenderPrompt measures the full compile + execute pipeline.
func BenchmarkRenderPrompt(b *testing.B) {
	data := map[string]any{
		"audit": []map[string]any{
			{"key": "a.go", "result": map[string]any{"finding": "nil deref", "severity": "high"}},
			{"key": "b.go", "result": map[string]any{"finding": "race", "severity": "medium"}},
		},
		"list": map[string]any{
			"files": []string{"a.go", "b.go", "c.go"},
		},
	}

	prompts := []string{
		"$audit[*].finding ($audit[*].severity)",
		"Files: $list.files",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prompt := prompts[i%len(prompts)]
		_, err := renderPrompt(prompt, data)
		if err != nil {
			b.Fatalf("renderPrompt failed for %q: %v", prompt, err)
		}
	}
	b.StopTimer()
}
