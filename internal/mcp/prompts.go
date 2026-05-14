package mcp

import "fmt"

type Prompts struct {
	prompts []PromptDefinition
}

func NewPrompts() *Prompts {
	return &Prompts{
		prompts: []PromptDefinition{
			{
				Name:        "review-workflow",
				Description: "Guide for reviewing and approving issues",
				Arguments: []PromptArgument{
					{Name: "issueId", Description: "Issue ID to review", Required: true},
				},
			},
			{
				Name:        "issue-triage",
				Description: "Guide for triaging a new issue",
				Arguments: []PromptArgument{
					{Name: "issueId", Description: "Issue ID to triage", Required: true},
				},
			},
		},
	}
}

func (p *Prompts) List() []PromptDefinition {
	return p.prompts
}

func (p *Prompts) Get(name string, args map[string]string) (string, error) {
	switch name {
	case "review-workflow":
		return fmt.Sprintf(
			"You are reviewing issue %s. Check the title, description, and discussion. Add comments if needed, then approve or reject the issue.",
			args["issueId"],
		), nil
	case "issue-triage":
		return fmt.Sprintf(
			"You are triaging issue %s. Read the issue, classify its priority (critical/high/medium/low), check if it has a clear description, assign appropriate agents, and move it to the right state.",
			args["issueId"],
		), nil
	default:
		return "", fmt.Errorf("unknown prompt: %s", name)
	}
}
