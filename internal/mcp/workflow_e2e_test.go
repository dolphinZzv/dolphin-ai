package mcp_test

import (
	"encoding/json"
	"testing"

	"chick/internal/models"
)

func TestAgentWorkflowE2E(t *testing.T) {
	srv, projectSvc, agentSvc, _ := setupTest(t)

	// ── Step 1: Create a project via service ──
	proj, err := projectSvc.Create("E2E Test Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// ── Step 2: Register agent via service ──
	agent, err := agentSvc.Register("e2e-agent", models.AgentKindAI, "e2e-001", "pass123", nil, "Linux / Chrome", "Claude 4 Opus")
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}
	agentID := agent.ID

	// Add to project
	projectSvc.AddMember(proj.ID, agentID, models.ProjectRoleMember)

	// ── Step 3: Verify agent info ──
	infoResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "get_agent_info",
		"arguments": map[string]interface{}{
			"agentId": "1",
		},
	}, agentID)
	if infoResult["deviceInfo"] != "Linux / Chrome" {
		t.Errorf("expected deviceInfo, got %v", infoResult["deviceInfo"])
	}
	if infoResult["modelInfo"] != "Claude 4 Opus" {
		t.Errorf("expected modelInfo, got %v", infoResult["modelInfo"])
	}
	if infoResult["status"] != "online" {
		t.Errorf("expected status online, got %v", infoResult["status"])
	}

	// ── Step 4: List agents by project ──
	listResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "list_agents",
		"arguments": map[string]interface{}{
			"projectId": "1",
		},
	}, agentID)
	items := toSlice(listResult["items"])
	if len(items) < 1 {
		t.Error("expected at least 1 agent in project")
	}

	// ── Step 5: Create issue (auto-derive project from membership) ──
	issueResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"title":       "E2E Test Issue",
			"description": "Created without projectId",
			"priority":    "high",
		},
	}, agentID)
	if issueResult["title"] != "E2E Test Issue" {
		t.Errorf("expected title, got %v", issueResult["title"])
	}
	if issueResult["state"] != "open" {
		t.Errorf("expected state open, got %v", issueResult["state"])
	}
	issueID := issueResult["id"].(string)

	// ── Step 6: Transition issue to in_progress ──
	transResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "transition_issue",
		"arguments": map[string]interface{}{
			"issueId": issueID,
			"toState": "in_progress",
		},
	}, agentID)
	if transResult["state"] != "in_progress" {
		t.Errorf("expected in_progress, got %v", transResult["state"])
	}

	// ── Step 7: Add comment ──
	call(t, srv, "tools/call", map[string]interface{}{
		"name": "add_comment",
		"arguments": map[string]interface{}{
			"issueId": issueID,
			"body":    "Working on it",
		},
	}, agentID)

	// ── Step 8: Transition to review ──
	transReview := call(t, srv, "tools/call", map[string]interface{}{
		"name": "transition_issue",
		"arguments": map[string]interface{}{
			"issueId": issueID,
			"toState": "review",
		},
	}, agentID)
	if transReview["state"] != "review" {
		t.Fatalf("expected review, got %v", transReview["state"])
	}

	// ── Step 9: Search issues ──
	searchResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "search_issues",
		"arguments": map[string]interface{}{
			"state": "review",
		},
	}, agentID)
	if toInt(searchResult["total"]) < 1 {
		t.Errorf("expected at least 1 issue in review state, got %v", searchResult["total"])
	}

	// ── Step 10: Agent heartbeat ──
	hbResult := call(t, srv, "tools/call", map[string]interface{}{
		"name": "agent_heartbeat",
		"arguments": map[string]interface{}{},
	}, agentID)
	if hbResult["success"] != true {
		t.Error("heartbeat failed")
	}

	// ── Step 11: Verify create_issue with explicit projectId still works ──
	call(t, srv, "tools/call", map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"projectId": "1",
			"title":     "Explicit Project ID",
		},
	}, agentID)
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
