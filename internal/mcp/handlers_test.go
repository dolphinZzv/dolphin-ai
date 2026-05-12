package mcp_test

import (
	"encoding/json"
	"testing"

	"chick/internal/mcp"
)

func TestAgentHeartbeat(t *testing.T) {
	srv, _, agentSvc, _ := setupTest(t)

	_, err := agentSvc.Register("hb-agent", "ai", "hb-001", "pass", []string{"CODING"}, "", "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "agent_heartbeat",
		"arguments": map[string]interface{}{
			"agentId": "1",
		},
	})
	if result["success"] != true {
		t.Error("expected heartbeat to succeed")
	}
}

func TestAssignIssue(t *testing.T) {
	srv, _, agentSvc, _ := setupTest(t)

	agentSvc.Register("assign-agent", "ai", "assign-001", "pass", nil, "", "")

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_project",
		"arguments": map[string]interface{}{"name": "AssignTest"},
	})
	projectID := result["id"].(string)

	result = call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"projectId": projectID,
			"title":     "Assignable",
			"creatorId": "1",
		},
	})
	issueID := result["id"].(string)

	result = call(t, srv, "tools/call", map[string]interface{}{
		"name": "assign_issue",
		"arguments": map[string]interface{}{
			"issueId": issueID,
			"agentId": "1",
		},
	})
	if result["success"] != true {
		t.Errorf("expected assign to succeed, got %v", result)
	}
}

func TestListAgents(t *testing.T) {
	srv, _, agentSvc, _ := setupTest(t)

	agentSvc.Register("list-agent-1", "ai", "list-001", "pass", []string{"CODING"}, "", "")
	agentSvc.Register("list-agent-2", "ai", "list-002", "pass", []string{"TESTING"}, "", "")

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "list_agents",
		"arguments": map[string]interface{}{
			"kind": "ai",
		},
	})
	items, ok := result["items"].([]interface{})
	if !ok {
		data, _ := json.Marshal(result["items"])
		json.Unmarshal(data, &items)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 agents, got %d", len(items))
	}
}

func TestListAgentsAll(t *testing.T) {
	srv, _, agentSvc, _ := setupTest(t)

	agentSvc.Register("agent-1", "ai", "la-001", "pass", nil, "", "")
	agentSvc.Register("agent-2", "human", "la-002", "pass", nil, "", "")

	result := call(t, srv, "tools/call", map[string]interface{}{
		"name": "list_agents",
		"arguments": map[string]interface{}{},
	})
	data, _ := json.Marshal(result["items"])
	var items []interface{}
	json.Unmarshal(data, &items)
	if len(items) < 2 {
		t.Errorf("expected at least 2 agents, got %d", len(items))
	}
}

func TestJSONRPCBasic(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "ping",
	}
	resp := srv.HandleRequest(req)
	if resp.Error != nil {
		t.Fatalf("unexpected ping error: %s", resp.Error.Message)
	}

	req2 := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "invalid_method",
	}
	resp2 := srv.HandleRequest(req2)
	if resp2.Error == nil {
		t.Fatal("expected error for invalid method")
	}
}
