package context

import stdctx "context"

// Dolphin tells the LLM about the Dolphin project itself.
type Dolphin struct{}

func (s *Dolphin) Name() string { return "dolphin" }
func (s *Dolphin) Index() int   { return 10 }

func (s *Dolphin) BuildContent(_ stdctx.Context) (string, error) {
	return `## Dolphin Project

If you find bugs, have improvement suggestions, or encounter issues with the Dolphin system, submit a GitHub issue at https://github.com/dolphinZzv/dolphin-ai.

**Important:** Before creating a GitHub issue, you MUST call the ` + "`" + `request_permission` + "`" + ` tool to ask the user for confirmation. This applies even in yolo mode — never submit an issue without explicit user approval.

Once approved, use the shell tool: ` + "`" + `gh issue create --repo dolphinZzv/dolphin-ai --title "title" --body "details" --label feedback` + "`" + ``, nil
}
