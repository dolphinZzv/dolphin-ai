package mcp_test

import (
	"encoding/json"
	"testing"
)

func TestAgentWorkflowE2E(t *testing.T) {
	srv, projectSvc, _, _ := setupTest(t)

	// ── Step 1: Create a project ──
	projectResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_project",
		"arguments": map[string]interface{}{
			"name": "E2E Test Project",
		},
	})
	projectID := projectResult["id"].(string)

	// ── Step 2: Register agent with projectId ──
	regResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "register_agent",
		"arguments": map[string]interface{}{
			"name":       "e2e-agent",
			"kind":       "ai",
			"externalId": "e2e-001",
			"secret":     "pass123",
			"projectId":  projectID,
			"deviceInfo": "Linux / Chrome",
			"modelInfo":  "Claude 4 Opus",
		},
	})
	agentID := regResult["id"].(string)
	if regResult["externalId"] != "e2e-001" {
		t.Errorf("expected externalId e2e-001, got %v", regResult["externalId"])
	}

	// ── Step 3: Re-register same externalId should succeed (idempotent) ──
	reRegResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "register_agent",
		"arguments": map[string]interface{}{
			"name":       "e2e-agent",
			"kind":       "ai",
			"externalId": "e2e-001",
			"secret":     "pass123",
		},
	})
	if reRegResult["id"] != agentID {
		t.Errorf("expected same agent ID %s, got %v", agentID, reRegResult["id"])
	}

	// ── Step 4: Verify agent is in project ──
	members, err := projectSvc.ListMembers(parseUint(projectID))
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	found := false
	for _, m := range members {
		if m.AgentID == parseUint(agentID) {
			found = true
			break
		}
	}
	if !found {
		t.Error("agent was not added to project members")
	}

	// ── Step 5: Get agent info ──
	infoResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "get_agent_info",
		"arguments": map[string]interface{}{
			"agentId": agentID,
		},
	})
	if infoResult["deviceInfo"] != "Linux / Chrome" {
		t.Errorf("expected deviceInfo, got %v", infoResult["deviceInfo"])
	}
	if infoResult["modelInfo"] != "Claude 4 Opus" {
		t.Errorf("expected modelInfo, got %v", infoResult["modelInfo"])
	}
	if infoResult["status"] != "online" {
		t.Errorf("expected status online, got %v", infoResult["status"])
	}

	// ── Step 6: List agents by project ──
	listResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "list_agents",
		"arguments": map[string]interface{}{
			"projectId": projectID,
		},
	})
	items := toSlice(listResult["items"])
	if len(items) < 1 {
		t.Error("expected at least 1 agent in project")
	}

	// ── Step 7: Create issue without projectId (auto-derive) ──
	issueResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"title":       "E2E Test Issue",
			"description": "Created without projectId",
			"creatorId":   agentID,
			"priority":    "high",
		},
	})
	if issueResult["title"] != "E2E Test Issue" {
		t.Errorf("expected title, got %v", issueResult["title"])
	}
	if issueResult["state"] != "open" {
		t.Errorf("expected state open, got %v", issueResult["state"])
	}
	issueID := issueResult["id"].(string)

	// ── Step 8: Transition issue to in_progress ──
	transResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "transition_issue",
		"arguments": map[string]interface{}{
			"issueId": issueID,
			"toState": "in_progress",
			"actorId": agentID,
		},
	})
	if transResult["state"] != "in_progress" {
		t.Errorf("expected in_progress, got %v", transResult["state"])
	}

	// ── Step 9: Add comment ──
	call(t, srv, "tools/call", map[string]interface{}{
		"name": "add_comment",
		"arguments": map[string]interface{}{
			"issueId":  issueID,
			"authorId": agentID,
			"body":     "Working on it",
		},
	})

	// ── Step 10: Transition to review ──
	transReview := call(t, srv, "tools/call", map[string]interface{}{
		"name": "transition_issue",
		"arguments": map[string]interface{}{
			"issueId": issueID,
			"toState": "review",
			"actorId": agentID,
		},
	})
	if transReview["state"] != "review" {
		t.Fatalf("expected review, got %v", transReview["state"])
	}

	// ── Step 11: Search issues ──
	searchResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "search_issues",
		"arguments": map[string]interface{}{
			"projectId": projectID,
			"state":     "review",
		},
	})
	if toInt(searchResult["total"]) < 1 {
		t.Errorf("expected at least 1 issue in review state, got %v", searchResult["total"])
	}

	// ── Step 12: Agent heartbeat ──
	hbResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "agent_heartbeat",
		"arguments": map[string]interface{}{
			"agentId": agentID,
		},
	})
	if hbResult["success"] != true {
		t.Error("heartbeat failed")
	}

	// ── Step 13: Search projects ──
	searchProjResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "search_projects",
		"arguments": map[string]interface{}{
			"search": "E2E",
		},
	})
	projItems := toSlice(searchProjResult["items"])
	if len(projItems) < 1 {
		t.Errorf("expected at least 1 project matching E2E, got %v", searchProjResult["items"])
	}

	// ── Step 14: Verify create_issue with explicit projectId still works ──
	call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"projectId": projectID,
			"title":     "Explicit Project ID",
			"creatorId": agentID,
		},
	})
}

// toSlice converts interface{} to []interface{} safely
func toSlice(v interface{}) []interface{} {
	if s, ok := v.([]interface{}); ok {
		return s
	}
	data, _ := json.Marshal(v)
	var s []interface{}
	json.Unmarshal(data, &s)
	return s
}

// toInt converts interface{} to int safely
func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

// Helper to parse uint from string ID returned by MCP
func parseUint(idStr string) uint {
	var id uint
	for _, c := range idStr {
		if c >= '0' && c <= '9' {
			id = id*10 + uint(c-'0')
		} else {
			break
		}
	}
	return id
}
