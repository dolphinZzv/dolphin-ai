package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/mcp"
)

type Tool struct {
	cfg    *config.MCPA2AConfig
	client *http.Client
}

func New(cfg *config.Config) *Tool {
	return &Tool{
		cfg:    &cfg.MCP.A2A,
		client: &http.Client{Timeout: a2aTimeout(&cfg.MCP.A2A)},
	}
}

type ListTool struct {
	cfg *config.MCPA2AConfig
}

func NewListTool(cfg *config.Config) *ListTool {
	return &ListTool{cfg: &cfg.MCP.A2A}
}

func (t *ListTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "a2a_list",
		Description: "List all configured A2A agents",
		InputSchema: json.RawMessage(`{"type": "object"}`),
		Priority:    10,
		Source:      "built-in",
	}
}

func (t *ListTool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	agents := make([]map[string]string, len(t.cfg.Agents))
	for i, a := range t.cfg.Agents {
		agents[i] = map[string]string{
			"name": a.Name,
			"url":  a.URL,
		}
	}

	data, _ := json.Marshal(map[string]any{
		"agents": agents,
		"total":  len(agents),
	})

	return &mcp.ToolResult{Content: string(data)}, nil
}

func (t *Tool) Definition() mcp.ToolDefinition {
	agents := make([]string, len(t.cfg.Agents))
	for i, a := range t.cfg.Agents {
		agents[i] = a.Name
	}

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent": map[string]any{
				"type":        "string",
				"description": "Target agent name (from configured agents)",
			},
			"agent_url": map[string]any{
				"type":        "string",
				"description": "URL of the target A2A agent (alternative to agent name)",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "Task description to send",
			},
			"api_key": map[string]any{
				"type":        "string",
				"description": "Optional API key for authentication",
			},
		},
		"required": []string{"task"},
	}

	if len(agents) > 0 {
		schema["properties"].(map[string]any)["agent"] = map[string]any{
			"type":        "string",
			"description": "Target agent name: " + fmt.Sprintf("%v", agents),
			"enum":        agents,
		}
	}

	schemaJSON, _ := json.Marshal(schema)

	return mcp.ToolDefinition{
		Name:        "a2a_send",
		Description: "Send a task to an external A2A agent. Configure multiple agents in config.",
		InputSchema: schemaJSON,
		Priority:    10,
		Source:      "built-in",
	}
}

func (t *Tool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var args struct {
		Agent    string `json:"agent,omitempty"`
		AgentURL string `json:"agent_url,omitempty"`
		Task     string `json:"task"`
		APIKey   string `json:"api_key,omitempty"`
	}

	if err := json.Unmarshal(input, &args); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	if args.Task == "" {
		return &mcp.ToolResult{Content: "task is required", IsError: true}, nil
	}

	targetURL := args.AgentURL

	if targetURL == "" && args.Agent != "" {
		for _, a := range t.cfg.Agents {
			if a.Name == args.Agent {
				targetURL = a.URL
				if args.APIKey == "" {
					args.APIKey = a.APIKey
				}
				break
			}
		}
	}

	if targetURL == "" {
		if len(t.cfg.Agents) > 0 {
			targetURL = t.cfg.Agents[0].URL
		} else {
			return &mcp.ToolResult{Content: "no agent_url provided and no agents configured", IsError: true}, nil
		}
	}

	taskID := generateID()
	msg := struct {
		Role  string `json:"role"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"parts"`
	}{
		Role: "user",
		Parts: []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		}{
			{Type: "text", Text: args.Task},
		},
	}

	params := struct {
		ID      string      `json:"id"`
		Message interface{} `json:"message"`
	}{
		ID:      taskID,
		Message: msg,
	}
	paramsJSON, _ := json.Marshal(params)

	rpcReq := struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		ID      interface{}     `json:"id"`
	}{
		JSONRPC: "2.0",
		Method:  "tasks/send",
		Params:  paramsJSON,
		ID:      1,
	}
	reqJSON, _ := json.Marshal(rpcReq)

	rpcPath := t.cfg.DefaultRPCPath
	if rpcPath == "" {
		rpcPath = "/rpc"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL+rpcPath, bytes.NewBuffer(reqJSON))
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("create request failed: %v", err), IsError: true}, nil
	}

	req.Header.Set("Content-Type", "application/json")
	if args.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+args.APIKey)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("request failed: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return &mcp.ToolResult{Content: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)), IsError: true}, nil
	}

	var rpcResp struct {
		ID     interface{}     `json:"id"`
		Result json.RawMessage `json:"result,omitempty"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("parse response failed: %v", err), IsError: true}, nil
	}

	if rpcResp.Error != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message), IsError: true}, nil
	}

	var result struct {
		ID     string `json:"id"`
		Status struct {
			State   string `json:"state"`
			Message *struct {
				Role  string `json:"role"`
				Parts []struct {
					Type string `json:"type"`
					Text string `json:"text,omitempty"`
				} `json:"parts"`
			} `json:"message,omitempty"`
		} `json:"status"`
	}
	if err := json.Unmarshal(rpcResp.Result, &result); err != nil {
		return &mcp.ToolResult{Content: string(body), IsError: false}, nil
	}

	state := result.Status.State
	var responseText string
	if result.Status.Message != nil {
		for _, part := range result.Status.Message.Parts {
			if part.Type == "text" {
				responseText += part.Text
			}
		}
	}

	data, _ := json.Marshal(map[string]any{
		"task_id":  taskID,
		"state":    state,
		"response": responseText,
	})

	return &mcp.ToolResult{Content: string(data)}, nil
}

func generateID() string {
	return fmt.Sprintf("task-%d", time.Now().UnixNano())
}

func a2aTimeout(cfg *config.MCPA2AConfig) time.Duration {
	if cfg.TimeoutSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(cfg.TimeoutSeconds) * time.Second
}
