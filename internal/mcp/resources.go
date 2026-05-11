package mcp

import "encoding/json"

type Resources struct {
	resources []ResourceDefinition
}

func NewResources() *Resources {
	return &Resources{
		resources: []ResourceDefinition{
			{
				URI:         "project://{id}",
				Name:        "Project",
				Description: "Project details",
				MimeType:    "application/json",
			},
			{
				URI:         "issue://{project}/{number}",
				Name:        "Issue",
				Description: "Issue details by project and number",
				MimeType:    "application/json",
			},
			{
				URI:         "agent://{id}",
				Name:        "Agent",
				Description: "Agent details",
				MimeType:    "application/json",
			},
		},
	}
}

func (r *Resources) List() []ResourceDefinition {
	return r.resources
}

func (r *Resources) Read(uri string) (interface{}, error) {
	return map[string]interface{}{
		"uri":     uri,
		"content": "Resource reading not yet implemented",
	}, nil
}

// prompts.go
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
				Name:        "skill-exec",
				Description: "Guide for executing a skill on an issue",
				Arguments: []PromptArgument{
					{Name: "skillName", Description: "Skill name", Required: true},
					{Name: "issueId", Description: "Issue ID", Required: true},
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
		return "You are reviewing issue " + args["issueId"] + ". Check the code, add comments, and approve or reject.", nil
	case "skill-exec":
		return "You are executing skill " + args["skillName"] + " on issue " + args["issueId"] + ". Follow the skill steps.", nil
	case "issue-triage":
		return "You are triaging issue " + args["issueId"] + ". Analyze the description, add labels, and assign priority.", nil
	default:
		return "", nil
	}
}

// Ensure json import is used
var _ = json.Marshal
