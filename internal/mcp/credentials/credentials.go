package credentials

import (
	"context"
	"encoding/json"
	"fmt"

	"dolphin/internal/mcp"
)

type Tool struct {
	store Store
}

func New(cfg *CredentialsConfig) *Tool {
	var store Store
	if cfg != nil && cfg.Enabled {
		store = NewFileStore(cfg)
	}
	return &Tool{store: store}
}

func (t *Tool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "credentials_search",
		Description: "Search stored credentials. Returns credential names, types, URLs, and comments (no secrets). Use credentials_get to retrieve the full credential with secret.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {
					"type": "string",
					"description": "Search keyword (matches name, url, username, comment)"
				},
				"type": {
					"type": "string",
					"enum": ["api_key", "oauth", "aws_access_key", "password", "ssh_key"],
					"description": "Filter by credential type"
				},
				"limit": {
					"type": "integer",
					"description": "Maximum results to return",
					"default": 10
				}
			}
		}`),
		Priority: 50,
		Source:   "built-in",
	}
}

func (t *Tool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if t.store == nil {
		return &mcp.ToolResult{Content: "credentials not enabled", IsError: true}, nil
	}

	var args struct {
		Query string `json:"query"`
		Type  string `json:"type"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	results, err := t.store.Search(args.Query, args.Type, args.Limit)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("search error: %v", err), IsError: true}, nil
	}

	data, _ := json.MarshalIndent(map[string]interface{}{
		"credentials": results,
		"total":       len(results),
	}, "", "  ")
	return &mcp.ToolResult{Content: string(data)}, nil
}

type GetTool struct {
	store Store
}

func NewGetTool(cfg *CredentialsConfig) *GetTool {
	var store Store
	if cfg != nil && cfg.Enabled {
		store = NewFileStore(cfg)
	}
	return &GetTool{store: store}
}

func (t *GetTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "credentials_get",
		Description: "Get a single credential by name, including its secret. Use credentials_search first to find the credential name.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Credential name"
				}
			},
			"required": ["name"]
		}`),
		Priority: 50,
		Source:   "built-in",
	}
}

func (t *GetTool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if t.store == nil {
		return &mcp.ToolResult{Content: "credentials not enabled", IsError: true}, nil
	}

	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	cred, err := t.store.Get(args.Name)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("get error: %v", err), IsError: true}, nil
	}
	if cred == nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("credential not found: %s", args.Name), IsError: true}, nil
	}

	data, _ := json.MarshalIndent(cred, "", "  ")
	return &mcp.ToolResult{Content: string(data)}, nil
}

type AddTool struct {
	store Store
}

func NewAddTool(cfg *CredentialsConfig) *AddTool {
	var store Store
	if cfg != nil && cfg.Enabled {
		store = NewFileStore(cfg)
	}
	return &AddTool{store: store}
}

func (t *AddTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "credentials_add",
		Description: "Add a new credential or update existing one. Use this when user provides API key, OAuth token, or password.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Unique credential name (e.g., 'github-api', 'aws-prod')"
				},
				"type": {
					"type": "string",
					"enum": ["api_key", "oauth", "aws_access_key", "password", "ssh_key", "client_secret"],
					"description": "Credential type"
				},
				"url": {
					"type": "string",
					"description": "API endpoint URL (optional)"
				},
				"username": {
					"type": "string",
					"description": "Username (optional)"
				},
				"secret": {
					"type": "string",
					"description": "API key, token, or password"
				},
				"comment": {
					"type": "string",
					"description": "Description or note (optional)"
				}
			},
			"required": ["name", "type", "secret"]
		}`),
		Priority: 50,
		Source:   "built-in",
	}
}

func (t *AddTool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if t.store == nil {
		return &mcp.ToolResult{Content: "credentials not enabled", IsError: true}, nil
	}

	var args struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Secret   string `json:"secret"`
		Comment  string `json:"comment"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	if args.Name == "" {
		return &mcp.ToolResult{Content: "name is required", IsError: true}, nil
	}
	if args.Secret == "" {
		return &mcp.ToolResult{Content: "secret is required", IsError: true}, nil
	}

	cred := &Credential{
		Name:     args.Name,
		Type:     args.Type,
		URL:      args.URL,
		Username: args.Username,
		Secret:   args.Secret,
		Comment:  args.Comment,
	}

	if err := t.store.Add(cred); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("add error: %v", err), IsError: true}, nil
	}

	return &mcp.ToolResult{Content: fmt.Sprintf(`{"success": true, "name": "%s", "message": "Credential saved successfully"}`, args.Name)}, nil
}

type DeleteTool struct {
	store Store
}

func NewDeleteTool(cfg *CredentialsConfig) *DeleteTool {
	var store Store
	if cfg != nil && cfg.Enabled {
		store = NewFileStore(cfg)
	}
	return &DeleteTool{store: store}
}

func (t *DeleteTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "credentials_delete",
		Description: "Delete a stored credential by name. Use credentials_search first to find the credential name.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "Credential name to delete"
				}
			},
			"required": ["name"]
		}`),
		Priority: 50,
		Source:   "built-in",
	}
}

func (t *DeleteTool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	if t.store == nil {
		return &mcp.ToolResult{Content: "credentials not enabled", IsError: true}, nil
	}

	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	if err := t.store.Delete(args.Name); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("delete error: %v", err), IsError: true}, nil
	}

	return &mcp.ToolResult{Content: fmt.Sprintf(`{"success": true, "name": "%s", "message": "Credential deleted"}`, args.Name)}, nil
}
